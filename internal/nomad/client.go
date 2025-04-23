package nomad

import (
	"fmt"
	"strings"

	api "github.com/hashicorp/nomad/api"
	"github.com/kyleseneker/nomad-nemesis/internal/config"
	"github.com/kyleseneker/nomad-nemesis/internal/logging"
)

// Client wraps the Nomad API client.
type Client struct {
	api    *api.Client
	logger logging.Logger
}

// NewClient creates a new Nomad API client wrapper.
func NewClient(cfg *config.Config) (*Client, error) {
	nomadConfig := api.DefaultConfig()
	if cfg.NomadAddr != "" {
		nomadConfig.Address = cfg.NomadAddr
	}
	// TODO: Add other config options like TLS, token, etc.

	client, err := api.NewClient(nomadConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating Nomad API client: %w", err)
	}
	// Get the application logger interface and create a sub-logger
	logger := logging.Get().Named("nomad_client")
	return &Client{api: client, logger: logger}, nil
}

// Raw returns the underlying official Nomad API client.
// Use this for accessing endpoints not explicitly wrapped by this client.
func (c *Client) Raw() *api.Client {
	return c.api
}

// Helper function to check if a string is in a slice of strings.
func contains(slice []string, item string) bool {
	set := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		set[s] = struct{}{}
	}
	_, ok := set[item]
	return ok
}

// SelectTargetAllocations finds running allocations matching criteria, excluding specified jobs and suffixes.
func (c *Client) SelectTargetAllocations(cfg config.Config) ([]*api.AllocationListStub, error) {
	potentialAllocations := []*api.AllocationListStub{}
	allocClient := c.api.Allocations()

	for _, jobID := range cfg.TargetJobs {
		// Skip this target job if it's explicitly excluded
		if contains(cfg.ExcludeJobs, jobID) {
			c.logger.Debug("Skipping target job because it is in the exclusion list.", "job_id", jobID)
			continue
		}
		// Skip this target job if it matches an excluded suffix
		hasExcludedSuffix := false
		for _, suffix := range cfg.ExcludeJobSuffixes {
			if strings.HasSuffix(jobID, suffix) {
				c.logger.Debug("Skipping target job because it matches excluded suffix.", "job_id", jobID, "suffix", suffix)
				hasExcludedSuffix = true
				break
			}
		}
		if hasExcludedSuffix {
			continue
		}

		// Filter for running allocations for the specific job ID
		filter := fmt.Sprintf("JobID == %q and ClientStatus == \"running\"", jobID)
		jobAllocs, _, err := allocClient.List(&api.QueryOptions{Filter: filter})
		if err != nil {
			c.logger.Warn("Failed to list allocations for job", "job_id", jobID, "filter", filter, "error", err)
			continue
		}

		for _, alloc := range jobAllocs {
			// Filter by task group if specified (target groups)
			if len(cfg.TargetGroups) > 0 && !contains(cfg.TargetGroups, alloc.TaskGroup) {
				continue // Skip if not in the target group list
			}
			potentialAllocations = append(potentialAllocations, alloc)
		}
	}

	return potentialAllocations, nil
}

// SelectTargetNodes finds potentially drainable nodes matching criteria, excluding specified node names.
func (c *Client) SelectTargetNodes(cfg config.Config) ([]*api.NodeListStub, error) {
	nodesClient := c.api.Nodes()

	// Build the filter string based on status, type, and attributes
	var filterParts []string
	filterParts = append(filterParts, "Status == \"ready\"")
	filterParts = append(filterParts, "NodeType == \"client\"")
	for key, value := range cfg.TargetNodeAttrs {
		filterParts = append(filterParts, fmt.Sprintf("Attributes.%s == %q", key, value))
	}
	filterString := strings.Join(filterParts, " and ")
	c.logger.Debug("Selecting target nodes", "filter", filterString)

	// Get nodes matching API filter
	potentialNodes, _, err := nodesClient.List(&api.QueryOptions{Filter: filterString})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes with filter %q: %w", filterString, err)
	}

	// Post-filter based on excluded node names
	finalNodes := []*api.NodeListStub{}
	excludedCount := 0
	for _, node := range potentialNodes {
		if contains(cfg.ExcludeNodes, node.Name) {
			excludedCount++
			continue
		}
		finalNodes = append(finalNodes, node)
	}

	if excludedCount > 0 {
		c.logger.Info("Excluded nodes based on node name exclusion list.", "count", excludedCount)
	}

	return finalNodes, nil
}

