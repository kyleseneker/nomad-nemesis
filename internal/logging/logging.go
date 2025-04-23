package logging

import (
	"os"
	"strings"

	"github.com/hashicorp/go-hclog"

	"github.com/kyleseneker/nomad-nemesis/internal/config"
)

// Logger defines the logging interface used by the application.
// This abstracts the underlying logging library (hclog).
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})

	// Named creates a sublogger with a name component.
	Named(name string) Logger
	// With adds key-value pairs to the logger's context.
	With(args ...interface{}) Logger
}

// Ensure hclogWrapper implements Logger.
var _ Logger = (*hclogWrapper)(nil)

// hclogWrapper adapts hclog.Logger to the Logger interface.
type hclogWrapper struct {
	logger hclog.Logger
}

func (w *hclogWrapper) Debug(msg string, args ...interface{}) {
	w.logger.Debug(msg, args...)
}

func (w *hclogWrapper) Info(msg string, args ...interface{}) {
	w.logger.Info(msg, args...)
}

func (w *hclogWrapper) Warn(msg string, args ...interface{}) {
	w.logger.Warn(msg, args...)
}

func (w *hclogWrapper) Error(msg string, args ...interface{}) {
	w.logger.Error(msg, args...)
}

func (w *hclogWrapper) Named(name string) Logger {
	return &hclogWrapper{logger: w.logger.Named(name)}
}

func (w *hclogWrapper) With(args ...interface{}) Logger {
	return &hclogWrapper{logger: w.logger.With(args...)}
}

// appLogger is the global logger instance for the application.
// It's initialized by InitializeLogger and implements the Logger interface.
// Using a global instance simplifies passing it around, but consider
// dependency injection for larger applications or testing.
var appLogger Logger // Use the interface type

// InitializeLogger creates the application's logger instance based on configuration.
// It should be called early in the application startup.
func InitializeLogger(cfg *config.Config) {
	level := hclog.LevelFromString(cfg.LogLevel)
	if level == hclog.NoLevel {
		// Default to INFO if parsing fails (should be caught by config validation, but be safe)
		level = hclog.Info
	}

	jsonFormat := strings.ToLower(cfg.LogFormat) == "json"

	// Create the underlying hclog logger
	hclogger := hclog.New(&hclog.LoggerOptions{
		Name:       "nomad-nemesis",
		Level:      level,
		Output:     os.Stderr,
		JSONFormat: jsonFormat,
		// IncludeLocation: true, // Might be useful for debugging, but adds overhead
	})

	// Store the wrapper implementation in the global variable
	appLogger = &hclogWrapper{logger: hclogger}

	appLogger.Info("Logger initialized", "level", level.String(), "format", cfg.LogFormat)
}

// Get returns the initialized application logger interface.
// Returns a fallback logger if InitializeLogger has not been called.
func Get() Logger { // Return the interface type
	if appLogger == nil {
		// This should not happen if InitializeLogger is called correctly.
		// Fallback to a default logger just in case, but log a warning.
		fallbackHclogger := hclog.New(&hclog.LoggerOptions{
			Name:  "nomad-nemesis-fallback",
			Level: hclog.Warn,
		})
		// Use the wrapper for the fallback too
		fallbackLogger := &hclogWrapper{logger: fallbackHclogger}
		fallbackLogger.Error("Get() called before InitializeLogger!")
		return fallbackLogger
	}
	return appLogger
}
