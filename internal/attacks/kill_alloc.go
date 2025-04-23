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

// --- Allocation Killer Attack ---

// AllocKiller targets and kills Nomad allocations, maintaining state for per-job limits.
type AllocKiller struct {
	tracker       tracker.Tracker
	jobKillCounts map[string]int
	lastKillTimes map[string]time.Time
	logger        logging.Logger
}

// NewAllocKiller creates a new AllocKiller attack instance.
func NewAllocKiller(t tracker.Tracker) *AllocKiller {
	return &AllocKiller{
		tracker:       t,
		jobKillCounts: make(map[string]int),
		lastKillTimes: make(map[string]time.Time),
		logger:        logging.Get().Named("alloc_killer"),
	}
}

func (a *AllocKiller) Name() string {
	return "Allocation Killer"
}

func (a *AllocKiller) IsEnabled(cfg config.Config) bool {
	return len(cfg.TargetJobs) > 0
}

// Helper function to get effective alloc kill parameters for a job
func getEffectiveAllocKillParams(cfg *config.Config, jobID string) (float64, time.Duration, int, time.Duration) {
	globalProb := cfg.KillProbability
	globalMTBK := cfg.MeanTimeBetweenKills
	globalMaxCount := cfg.MaxKillCount
	globalMinTime := cfg.MinTimeBetweenKills

	jobOverride, exists := cfg.JobOverrides[jobID]
	if !exists {
		// No override for this job, return global defaults
		return globalProb, globalMTBK, globalMaxCount, globalMinTime
	}

	// Start with global defaults, override if specified
	effectiveProb := globalProb
	if jobOverride.KillProbability != nil {
		effectiveProb = *jobOverride.KillProbability
	}

	effectiveMTBK := globalMTBK
	if jobOverride.MeanTimeBetweenKills != nil {
		effectiveMTBK = *jobOverride.MeanTimeBetweenKills
	}

	effectiveMaxCount := globalMaxCount
	if jobOverride.MaxKillCount != nil {
		effectiveMaxCount = *jobOverride.MaxKillCount
	}

	effectiveMinTime := globalMinTime
	if jobOverride.MinTimeBetweenKills != nil {
		effectiveMinTime = *jobOverride.MinTimeBetweenKills
	}

	// If MTBK is set in override, it potentially nullifies the probability value,
	// but we return both for logging/context. The Execute logic decides which to use.
	return effectiveProb, effectiveMTBK, effectiveMaxCount, effectiveMinTime
}

