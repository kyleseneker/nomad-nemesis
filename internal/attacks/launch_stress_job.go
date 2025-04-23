package attacks

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	// For parsing jobspec
	// For generating unique job IDs

	"github.com/google/uuid"
	"github.com/hashicorp/nomad/api"
	"github.com/kyleseneker/nomad-nemesis/internal/config"
	"github.com/kyleseneker/nomad-nemesis/internal/logging"
	"github.com/kyleseneker/nomad-nemesis/internal/nomad"
	"github.com/kyleseneker/nomad-nemesis/internal/tracker"
)

// --- Stress Job Launcher Attack ---

// StressJobLauncher attack launches short-lived stress jobs on target nodes.
type StressJobLauncher struct {
	tracker              tracker.Tracker
	logger               logging.Logger
	mu                   sync.Mutex        // Protects concurrentStressJobs
	concurrentStressJobs map[string]string // Map NodeID -> Stress Job ID (simple in-memory tracking)
}

// NewStressJobLauncher creates a new StressJobLauncher attack instance.
func NewStressJobLauncher(t tracker.Tracker) *StressJobLauncher {
	return &StressJobLauncher{
		tracker:              t,
		logger:               logging.Get().Named("stress_job_launcher"),
		concurrentStressJobs: make(map[string]string),
	}
}

// Name returns the user-friendly name of the attack.
func (a *StressJobLauncher) Name() string {
	return "Stress Job Launcher"
}

// IsEnabled checks if this attack is enabled in the configuration.
func (a *StressJobLauncher) IsEnabled(cfg config.Config) bool {
	return cfg.StressJobLauncher.Enabled
}

