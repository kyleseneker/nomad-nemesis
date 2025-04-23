package runner

import (
	"strings"
	"time"

	"github.com/kyleseneker/nomad-nemesis/internal/attacks"
	"github.com/kyleseneker/nomad-nemesis/internal/config"
	"github.com/kyleseneker/nomad-nemesis/internal/logging"
	"github.com/kyleseneker/nomad-nemesis/internal/nomad"
	"github.com/kyleseneker/nomad-nemesis/internal/outage"
	"github.com/kyleseneker/nomad-nemesis/internal/tracker"
)

// Runner manages the main execution loop.
type Runner struct {
	cfg           config.Config
	nomadClient   *nomad.Client
	tracker       tracker.Tracker
	outageChecker outage.OutageChecker
	attacks       []attacks.Attack
	logger        logging.Logger
	// Circuit Breaker State
	consecutiveErrors int
	breakerTripped    bool
	breakerTripTime   time.Time
	stopChan          chan struct{}
}

// NewRunner creates a new Runner.
func NewRunner(cfg config.Config, nomadClient *nomad.Client, chaosTracker tracker.Tracker, logger logging.Logger) *Runner {
	var oc outage.OutageChecker
	if !cfg.OutageCheckEnabled {
		logger.Info("Outage checking disabled via outage_check_enabled=false.")
		oc = outage.NewNoopOutageChecker()
	} else {
		switch cfg.OutageCheckerType {
		case "nomad":
			logger.Info("Using Nomad outage checker.")
			oc = outage.NewNomadOutageChecker(nomadClient)
		case "none":
			logger.Info("Outage checking disabled via outage_checker_type='none'.")
			oc = outage.NewNoopOutageChecker()
		default:
			// This case should be prevented by config validation
			logger.Error("Invalid outage_checker_type configured, defaulting to 'none'", "type", cfg.OutageCheckerType)
			oc = outage.NewNoopOutageChecker()
		}
	}

	registeredAttacks := []attacks.Attack{
		attacks.NewAllocKiller(chaosTracker),
		attacks.NewNodeDrainer(chaosTracker),
		attacks.NewStressJobLauncher(chaosTracker),
	}

	return &Runner{
		cfg:               cfg,
		nomadClient:       nomadClient,
		tracker:           chaosTracker,
		outageChecker:     oc,
		attacks:           registeredAttacks,
		logger:            logger.Named("runner"),
		consecutiveErrors: 0,
		breakerTripped:    false,
		breakerTripTime:   time.Time{},
		stopChan:          make(chan struct{}),
	}
}

// Run starts the main execution loop and blocks until Shutdown is called.
func (r *Runner) Run() {
	r.logger.Info("Starting chaos runner", "interval", r.cfg.Interval, "window_days", r.cfg.RunDays, "window_start", r.cfg.RunStartTime, "window_end", r.cfg.RunEndTime, "window_tz", r.cfg.RunTimeLocation)
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	// Run once immediately at startup
	r.runChaosCycle()

Loop:
	for {
		select {
		case <-ticker.C:
			r.runChaosCycle()
		case <-r.stopChan:
			r.logger.Info("Shutdown signal received, stopping runner loop.")
			break Loop
		}
	}

	r.logger.Info("Runner loop stopped.")
}

// Shutdown gracefully stops the runner.
func (r *Runner) Shutdown() {
	r.logger.Info("Initiating runner shutdown...")

	close(r.stopChan)

	if err := r.tracker.Close(); err != nil {
		r.logger.Error("Error closing tracker", "error", err)
	}

	r.logger.Info("Runner shutdown complete.")
}

// checkRunWindow checks if the current time is within the allowed schedule.
func (r *Runner) checkRunWindow() bool {
	now := time.Now().In(r.cfg.RunTimeLocation)
	currentDay := now.Weekday()
	currentTimeStr := now.Format("15:04") // HH:MM format

	// Check if today is an allowed day
	isAllowedDay := false
	for _, allowedDay := range r.cfg.RunDays {
		if currentDay == allowedDay {
			isAllowedDay = true
			break
		}
	}
	if !isAllowedDay {
		r.logger.Debug("Skipping cycle: Not an allowed day", "current_day", currentDay, "allowed_days", r.cfg.RunDays)
		return false
	}

	// Check if current time is within the allowed time window
	if currentTimeStr < r.cfg.RunStartTime || currentTimeStr >= r.cfg.RunEndTime {
		r.logger.Debug("Skipping cycle: Outside allowed time range", "current_time", currentTimeStr, "start_time", r.cfg.RunStartTime, "end_time", r.cfg.RunEndTime)
		return false
	}

	r.logger.Debug("Current time is within the allowed run window.")
	return true
}