// Execute runs the AllocKiller attack cycle.
func (a *AllocKiller) Execute(nomadClient *nomad.Client, cfg config.Config) error {
	a.logger.Info("Starting execution cycle")

	var lastError error // Variable to hold the last error encountered

	// Select potential targets using the nomad client method
	targets, err := nomadClient.SelectTargetAllocations(cfg)
	if err != nil {
		a.logger.Error("Failed to select target allocations", "error", err)
		return fmt.Errorf("failed to select target allocations: %w", err) // Return error
	}

	numTargets := len(targets)
	if numTargets == 0 {
		a.logger.Info("No target allocations found matching criteria.")
		return nil
	}
	a.logger.Info("Found potential target allocations", "count", numTargets)

	// Calculate the *global* kill limit for this cycle based on config
	globalKillLimit := calculateLimit(cfg.MaxKillCount, cfg.MaxKillPercent, numTargets)
	if globalKillLimit == 0 {
		a.logger.Info("Global kill limit calculated as 0. Skipping termination cycle.", "max_count", cfg.MaxKillCount, "max_percent", cfg.MaxKillPercent, "total_targets", numTargets)
		return nil
	}
	a.logger.Debug("Calculated global kill limit for this cycle", "limit", globalKillLimit, "max_count", cfg.MaxKillCount, "max_percent", cfg.MaxKillPercent, "total_targets", numTargets)

	killedCount := 0
	rand.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })

	for _, allocStub := range targets {
		// Create a logger specific to this allocation attempt
		allocLogger := a.logger.With("alloc_id", allocStub.ID, "job_id", allocStub.JobID, "node_id", allocStub.NodeID, "node_name", allocStub.NodeName)

		if killedCount >= globalKillLimit {
			allocLogger.Info("Reached global kill limit for this cycle.", "limit", globalKillLimit)
			break // Stop processing more targets
		}

		// --- Get Effective Config & Apply Per-Job/Effective Limits ---
		effectiveBaseProb, effectiveMTBK, effectiveMaxCount, effectiveMinTime := getEffectiveAllocKillParams(&cfg, allocStub.JobID)
		jobKillsSoFar := a.jobKillCounts[allocStub.JobID]

		// Determine the actual probability to use (MTBK overrides probability)
		var probabilityToUse float64
		mtbkInUse := false
		if effectiveMTBK > 0 {
			// Calculate probability from MTBK and Interval
			// Ensure no division by zero and MTBK >= Interval (validated in config)
			if cfg.Interval > 0 {
				probabilityToUse = float64(cfg.Interval) / float64(effectiveMTBK)
				mtbkInUse = true
			} else {
				// Interval is zero or negative, cannot calculate probability from MTBK
				a.logger.Warn("Cannot use MTBK for probability calculation because interval is zero or negative", "job_id", allocStub.JobID, "interval", cfg.Interval)
				probabilityToUse = 0 // Effectively disable kills based on MTBK if interval is invalid
			}
		} else {
			// MTBK not set, use the configured KillProbability
			probabilityToUse = effectiveBaseProb
		}

		allocLogger = allocLogger.With(
			"effective_prob", probabilityToUse,
			"mtbk_in_use", mtbkInUse,
			"effective_mtbk", effectiveMTBK.String(),
			"effective_max_count", effectiveMaxCount,
			"effective_min_time", effectiveMinTime.String(),
			"job_kills_so_far", jobKillsSoFar,
		)

		// 1. Check Per-Job MaxKillCount Limit
		if effectiveMaxCount >= 0 && jobKillsSoFar >= effectiveMaxCount {
			allocLogger.Debug("Skipping: Job-specific MaxKillCount limit reached.")
			continue
		}

		// 2. Check Per-Job MinTimeBetweenKills Cooldown
		if lastKill, ok := a.lastKillTimes[allocStub.JobID]; ok {
			elapsed := time.Since(lastKill)
			if elapsed < effectiveMinTime {
				allocLogger.Debug("Skipping: Job-specific MinTimeBetweenKills cooldown active", "elapsed", elapsed.Round(time.Second))
				continue
			}
		}

		// Also check the global tracker for MinTimeBetweenKills
		lastGlobalKillTime, found := a.tracker.GetLastActionTime(allocStub.ID)
		if found && time.Since(lastGlobalKillTime) < cfg.MinTimeBetweenKills {
			allocLogger.Debug("Skipping: Global minimum time between kills not elapsed", "global_min_time", cfg.MinTimeBetweenKills, "last_kill_time", lastGlobalKillTime.Format(time.RFC3339))
			continue
		}

		// 3. Check Kill Probability using the calculated probabilityToUse
		randomDraw := rand.Float64()
		if randomDraw >= probabilityToUse { // Use the derived probability
			allocLogger.Debug("Skipping: Random draw >= effective probability", "draw", randomDraw)
			continue
		}

		killedCount++
		allocLogger = allocLogger.With("attempt_num", killedCount, "global_limit", globalKillLimit)

		if cfg.DryRun {
			allocLogger.Warn("Dry Run: Would terminate allocation")
			a.jobKillCounts[allocStub.JobID]++
			a.lastKillTimes[allocStub.JobID] = time.Now()
			if err := a.tracker.RecordAction("allocation", allocStub.ID, "killed (dry run)"); err != nil {
				allocLogger.Warn("Failed to record kill action (dry run) in tracker", "error", err)
				// Don't treat tracker error as cycle error for circuit breaker
			}
		} else {
			allocLogger.Warn("Attempting to terminate allocation")
			err := nomadClient.StopAllocation(allocStub.ID)
			if err != nil {
				allocLogger.Error("Failed to stop allocation", "error", err)
				killedCount--   // Decrement count as the kill failed
				lastError = err // Store the error
				continue        // Move to next target alloc but record the error
			}

			allocLogger.Info("Successfully terminated allocation")
			a.jobKillCounts[allocStub.JobID]++
			a.lastKillTimes[allocStub.JobID] = time.Now()
			if err := a.tracker.RecordAction("allocation", allocStub.ID, "killed"); err != nil {
				allocLogger.Warn("Failed to record kill action in tracker", "error", err)
				// Don't treat tracker error as cycle error for circuit breaker
			}
		}
	}

	return lastError // Return the last error encountered during the cycle, if any
}
