package tracker

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/kyleseneker/nomad-nemesis/internal/logging"
	_ "github.com/lib/pq" // Import the PostgreSQL driver
)

// SqlTracker persists action history to a SQL database.
type SqlTracker struct {
	db     *sql.DB
	logger logging.Logger // Use the interface type
}

// NewSqlTracker creates a tracker using a SQL database connection.
func NewSqlTracker(dsn string) (*SqlTracker, error) {
	logger := logging.Get().Named("sql_tracker") // Get() returns logging.Logger
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQL database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close() // Close connection if ping fails
		return nil, fmt.Errorf("failed to connect to SQL database: %w", err)
	}

	t := &SqlTracker{db: db, logger: logger}

	if err := t.ensureSchema(); err != nil {
		t.db.Close()
		return nil, fmt.Errorf("failed to ensure database schema: %w", err)
	}

	t.logger.Info("SQL Tracker initialized successfully.")
	return t, nil
}

// ensureSchema creates the actions table if it doesn't already exist.
func (t *SqlTracker) ensureSchema() error {
	query := `
	CREATE TABLE IF NOT EXISTS nemesis_actions (
		id SERIAL PRIMARY KEY,
		target_type VARCHAR(50) NOT NULL,
		target_id VARCHAR(255) NOT NULL,
		action VARCHAR(50) NOT NULL,
		timestamp TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_nemesis_actions_target_id_timestamp ON nemesis_actions (target_id, timestamp DESC);
	`
	_, err := t.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to execute schema creation query: %w", err)
	}
	return nil
}

// RecordAction records an action by inserting into the database.
func (t *SqlTracker) RecordAction(targetType string, targetID string, action string) error {
	now := time.Now().UTC()
	query := `INSERT INTO nemesis_actions (target_type, target_id, action, timestamp) VALUES ($1, $2, $3, $4)`
	_, err := t.db.Exec(query, targetType, targetID, action, now)
	if err != nil {
		return fmt.Errorf("failed to insert action record into database: %w", err)
	}
	t.logger.Info("Recorded action", "action", action, "target_type", targetType, "target_id", targetID)
	return nil
}

// GetLastActionTime returns the timestamp of the last recorded action for a target.
func (t *SqlTracker) GetLastActionTime(targetID string) (time.Time, bool) {
	var lastTime time.Time
	query := `SELECT timestamp FROM nemesis_actions WHERE target_id = $1 ORDER BY timestamp DESC LIMIT 1`
	err := t.db.QueryRow(query, targetID).Scan(&lastTime)

	if err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, false // No record found
		} else {
			// Log unexpected error, but treat as no record found for safety
			t.logger.Warn("Error querying last action time", "target_id", targetID, "error", err)
			return time.Time{}, false
		}
	}

	return lastTime, true
}

// Close closes the database connection.
func (t *SqlTracker) Close() error {
	if t.db != nil {
		t.logger.Info("Closing SQL Tracker database connection...")
		return t.db.Close()
	}
	return nil
}
