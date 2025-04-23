package outage

import (
	"fmt"

	"github.com/kyleseneker/nomad-nemesis/internal/logging"
	"github.com/kyleseneker/nomad-nemesis/internal/nomad"
)

// OutageChecker defines the interface for checking system health.
type OutageChecker interface {
	// IsOutage returns true if the system is considered to be in an outage state.
	IsOutage() (bool, error)
}

// --- Nomad Health Checker ---

// NomadOutageChecker checks the health of the Nomad cluster itself.
type NomadOutageChecker struct {
	nomadClient *nomad.Client
	logger      logging.Logger
}

// NewNomadOutageChecker creates a new checker.
func NewNomadOutageChecker(client *nomad.Client) *NomadOutageChecker {
	return &NomadOutageChecker{
		nomadClient: client,
		logger:      logging.Get().Named("nomad_outage_checker"),
	}
}

// IsOutage checks if the Nomad cluster appears healthy.
// Basic check: Verify we can list members and that a majority are alive.
func (c *NomadOutageChecker) IsOutage() (bool, error) {
	c.logger.Info("Checking Nomad cluster health for potential outage...")

	rawClient := c.nomadClient.Raw()

	members, err := rawClient.Agent().Members()
	if err != nil {
		c.logger.Error("Outage detected: Failed to list Nomad agent members", "error", err)
		return true, fmt.Errorf("failed to list Nomad agent members: %w", err)
	}

	liveServers := 0
	totalServers := 0
	for _, member := range members.Members {
		if member.Tags["role"] == "nomad" { // Check if it's a server
			totalServers++
			if member.Status == "alive" {
				liveServers++
			}
		}
	}

	if totalServers == 0 {
		c.logger.Warn("Outage detected: Could not find any Nomad servers in member list.")
		return true, fmt.Errorf("no Nomad servers found in member list")
	}

	// Define outage threshold: simple majority of servers must be alive.
	requiredServers := (totalServers / 2) + 1
	if liveServers < requiredServers {
		c.logger.Warn("Outage detected: Insufficient live servers.", "live", liveServers, "total", totalServers, "required", requiredServers)
		return true, fmt.Errorf("only %d/%d servers alive", liveServers, totalServers)
	}

	c.logger.Info("Nomad cluster health check passed.", "live", liveServers, "total", totalServers)
	return false, nil
}

// --- Noop Checker ---

// NoopOutageChecker always returns false (no outage).
type NoopOutageChecker struct {
	logger logging.Logger
}

// NewNoopOutageChecker creates a checker that never detects an outage.
func NewNoopOutageChecker() *NoopOutageChecker {
	return &NoopOutageChecker{
		logger: logging.Get().Named("noop_outage_checker"),
	}
}

// IsOutage always returns false.
func (c *NoopOutageChecker) IsOutage() (bool, error) {
	c.logger.Info("Outage check disabled.")
	return false, nil
}
