package tracker

import (
	"time"
)

// ActionRecord stores information about a performed action.
type ActionRecord struct {
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Action     string    `json:"action"`
	Timestamp  time.Time `json:"timestamp"`
}

// Tracker defines the interface for recording actions and checking history.
type Tracker interface {
	RecordAction(targetType string, targetID string, action string) error
	GetLastActionTime(targetID string) (time.Time, bool)
	Close() error
}
