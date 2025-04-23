package attacks

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/kyleseneker/nomad-nemesis/internal/config"
	"github.com/kyleseneker/nomad-nemesis/internal/logging"
	"github.com/kyleseneker/nomad-nemesis/internal/nomad"
	"github.com/kyleseneker/nomad-nemesis/internal/tracker"
)

// --- Node Drainer Attack ---

type NodeDrainer struct {
	tracker tracker.Tracker
	logger  logging.Logger
}

// NewNodeDrainer creates a new NodeDrainer attack.
func NewNodeDrainer(t tracker.Tracker) *NodeDrainer {
	return &NodeDrainer{
		tracker: t,
		logger:  logging.Get().Named("node_drainer"),
	}
}

func (a *NodeDrainer) Name() string {
	return "Node Drainer"
}

func (a *NodeDrainer) IsEnabled(cfg config.Config) bool {
	return cfg.EnableNodeDrain
}

func (a *NodeDrainer) Execute(nomadClient *nomad.Client, cfg config.Config) error {
	a.logger.Info("Starting execution cycle...")

	var lastError error // Variable to hold the last error encountered

	targets, err := nomadClient.SelectTargetNodes(cfg)
	if err != nil {
		a.logger.Error("Error selecting targets", "error", err)
		return fmt.Errorf("failed to select target nodes: %w", err) // Return error
	}

	numTargets := len(targets)
	a.logger.Info("Found potential targets", "count", numTargets)

	limit := calculateLimit(cfg.MaxDrainCount, cfg.MaxDrainPercent, numTargets)
	if limit == 0 && numTargets > 0 {
		a.logger.Info("Calculated limit is 0, skipping drain.", "max_count", cfg.MaxDrainCount, "max_percent", cfg.MaxDrainPercent, "total_targets", numTargets)
		return nil
	}
	a.logger.Debug("Applying limit", "limit", limit, "max_count", cfg.MaxDrainCount, "max_percent", cfg.MaxDrainPercent)

	drainedCount := 0
	rand.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })

	for _, nodeStub := range targets {
		nodeLogger := a.logger.With("node_id", nodeStub.ID, "node_name", nodeStub.Name, "datacenter", nodeStub.Datacenter, "status", nodeStub.Status)

		if drainedCount >= limit {
			nodeLogger.Info("Reached drain limit for this cycle.", "limit", limit)
			break // Stop processing more targets
		}

		// --- Eligibility Checks ---
		if nodeStub.Status != "ready" {
			nodeLogger.Debug("Skipping: Status not 'ready'.")
			continue
		}

		lastDrainTime, found := a.tracker.GetLastActionTime(nodeStub.ID)
		if found && time.Since(lastDrainTime) < cfg.MinTimeBetweenDrains {
			nodeLogger.Debug("Skipping: Minimum time between drains not elapsed", "min_time", cfg.MinTimeBetweenDrains, "last_drain", lastDrainTime.Format(time.RFC3339))
			continue
		}

		// --- Action Logic ---
		randomDraw := rand.Float64()
		if randomDraw < cfg.DrainProbability {
			drainedCount++
			nodeLogger = nodeLogger.With("attempt_num", drainedCount, "limit", limit, "probability", cfg.DrainProbability, "draw", randomDraw)

			if !cfg.DryRun {
				nodeLogger.Warn("Attempting to drain node")
				err := nomadClient.DrainNode(nodeStub.ID)
				if err != nil {
					nodeLogger.Error("Failed to drain node", "error", err)
					lastError = err // Store the error
					// Continue to next node even if drain fails for one
				} else {
					nodeLogger.Info("Successfully initiated drain for node")
					if err := a.tracker.RecordAction("node", nodeStub.ID, "drained"); err != nil {
						nodeLogger.Warn("Failed to record drain action in tracker", "error", err)
						// Don't treat tracker error as cycle error
					}
				}
			} else {
				nodeLogger.Warn("Dry Run: Would have drained node")
				if err := a.tracker.RecordAction("node", nodeStub.ID, "drained (dry run)"); err != nil {
					nodeLogger.Warn("Failed to record drain action (dry run) in tracker", "error", err)
					// Don't treat tracker error as cycle error
				}
			}
		} else {
			nodeLogger.Debug("Skipping: Random draw >= drain probability", "draw", randomDraw, "probability", cfg.DrainProbability)
		}
	} // End loop over targets

	// Final summary log
	summaryLogger := a.logger.With("drained_count", drainedCount, "limit", limit, "dry_run", cfg.DryRun)
	if lastError != nil {
		summaryLogger.Warn("Finished cycle with errors.")
	} else if drainedCount == 0 {
		summaryLogger.Info("Finished cycle. No nodes drained.")
	} else {
		summaryLogger.Info("Finished cycle.")
	}

	return lastError // Return the last error encountered, or nil
}
