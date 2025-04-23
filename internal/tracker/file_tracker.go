package tracker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kyleseneker/nomad-nemesis/internal/logging"
)

const defaultFileName = "nomad-nemesis-history.json"

// FileTracker persists action history to a JSON file.
type FileTracker struct {
	mu         sync.RWMutex
	filePath   string
	historyMap map[string]ActionRecord // Maps TargetID -> Last Action Record (for fast lookup)
	records    []ActionRecord          // Holds all records read/written (for full history)
	logger     logging.Logger          // Use the interface type
}

// NewFileTracker creates or loads a tracker using a file.
func NewFileTracker(dir string) (*FileTracker, error) {
	logger := logging.Get().Named("file_tracker") // Get() returns logging.Logger
	if dir == "" {
		dir = "."
	}
	filePath := filepath.Join(dir, defaultFileName)

	t := &FileTracker{
		filePath:   filePath,
		historyMap: make(map[string]ActionRecord),
		records:    []ActionRecord{},
		logger:     logger,
	}

	if err := t.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load tracker history from %s: %w", filePath, err)
	}

	t.logger.Info("FileTracker initialized.", "path", filePath, "loaded_records", len(t.records))
	return t, nil
}

// load reads the history file and populates both the map and the record list.
func (t *FileTracker) load() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return err // Handles os.IsNotExist
	}

	if len(data) == 0 {
		t.historyMap = make(map[string]ActionRecord)
		t.records = []ActionRecord{}
		return nil
	}

	var loadedRecords []ActionRecord
	if err := json.Unmarshal(data, &loadedRecords); err != nil {
		return fmt.Errorf("failed to unmarshal history file %s: %w", t.filePath, err)
	}

	t.records = loadedRecords // Store the full list

	// Rebuild the map, storing only the latest action per target ID
	historyMap := make(map[string]ActionRecord)
	for _, rec := range t.records { // Iterate over the loaded records
		if current, exists := historyMap[rec.TargetID]; !exists || rec.Timestamp.After(current.Timestamp) {
			historyMap[rec.TargetID] = rec
		}
	}
	t.historyMap = historyMap
	return nil
}

// save writes the current full record list back to the file.
func (t *FileTracker) save() error {
	// Marshal the full list of records, not just the map
	data, err := json.MarshalIndent(t.records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	// Write atomically via temp file rename
	tempFilePath := t.filePath + ".tmp"
	if err := os.WriteFile(tempFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp history file %s: %w", tempFilePath, err)
	}

	if err := os.Rename(tempFilePath, t.filePath); err != nil {
		_ = os.Remove(tempFilePath) // Attempt cleanup on rename error
		return fmt.Errorf("failed to rename temp history file to %s: %w", t.filePath, err)
	}

	return nil
}

// RecordAction records that an action was performed on a target.
func (t *FileTracker) RecordAction(targetType string, targetID string, action string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UTC()
	record := ActionRecord{
		TargetType: targetType,
		TargetID:   targetID,
		Action:     action,
		Timestamp:  now,
	}

	// Update the map for fast lookups
	t.historyMap[targetID] = record
	// Append to the full list for persistence
	t.records = append(t.records, record)

	if err := t.save(); err != nil {
		// Log error but don't necessarily fail the operation
		t.logger.Warn("Failed to save tracker history", "error", err)
		return err
	}

	t.logger.Info("Recorded action", "action", action, "target_type", targetType, "target_id", targetID)
	return nil
}

// GetLastActionTime returns the timestamp of the last recorded action for a target.
func (t *FileTracker) GetLastActionTime(targetID string) (time.Time, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Use the map for efficient lookup
	record, ok := t.historyMap[targetID]
	if !ok {
		return time.Time{}, false
	}
	return record.Timestamp, true
}

// Close is a no-op for the file tracker.
func (t *FileTracker) Close() error {
	return nil
}
