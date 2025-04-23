package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/olekukonko/tablewriter" // Using a table library for nice output
	"github.com/spf13/cobra"

	"github.com/kyleseneker/nomad-nemesis/internal/config"
	"github.com/kyleseneker/nomad-nemesis/internal/nomad"
)

// eligibleCmd represents the eligible command
var eligibleCmd = &cobra.Command{
	Use:   "eligible",
	Short: "List allocations and nodes eligible for chaos attacks based on config",
	Long: `Loads the current configuration and queries the Nomad cluster
to identify which allocations and/or nodes would be considered potential
targets for attacks, respecting targeting rules and exclusions.

It does NOT check scheduling windows, probabilities, cooldowns, or limits.
This command is primarily for visibility and debugging configuration.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Get flag values defined in root.go
		// The cfgFile and dryRunOverride variables are already populated.
		listEligibleTargets(cfgFile, dryRunOverride)
	},
}

func init() {
	rootCmd.AddCommand(eligibleCmd)
}

func listEligibleTargets(configPath string, dryRunFlag bool) { // Accept flags as arguments
	// Pass flag values to LoadConfig
	cfg, err := config.LoadConfig(configPath, dryRunFlag)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}
	// Use a more informative log message, potentially including dry-run status if relevant
	log.Printf("Configuration loaded (DryRun: %v).\n", cfg.DryRun)

	nomadClient, err := nomad.NewClient(cfg)
	if err != nil {
		log.Fatalf("Error creating Nomad client: %v", err)
	}
	log.Println("Nomad client created successfully.")

	fmt.Println("\n--- Eligible Allocations for Killing ---")
	if len(cfg.TargetJobs) == 0 {
		fmt.Println("Allocation killing not configured (no target_jobs specified).")
	} else {
		allocs, err := nomadClient.SelectTargetAllocations(*cfg)
		if err != nil {
			log.Fatalf("Error selecting eligible allocations: %v", err)
		}

		if len(allocs) == 0 {
			fmt.Println("No allocations found matching the target/exclude criteria.")
		} else {
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Alloc ID", "Job ID", "Task Group", "Node Name", "Node ID"})
			table.SetBorder(false)

			for _, alloc := range allocs {
				table.Append([]string{
					alloc.ID,
					alloc.JobID,
					alloc.TaskGroup,
					alloc.NodeName,
					alloc.NodeID,
				})
			}
			table.Render()
		}
	}

	fmt.Println("\n--- Eligible Nodes for Draining ---")
	if !cfg.EnableNodeDrain {
		fmt.Println("Node draining not enabled in configuration.")
	} else {
		nodes, err := nomadClient.SelectTargetNodes(*cfg)
		if err != nil {
			log.Fatalf("Error selecting eligible nodes: %v", err)
		}

		if len(nodes) == 0 {
			fmt.Println("No nodes found matching the target/exclude criteria.")
		} else {
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Node ID", "Node Name", "Datacenter", "Status", "Version"})
			table.SetBorder(false)

			for _, node := range nodes {
				table.Append([]string{
					node.ID,
					node.Name,
					node.Datacenter,
					node.Status,
					node.Version,
				})
			}
			table.Render()
		}
	}

}
