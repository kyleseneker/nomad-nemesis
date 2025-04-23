package cmd

import (
	"github.com/spf13/cobra"
	// We might need viper here later if flags are bound to root
)

var (
	// Used for flags.
	cfgFile        string
	dryRunOverride bool

	rootCmd = &cobra.Command{
		Use:   "nomad-nemesis",
		Short: "A Chaos Monkey clone for HashiCorp Nomad",
		Long: `Nomad Nemesis periodically introduces failures into a Nomad cluster
for chaos engineering purposes. It can terminate allocations or drain nodes
based on configured probabilities, schedules, and safety limits.`,
		// Uncomment the following line if your bare application
		// has an action associated with it:
		// Run: func(cmd *cobra.Command, args []string) { },
	}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Define flags persistent across all commands
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.nomad-nemesis/nomad-nemesis.toml or ./nomad-nemesis.toml)")
	rootCmd.PersistentFlags().BoolVar(&dryRunOverride, "dry-run", false, "Override the dry_run setting in the config file.")

	// Add subcommands here
	// rootCmd.AddCommand(runCmd)
	// rootCmd.AddCommand(eligibleCmd)
}