// runChaosCycle performs one iteration of checking and executing enabled attacks.
func (r *Runner) runChaosCycle() {
	cycleLogger := r.logger.With("cycle_start_time", time.Now().UTC().Format(time.RFC3339))
	cycleLogger.Info("Starting chaos run cycle...")

	// Check -1: Is the circuit breaker tripped?
	if r.cfg.CircuitBreakerEnabled && r.breakerTripped {
		elapsedSinceTrip := time.Since(r.breakerTripTime)
		if elapsedSinceTrip < r.cfg.CircuitBreakerResetInterval {
			cycleLogger.Warn("Circuit breaker is tripped. Skipping cycle.", "tripped_since", elapsedSinceTrip.Round(time.Second), "reset_interval", r.cfg.CircuitBreakerResetInterval)
			cycleLogger.Info("Chaos run cycle finished", "status", "skipped_breaker_tripped")
			return
		} else {
			cycleLogger.Warn("Circuit breaker reset interval elapsed. Resetting breaker and proceeding.", "reset_interval", r.cfg.CircuitBreakerResetInterval)
			r.breakerTripped = false
			r.consecutiveErrors = 0
		}
	}

	// Check 0: Is Nemesis globally enabled?
	if !r.cfg.Enabled {
		cycleLogger.Warn("Nomad Nemesis is globally disabled (enabled=false). Skipping cycle.")
		cycleLogger.Info("Chaos run cycle finished", "status", "skipped_disabled")
		return
	}

	// Check 1: Is there an outage?
	isOutage, err := r.outageChecker.IsOutage()
	if err != nil {
		cycleLogger.Error("Error checking for outage, skipping cycle", "error", err)
		isOutage = true // Treat as outage for safety
	}
	if isOutage {
		cycleLogger.Warn("System outage detected (or check failed). Skipping cycle.")
		cycleLogger.Info("Chaos run cycle finished", "status", "skipped_outage")
		return
	}

	// Check 2: Are we within the allowed run window?
	if !r.checkRunWindow() {
		// Debug logging happens inside checkRunWindow
		cycleLogger.Info("Chaos run cycle finished", "status", "skipped_schedule")
		return
	}

	cycleLogger.Info("Proceeding with attacks...")

	cycleHadError := false
	for _, attack := range r.attacks {
		// Ensure logger name is safe for file systems / metrics
		attackName := strings.ToLower(strings.ReplaceAll(attack.Name(), " ", "_"))
		attackLogger := cycleLogger.Named(attackName)
		if attack.IsEnabled(r.cfg) {
			attackLogger.Info("Executing attack...")
			err := attack.Execute(r.nomadClient, r.cfg)
			if err != nil {
				attackLogger.Error("Error executing attack", "error", err)
				cycleHadError = true
			} else {
				attackLogger.Info("Attack execution finished successfully.")
			}
		} else {
			attackLogger.Debug("Skipping disabled attack")
		}
	}

	// Update circuit breaker state based on cycle outcome
	if r.cfg.CircuitBreakerEnabled {
		if cycleHadError {
			r.consecutiveErrors++
			cycleLogger.Warn("Chaos cycle completed with errors", "consecutive_errors", r.consecutiveErrors, "threshold", r.cfg.CircuitBreakerThreshold)
			if r.consecutiveErrors >= r.cfg.CircuitBreakerThreshold {
				if !r.breakerTripped { // Avoid logging trip message repeatedly
					cycleLogger.Error("Circuit breaker threshold reached! Tripping breaker.", "threshold", r.cfg.CircuitBreakerThreshold, "reset_interval", r.cfg.CircuitBreakerResetInterval)
					r.breakerTripped = true
					r.breakerTripTime = time.Now()
				}
			}
		} else {
			if r.consecutiveErrors > 0 {
				cycleLogger.Info("Chaos cycle completed successfully, resetting consecutive error count.", "previous_error_count", r.consecutiveErrors)
			}
			r.consecutiveErrors = 0
		}
	}

	cycleLogger.Info("Chaos run cycle finished", "status", "executed", "had_errors", cycleHadError)
}
