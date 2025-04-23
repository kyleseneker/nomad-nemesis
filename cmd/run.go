package cmd

import (
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/kyleseneker/nomad-nemesis/internal/config"
	"github.com/kyleseneker/nomad-nemesis/internal/logging"
	"github.com/kyleseneker/nomad-nemesis/internal/nomad"
	"github.com/kyleseneker/nomad-nemesis/internal/runner"
	"github.com/kyleseneker/nomad-nemesis/internal/tracker"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Nomad Nemesis in continuous chaos mode",
	Long: `Starts the Nomad Nemesis runner which periodically executes
configured chaos attacks (allocation killing, node draining) against
the target Nomad cluster according to the defined schedule, probabilities,
and safety limits.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Get flag values defined in root.go
		// These flags are parsed by Cobra automatically before Run is called.
		// The cfgFile and dryRunOverride variables are already populated.
		runNemesis(cfgFile, dryRunOverride)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	// No command-specific flags needed here for now
}

// runNemesis contains the main logic, now accepting flags
func runNemesis(configPath string, dryRunFlag bool) {
	// Load config first, passing the flag values
	cfg, err := config.LoadConfig(configPath, dryRunFlag)
	if err != nil {
		// Use standard log here since logger isn't initialized yet
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Initialize Logger using loaded config
	logging.InitializeLogger(cfg)
	logger := logging.Get()

	rand.Seed(time.Now().UnixNano())

	// Dry-run logic is now handled inside LoadConfig based on the passed flag
	// Remove the check here:
	// if rootCmd.PersistentFlags().Changed("dry-run") {
	// 	cfg.DryRun = dryRunOverride
	// }

	logger.Info("Configuration loaded", "dry_run", cfg.DryRun)
	logger.Info("Effective Run Window", "days", cfg.RunDays, "start", cfg.RunStartTime, "end", cfg.RunEndTime, "tz", cfg.RunTimeLocation)

	nomadClient, err := nomad.NewClient(cfg)
	if err != nil {
		logger.Error("Error creating Nomad client", "error", err)
		os.Exit(1)
	}
	logger.Info("Nomad client created successfully")

	var chaosTracker tracker.Tracker // Use the interface type
	switch cfg.TrackerType {
	case "file":
		chaosTracker, err = tracker.NewFileTracker(cfg.HistoryDir)
	case "sql":
		chaosTracker, err = tracker.NewSqlTracker(cfg.TrackerDSN)
		// TODO: Implement graceful shutdown to call chaosTracker.(*tracker.SqlTracker).Close()
	default:
		logger.Error("Invalid tracker_type specified", "tracker_type", cfg.TrackerType)
		os.Exit(1)
	}

	if err != nil {
		logger.Error("Error initializing tracker", "type", cfg.TrackerType, "error", err)
		os.Exit(1)
	}
	logger.Info("Tracker initialized successfully", "type", cfg.TrackerType)

	chaosRunner := runner.NewRunner(*cfg, nomadClient, chaosTracker, logger)
	logger.Info("Chaos runner initialized. Starting main loop...")

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go chaosRunner.Run()

	sig := <-signalChan
	logger.Warn("Received signal, initiating shutdown...", "signal", sig)

	chaosRunner.Shutdown()

	logger.Info("Nomad Nemesis shut down gracefully.")
}