// Execute runs the StressJobLauncher attack cycle.
func (a *StressJobLauncher) Execute(nomadClient *nomad.Client, cfg config.Config) error {
	a.logger.Info("Starting execution cycle...")

	var lastError error
	attackCfg := cfg.StressJobLauncher // Alias for easier access

	// 1. Select potential target nodes
	// TODO: Need to implement SelectResourcePressureTargets in nomad package
	targets, err := nomadClient.SelectStressJobLauncherTargets(cfg) // Use the renamed client method
	if err != nil {
		a.logger.Error("Error selecting target nodes for stress job launcher", "error", err) // Updated log message
		return fmt.Errorf("failed to select target nodes: %w", err)
	}

	numTargets := len(targets)
	if numTargets == 0 {
		a.logger.Info("No target nodes found matching criteria.")
		return nil
	}
	a.logger.Info("Found potential target nodes", "count", numTargets)

	// 2. Filter and Select Nodes to Stress
	kickedOffCount := 0
	rand.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })

	// Simple concurrent job tracking cleanup (could be more robust)
	a.cleanupFinishedStressJobs(nomadClient)

	for _, nodeStub := range targets {
		nodeLogger := a.logger.With("node_id", nodeStub.ID, "node_name", nodeStub.Name)

		// a. Check MaxConcurrent Limit
		a.mu.Lock()
		concurrentCount := len(a.concurrentStressJobs)
		if concurrentCount >= attackCfg.MaxConcurrent {
			nodeLogger.Info("Reached max concurrent stress jobs limit.", "limit", attackCfg.MaxConcurrent, "current", concurrentCount)
			a.mu.Unlock()
			break // Stop trying to launch more
		}
		// Check if this node *already* has a stress job from us
		if _, alreadyRunning := a.concurrentStressJobs[nodeStub.ID]; alreadyRunning {
			nodeLogger.Debug("Skipping: Node already has a stress job running.")
			a.mu.Unlock()
			continue
		}
		a.mu.Unlock() // Unlock early before potentially slow checks/actions

		// b. Check Cooldown (MinTimeBetween)
		lastStressTime, found := a.tracker.GetLastActionTime(nodeStub.ID)
		if found && time.Since(lastStressTime) < attackCfg.MinTimeBetween {
			nodeLogger.Debug("Skipping: Minimum time between attacks not elapsed", "min_time", attackCfg.MinTimeBetween, "last_stress", lastStressTime.Format(time.RFC3339))
			continue
		}

		// c. Check Probability
		randomDraw := rand.Float64()
		if randomDraw >= attackCfg.Probability {
			nodeLogger.Debug("Skipping: Random draw >= probability", "draw", randomDraw, "probability", attackCfg.Probability)
			continue
		}

		// --- Node Selected for Stress ---
		kickedOffCount++
		nodeLogger = nodeLogger.With("attempt_num", kickedOffCount, "probability", attackCfg.Probability, "draw", randomDraw)

		// 3. Parse and Modify Job Spec
		// We validated syntax in LoadConfig, now parse again (JSON only for now)
		var job api.Job
		if err := json.Unmarshal([]byte(attackCfg.StressJobSpec), &job); err != nil {
			nodeLogger.Error("Failed to parse stress_job_spec (JSON) during execution", "error", err)
			lastError = fmt.Errorf("failed to parse stress job spec for node %s: %w", nodeStub.ID, err)
			continue // Try next node
		}

		// Modify the job
		originalJobID := "stress-injector" // Default or get from parsed job
		if job.ID != nil && *job.ID != "" {
			originalJobID = *job.ID
		}
		uniqueJobID := fmt.Sprintf("%s-%s-%s", originalJobID, nodeStub.ID, uuid.New().String()[:8])
		job.ID = &uniqueJobID
		job.Name = &uniqueJobID // Keep Name and ID the same for simplicity

		// Ensure Task Groups exist
		if len(job.TaskGroups) == 0 {
			nodeLogger.Error("Stress job spec has no task groups defined")
			lastError = fmt.Errorf("stress job spec for node %s has no task groups", nodeStub.ID)
			continue
		}

		// Add Node ID Constraint (assuming simple case, add to first group)
		tg := job.TaskGroups[0]
		if tg.Constraints == nil {
			tg.Constraints = make([]*api.Constraint, 0)
		}
		nodeConstraint := api.NewConstraint("${node.id}", "=", nodeStub.ID)
		tg.Constraints = append(tg.Constraints, nodeConstraint)

		// Add Meta (used by jobspec, e.g., ${NOMAD_META_STRESS_DURATION})
		if tg.Meta == nil {
			tg.Meta = make(map[string]string)
		}
		tg.Meta["TARGET_NODE_ID"] = nodeStub.ID
		tg.Meta["STRESS_DURATION"] = attackCfg.StressDuration

		// 4. Register Job
		if cfg.DryRun {
			nodeLogger.Warn("Dry Run: Would register stress job", "job_id", uniqueJobID)
			// Record action even in dry run for cooldown
			if err := a.tracker.RecordAction("node", nodeStub.ID, "stressed (dry run)"); err != nil {
				nodeLogger.Warn("Dry Run: Failed to record stress action in tracker", "error", err)
			}
			// Track concurrency even in dry run to simulate limit
			a.trackStressJobStart(nodeStub.ID, uniqueJobID)
		} else {
			nodeLogger.Warn("Attempting to register stress job", "job_id", uniqueJobID)
			_, err := nomadClient.RegisterJob(&job)
			if err != nil {
				nodeLogger.Error("Failed to register stress job", "error", err)
				lastError = fmt.Errorf("failed to register stress job for node %s: %w", nodeStub.ID, err)
				continue // Try next node
			}
			nodeLogger.Info("Successfully registered stress job", "job_id", uniqueJobID)

			// Record success in tracker
			if err := a.tracker.RecordAction("node", nodeStub.ID, "stressed"); err != nil {
				nodeLogger.Warn("Failed to record stress action in tracker", "error", err)
				// Continue anyway, registration succeeded
			}
			// Track concurrency
			a.trackStressJobStart(nodeStub.ID, uniqueJobID)
		}
	} // End loop over targets

	summaryLogger := a.logger.With("launched_count", kickedOffCount, "dry_run", cfg.DryRun)
	if lastError != nil {
		summaryLogger.Warn("Finished cycle with errors.")
	} else if kickedOffCount == 0 {
		summaryLogger.Info("Finished cycle. No stress jobs launched.")
	} else {
		summaryLogger.Info("Finished cycle.")
	}

	return lastError
}

// trackStressJobStart adds a job to the concurrent tracking map.
func (a *StressJobLauncher) trackStressJobStart(nodeID, jobID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.concurrentStressJobs[nodeID] = jobID
}

// cleanupFinishedStressJobs checks the status of tracked jobs and removes finished ones.
func (a *StressJobLauncher) cleanupFinishedStressJobs(nomadClient *nomad.Client) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for nodeID, jobID := range a.concurrentStressJobs {
		// Correct assignment: GetJobStatus returns (string, error)
		status, err := nomadClient.GetJobStatus(jobID)
		if err != nil {
			// Error fetching status - maybe job was GC'd quickly or API error
			a.logger.Warn("Error checking status of stress job, assuming finished", "job_id", jobID, "node_id", nodeID, "error", err)
			delete(a.concurrentStressJobs, nodeID)
			continue
		}
		// Consider job finished if status is complete, failed, or unknown (potentially GC'd or not found)
		if status == "dead" || status == "complete" || status == "failed" || status == "" {
			a.logger.Debug("Cleaning up finished stress job record", "job_id", jobID, "node_id", nodeID, "status", status)
			delete(a.concurrentStressJobs, nodeID)
		}
	}
}
