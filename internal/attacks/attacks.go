package attacks

import (
	"math"

	"github.com/kyleseneker/nomad-nemesis/internal/config"
	"github.com/kyleseneker/nomad-nemesis/internal/nomad"
)

// Attack defines the interface for a chaos attack type.
type Attack interface {
	// Name returns the user-friendly name of the attack.
	Name() string
	// IsEnabled checks if this attack is enabled in the configuration.
	IsEnabled(cfg config.Config) bool
	// Execute runs the attack logic for one cycle.
	Execute(nomadClient *nomad.Client, cfg config.Config) error
}

// calculateLimit determines the maximum number of items to affect based on count/percent config.
// Kept here as it might be useful for future attack types.
func calculateLimit(maxCount, maxPercent, totalTargets int) int {
	if totalTargets == 0 {
		return 0 // Cannot affect anything if there are no targets
	}

	limit := totalTargets // Default to unlimited (affect all potential targets)

	// Apply percentage limit if set
	if maxPercent >= 0 {
		percentLimit := int(math.Ceil((float64(maxPercent) / 100.0) * float64(totalTargets)))
		limit = percentLimit
	}

	// Apply count limit if set, taking the minimum of count and percentage limits
	if maxCount >= 0 {
		if maxPercent >= 0 { // Both limits are set, take the minimum
			if maxCount < limit {
				limit = maxCount
			}
		} else { // Only count limit is set
			limit = maxCount
		}
	}

	// Ensure limit doesn't exceed the total number of targets
	if limit > totalTargets {
		limit = totalTargets
	}

	return limit
}