// SelectStressJobLauncherTargets finds potential target nodes for the stress job launcher attack.
// It's similar to SelectTargetNodes but uses the StressJobLauncher config section.
func (c *Client) SelectStressJobLauncherTargets(cfg config.Config) ([]*api.NodeListStub, error) {
	attackCfg := cfg.StressJobLauncher // Use the correct config section
	nodesClient := c.api.Nodes()

	// Build the filter string based on status, type, and attributes
	var filterParts []string
	filterParts = append(filterParts, "Status == \"ready\"")
	filterParts = append(filterParts, "NodeType == \"client\"")
	for key, value := range attackCfg.TargetNodeAttrs {
		filterParts = append(filterParts, fmt.Sprintf("Attributes.%s == %q", key, value))
	}
	filterString := strings.Join(filterParts, " and ")
	c.logger.Debug("Selecting target nodes for resource pressure", "filter", filterString)

	// Get nodes matching API filter
	potentialNodes, _, err := nodesClient.List(&api.QueryOptions{Filter: filterString})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes with filter %q: %w", filterString, err)
	}

	// Further filter based on nodes running target jobs (if specified)
	var nodesFromJobs map[string]bool
	if len(attackCfg.TargetJobs) > 0 {
		nodesFromJobs = make(map[string]bool)
		allocs, _, err := c.api.Allocations().List(&api.QueryOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list all allocations to find nodes for target jobs: %w", err)
		}
		for _, alloc := range allocs {
			if alloc.ClientStatus == "running" && contains(attackCfg.TargetJobs, alloc.JobID) {
				nodesFromJobs[alloc.NodeID] = true
			}
		}
		// If target_jobs is specified, we ONLY consider nodes found via this method OR matching TargetNodeAttrs
	}

	// Post-filter based on excluded node names AND target jobs
	finalNodes := []*api.NodeListStub{}
	excludedCount := 0
	targetJobFilteredCount := 0
	for _, node := range potentialNodes {
		// Check exclusion list
		if contains(attackCfg.ExcludeNodes, node.Name) {
			excludedCount++
			continue
		}

		// If target_jobs were specified, check if this node is running one OR if it matches direct attrs
		if nodesFromJobs != nil { // target_jobs were specified
			_, runningTargetJob := nodesFromJobs[node.ID]
			matchesDirectAttrs := len(attackCfg.TargetNodeAttrs) > 0 // Already pre-filtered by API query
			if !runningTargetJob && !matchesDirectAttrs {
				targetJobFilteredCount++
				continue // Node doesn't match direct attrs and isn't running a target job
			}
		}

		finalNodes = append(finalNodes, node)
	}

	if excludedCount > 0 {
		c.logger.Info("Excluded nodes based on node name exclusion list.", "count", excludedCount)
	}
	if targetJobFilteredCount > 0 {
		c.logger.Info("Filtered out nodes not running target jobs (when target_jobs specified).", "count", targetJobFilteredCount)
	}

	return finalNodes, nil
}

// --- Action Primitives ---
// These functions perform the raw API actions and will be used by the 'attacks' package.

// StopAllocation stops a specific allocation by ID.
func (c *Client) StopAllocation(allocID string) error {
	allocClient := c.api.Allocations()

	// Need to fetch the full allocation object to stop it
	alloc, _, err := allocClient.Info(allocID, nil) // Use nil for default QueryOptions
	if err != nil {
		// Don't return error immediately, maybe alloc is already gone.
		// Log it, the attack logic can decide how to handle.
		c.logger.Info("Failed to get info for alloc before stopping (may already be stopped)", "alloc_id", allocID, "error", err)
		// Return nil so the attack logic doesn't bail prematurely if the alloc disappeared
		// between listing and stopping.
		return nil
	}

	_, err = allocClient.Stop(alloc, nil) // Use nil for default WriteOptions
	if err != nil {
		return fmt.Errorf("failed to stop allocation %s: %w", allocID, err)
	}
	c.logger.Info("Successfully stopped allocation via API", "alloc_id", allocID)
	return nil
}

// DrainNode initiates drain on a specific node by ID.
func (c *Client) DrainNode(nodeID string) error {
	nodesClient := c.api.Nodes()

	drainSpec := &api.DrainSpec{
		Deadline:         0, // Immediate drain
		IgnoreSystemJobs: false,
	}
	markEligible := false // Keep draining

	_, err := nodesClient.UpdateDrain(nodeID, drainSpec, markEligible, nil) // Use nil for WriteOptions
	if err != nil {
		return fmt.Errorf("failed to drain node %s: %w", nodeID, err)
	}
	c.logger.Info("Successfully initiated drain for node via API", "node_id", nodeID)
	return nil
}

// RegisterJob registers a job definition with Nomad.
func (c *Client) RegisterJob(job *api.Job) (*api.JobRegisterResponse, error) {
	resp, _, err := c.api.Jobs().Register(job, nil) // Use nil for default WriteOptions
	if err != nil {
		return nil, fmt.Errorf("failed to register job %q: %w", *job.ID, err)
	}
	c.logger.Debug("Successfully registered job via API", "job_id", *job.ID)
	return resp, nil
}

// GetJobStatus retrieves the status of a specific job by ID.
func (c *Client) GetJobStatus(jobID string) (string, error) {
	job, _, err := c.api.Jobs().Info(jobID, nil) // Ignore QueryMeta
	if err != nil {
		// Simple check for "not found" errors, treating them as dead/GC'd
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			c.logger.Debug("Job not found when checking status, assuming dead/gc'd", "job_id", jobID)
			return "dead", nil
		}
		// Other error fetching job info
		return "", fmt.Errorf("failed to get info for job %q: %w", jobID, err)
	}
	// Handle case where job or status might be nil pointers
	if job == nil || job.Status == nil {
		c.logger.Warn("Job or Job.Status pointer was nil, treating as unknown/dead", "job_id", jobID)
		return "dead", nil // Treat nil job/status as dead/unknown
	}
	return *job.Status, nil
}
