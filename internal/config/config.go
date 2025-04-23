package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/spf13/viper"
)

// dayMap maps lowercase day abbreviations to time.Weekday
var dayMap = map[string]time.Weekday{
	"sun": time.Sunday,
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
}

// JobOverrideConfig holds specific settings for a job.
type JobOverrideConfig struct {
	KillProbability      *float64       `mapstructure:"kill_probability"`        // Pointer to distinguish between 0 and unset
	MeanTimeBetweenKills *time.Duration `mapstructure:"mean_time_between_kills"` // Alternative to probability (duration)
	MaxKillCount         *int           `mapstructure:"max_kill_count"`          // Pointer to distinguish between 0 and unset
	MaxKillPercent       *int           `mapstructure:"max_kill_percent"`        // Pointer to distinguish between 0 and unset
	MinTimeBetweenKills  *time.Duration `mapstructure:"min_time_between_kills"`  // Cooldown (pointer to distinguish between 0 and unset)
	// Add other overrideable fields here later if needed (e.g., Enabled bool?)
}

// StressJobLauncherConfig holds settings for the stress job launcher attack.
type StressJobLauncherConfig struct {
	Enabled         bool              `mapstructure:"enabled"`
	TargetJobs      []string          `mapstructure:"target_jobs"`
	TargetNodeAttrs map[string]string `mapstructure:"target_node_attrs"`
	ExcludeNodes    []string          `mapstructure:"exclude_nodes"`
	StressJobSpec   string            `mapstructure:"stress_job_spec"`
	StressDuration  string            `mapstructure:"stress_duration"`
	Probability     float64           `mapstructure:"probability"`
	MaxConcurrent   int               `mapstructure:"max_concurrent"`
	MinTimeBetween  time.Duration     `mapstructure:"min_time_between"`
}

// Config holds the application configuration.
type Config struct {
	NomadAddr string `mapstructure:"nomad_addr"`
	// Alloc Killer
	TargetJobs           []string      `mapstructure:"target_jobs"`
	TargetGroups         []string      `mapstructure:"target_groups"`
	ExcludeJobs          []string      `mapstructure:"exclude_jobs"`
	ExcludeJobSuffixes   []string      `mapstructure:"exclude_job_suffixes"`
	KillProbability      float64       `mapstructure:"kill_probability"`        // Default probability if MTBK not set
	MeanTimeBetweenKills time.Duration `mapstructure:"mean_time_between_kills"` // Alternative probability config (duration)
	MaxKillCount         int           `mapstructure:"max_kill_count"`
	MaxKillPercent       int           `mapstructure:"max_kill_percent"`
	MinTimeBetweenKills  time.Duration `mapstructure:"min_time_between_kills"` // Cooldown
	// Job Specific Overrides
	JobOverrides map[string]JobOverrideConfig `mapstructure:"job_overrides"`
	// Node Drainer
	EnableNodeDrain      bool              `mapstructure:"enable_node_drain"`
	TargetNodeAttrs      map[string]string `mapstructure:"target_node_attrs"`
	ExcludeNodes         []string          `mapstructure:"exclude_nodes"`
	DrainProbability     float64           `mapstructure:"drain_probability"`
	MaxDrainCount        int               `mapstructure:"max_drain_count"`
	MaxDrainPercent      int               `mapstructure:"max_drain_percent"`
	MinTimeBetweenDrains time.Duration     `mapstructure:"min_time_between_drains"`
	// Scheduling
	RunDaysRaw      []string       `mapstructure:"run_days"`
	RunStartTime    string         `mapstructure:"run_start_time"`
	RunEndTime      string         `mapstructure:"run_end_time"`
	RunTimezone     string         `mapstructure:"run_timezone"`
	RunDays         []time.Weekday `mapstructure:"-"`
	RunTimeLocation *time.Location `mapstructure:"-"`
	// General
	Enabled            bool          `mapstructure:"enabled"` // Top-level kill switch for all attacks
	OutageCheckEnabled bool          `mapstructure:"outage_check_enabled"`
	OutageCheckerType  string        `mapstructure:"outage_checker_type"` // "none", "nomad"
	HistoryDir         string        `mapstructure:"history_dir"`
	DryRun             bool          `mapstructure:"dry_run"`
	Interval           time.Duration `mapstructure:"interval"`
	// Tracking Configuration
	TrackerType string `mapstructure:"tracker_type"` // "file" or "sql"
	TrackerDSN  string `mapstructure:"tracker_dsn"`  // Database Source Name (e.g., "postgres://user:pass@host:port/db?sslmode=disable") - Used if tracker_type is 'sql'
	// Circuit Breaker Configuration
	CircuitBreakerEnabled       bool          `mapstructure:"circuit_breaker_enabled"`        // Enable automatic pausing of attacks on repeated errors
	CircuitBreakerThreshold     int           `mapstructure:"circuit_breaker_threshold"`      // Number of consecutive cycle errors to trip the breaker
	CircuitBreakerResetInterval time.Duration `mapstructure:"circuit_breaker_reset_interval"` // How long the breaker stays tripped before attempting to reset
	// Logging Configuration
	LogLevel  string `mapstructure:"log_level"`  // Logging level (e.g., "DEBUG", "INFO", "WARN", "ERROR")
	LogFormat string `mapstructure:"log_format"` // Logging format ("text" or "json")

	// Stress Job Launcher Attack Configuration
	StressJobLauncher StressJobLauncherConfig `mapstructure:"stress_job_launcher"`
}

// timeFormat defines the expected format for start/end times
const timeFormat = "15:04"

// LoadConfig loads configuration from file, environment variables, and defaults using Viper.
// It expects the config file path and dry-run override value to be passed in.
func LoadConfig(configPath string, dryRunFlag bool) (*Config, error) {
	// Remove flag definitions and parsing from here
	// var configPath string // Removed
	// var dryRunOverride bool // Renamed to dryRunFlag (parameter)
	// flag.StringVar(...) // Removed
	// flag.BoolVar(...) // Removed
	// flag.Parse() // Removed

	v := viper.New()

	v.SetDefault("nomad_addr", "")
	v.SetDefault("target_jobs", []string{})
	v.SetDefault("target_groups", []string{})
	v.SetDefault("exclude_jobs", []string{})
	v.SetDefault("exclude_job_suffixes", []string{"-canary", "-stage", "-dev"})
	v.SetDefault("kill_probability", 0.1)
	v.SetDefault("mean_time_between_kills", "0s")
	v.SetDefault("max_kill_count", -1)
	v.SetDefault("max_kill_percent", -1)
	v.SetDefault("min_time_between_kills", "24h")
	v.SetDefault("job_overrides", map[string]JobOverrideConfig{})
	v.SetDefault("enable_node_drain", false)
	v.SetDefault("target_node_attrs", map[string]string{})
	v.SetDefault("exclude_nodes", []string{})
	v.SetDefault("drain_probability", 0.05)
	v.SetDefault("max_drain_count", -1)
	v.SetDefault("max_drain_percent", -1)
	v.SetDefault("min_time_between_drains", "1h")
	v.SetDefault("run_days", []string{"Mon", "Tue", "Wed", "Thu", "Fri"})
	v.SetDefault("run_start_time", "09:00")
	v.SetDefault("run_end_time", "17:00")
	v.SetDefault("run_timezone", "Local")
	v.SetDefault("enabled", false)
	v.SetDefault("outage_check_enabled", true)
	v.SetDefault("outage_checker_type", "nomad")
	v.SetDefault("history_dir", ".")
	v.SetDefault("dry_run", false)
	v.SetDefault("interval", 10*time.Minute)
	v.SetDefault("tracker_type", "file")
	v.SetDefault("tracker_dsn", "")
	v.SetDefault("circuit_breaker_enabled", true)
	v.SetDefault("circuit_breaker_threshold", 3)
	v.SetDefault("circuit_breaker_reset_interval", "5m")
	v.SetDefault("log_level", "INFO")
	v.SetDefault("log_format", "text")

	// Defaults for Stress Job Launcher
	v.SetDefault("stress_job_launcher.enabled", false)
	// Defaults for Resource Pressure
	v.SetDefault("resource_pressure.enabled", false)
	v.SetDefault("resource_pressure.target_jobs", []string{})
	v.SetDefault("resource_pressure.target_node_attrs", map[string]string{})
	v.SetDefault("resource_pressure.exclude_nodes", []string{})
	v.SetDefault("resource_pressure.stress_job_spec", "") // Requires user definition
	v.SetDefault("resource_pressure.stress_duration", "1m")
	v.SetDefault("resource_pressure.probability", 0.05)
	v.SetDefault("resource_pressure.max_concurrent", 2)
	v.SetDefault("resource_pressure.min_time_between", "30m")

	v.SetEnvPrefix("NOMAD_NEMESIS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Use the passed-in configPath
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("nomad-nemesis")
		v.SetConfigType("toml")
		v.AddConfigPath("/etc/nomad-nemesis/")
		v.AddConfigPath("$HOME/.nomad-nemesis")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Printf("Info: No config file found, using defaults and environment variables.")
		} else {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshalling config: %w", err)
	}

	// Manually apply the dry-run flag override AFTER unmarshalling
	// This ensures the flag takes precedence over file/env/defaults
	if dryRunFlag {
		cfg.DryRun = true
	}

	// --- Post-Load Processing & Validation ---
	// Parse Timezone
	loc, err := time.LoadLocation(cfg.RunTimezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", cfg.RunTimezone, err)
	}
	cfg.RunTimeLocation = loc

	// Parse Days
	if len(cfg.RunDaysRaw) == 0 {
		return nil, errors.New("config key 'run_days' cannot be empty")
	}
	parsedDays := []time.Weekday{}
	seenDays := make(map[time.Weekday]bool)
	for _, dayStr := range cfg.RunDaysRaw {
		dayLower := strings.ToLower(strings.TrimSpace(dayStr))
		day, ok := dayMap[dayLower]
		if !ok {
			return nil, fmt.Errorf("invalid day specified in 'run_days': %q", dayStr)
		}
		if !seenDays[day] {
			parsedDays = append(parsedDays, day)
			seenDays[day] = true
		}
	}
	cfg.RunDays = parsedDays

	// Validate Times
	_, err = time.ParseInLocation(timeFormat, cfg.RunStartTime, cfg.RunTimeLocation)
	if err != nil {
		return nil, fmt.Errorf("invalid 'run_start_time' format %q (expect HH:MM): %w", cfg.RunStartTime, err)
	}
	_, err = time.ParseInLocation(timeFormat, cfg.RunEndTime, cfg.RunTimeLocation)
	if err != nil {
		return nil, fmt.Errorf("invalid 'run_end_time' format %q (expect HH:MM): %w", cfg.RunEndTime, err)
	}
	if cfg.RunEndTime <= cfg.RunStartTime {
		return nil, fmt.Errorf("'run_end_time' (%s) must be after 'run_start_time' (%s)", cfg.RunEndTime, cfg.RunStartTime)
	}

	// Other Validation
	if len(cfg.TargetJobs) == 0 && !cfg.EnableNodeDrain {
		return nil, errors.New("at least one attack type must be configured (target_jobs or enable_node_drain)")
	}
	if cfg.KillProbability < 0 || cfg.KillProbability > 1.0 {
		return nil, errors.New("kill_probability must be between 0.0 and 1.0")
	}
	if cfg.MeanTimeBetweenKills < 0 {
		return nil, errors.New("mean_time_between_kills cannot be negative")
	}
	if cfg.MeanTimeBetweenKills > 0 && cfg.MeanTimeBetweenKills < cfg.Interval {
		return nil, fmt.Errorf("mean_time_between_kills (%v) must be greater than or equal to the interval (%v)", cfg.MeanTimeBetweenKills, cfg.Interval)
	}
	if cfg.OutageCheckEnabled && !(cfg.OutageCheckerType == "nomad" || cfg.OutageCheckerType == "none") {
		return nil, fmt.Errorf("invalid outage_checker_type %q: must be 'nomad' or 'none' when outage_check_enabled is true", cfg.OutageCheckerType)
	}
	if cfg.DrainProbability < 0 || cfg.DrainProbability > 1.0 {
		return nil, errors.New("drain_probability must be between 0.0 and 1.0")
	}
	if cfg.Interval <= 0 {
		return nil, errors.New("interval must be a positive duration")
	}
	if cfg.MaxKillCount < -1 {
		return nil, errors.New("max_kill_count cannot be less than -1")
	}
	if cfg.MaxKillPercent < -1 || cfg.MaxKillPercent > 100 {
		return nil, errors.New("max_kill_percent must be between 0 and 100 (or -1)")
	}
	if cfg.MaxDrainCount < -1 {
		return nil, errors.New("max_drain_count cannot be less than -1")
	}
	if cfg.MaxDrainPercent < -1 || cfg.MaxDrainPercent > 100 {
		return nil, errors.New("max_drain_percent must be between 0 and 100 (or -1)")
	}
	if cfg.MinTimeBetweenKills < 0 {
		return nil, errors.New("min_time_between_kills cannot be negative")
	}
	if cfg.MinTimeBetweenDrains < 0 {
		return nil, errors.New("min_time_between_drains cannot be negative")
	}
	if cfg.TrackerType != "file" && cfg.TrackerType != "sql" {
		return nil, fmt.Errorf("invalid tracker_type %q: must be 'file' or 'sql'", cfg.TrackerType)
	}
	if cfg.TrackerType == "sql" && cfg.TrackerDSN == "" {
		return nil, errors.New("tracker_dsn must be set when tracker_type is 'sql'")
	}
	if cfg.CircuitBreakerEnabled {
		if cfg.CircuitBreakerThreshold < 1 {
			return nil, errors.New("circuit_breaker_threshold must be at least 1")
		}
		if cfg.CircuitBreakerResetInterval <= 0 {
			return nil, errors.New("circuit_breaker_reset_interval must be a positive duration")
		}
	}

	// Validate Log Level
	validLevels := map[string]bool{"DEBUG": true, "INFO": true, "WARN": true, "ERROR": true}
	if _, ok := validLevels[strings.ToUpper(cfg.LogLevel)]; !ok {
		return nil, fmt.Errorf("invalid log_level %q: must be one of DEBUG, INFO, WARN, ERROR", cfg.LogLevel)
	}
	// Validate Log Format
	validFormats := map[string]bool{"text": true, "json": true}
	if _, ok := validFormats[strings.ToLower(cfg.LogFormat)]; !ok {
		return nil, fmt.Errorf("invalid log_format %q: must be 'text' or 'json'", cfg.LogFormat)
	}

	// Validate Stress Job Launcher Job Spec if enabled
	if cfg.StressJobLauncher.Enabled {
		if cfg.StressJobLauncher.StressJobSpec == "" {
			return nil, errors.New("stress_job_launcher.stress_job_spec must be defined when stress_job_launcher is enabled")
		}
		// Attempt to minimally validate the job spec syntax
		jobSpecString := strings.TrimSpace(cfg.StressJobLauncher.StressJobSpec)
		var err error
		if len(jobSpecString) > 0 && jobSpecString[0] == '{' {
			var js json.RawMessage
			err = json.Unmarshal([]byte(jobSpecString), &js)
		} else if len(jobSpecString) > 0 {
			_, err = (&api.Client{}).Jobs().ParseHCL(jobSpecString, false)
		} else {
			err = errors.New("stress_job_spec is empty")
		}
		if err != nil {
			return nil, fmt.Errorf("error validating stress_job_launcher.stress_job_spec syntax: %w", err)
		}
	}

	// Validation for Stress Job Launcher fields
	if cfg.StressJobLauncher.Enabled {
		if len(cfg.StressJobLauncher.TargetJobs) == 0 && len(cfg.StressJobLauncher.TargetNodeAttrs) == 0 {
			return nil, errors.New("stress_job_launcher requires either target_jobs or target_node_attrs to be set when enabled")
		}
		if cfg.StressJobLauncher.Probability < 0 || cfg.StressJobLauncher.Probability > 1.0 {
			return nil, errors.New("stress_job_launcher.probability must be between 0.0 and 1.0")
		}
		if cfg.StressJobLauncher.MaxConcurrent < 0 {
			return nil, errors.New("stress_job_launcher.max_concurrent cannot be negative")
		}
		if cfg.StressJobLauncher.MinTimeBetween < 0 {
			return nil, errors.New("stress_job_launcher.min_time_between cannot be negative")
		}
		stressDuration, err := time.ParseDuration(cfg.StressJobLauncher.StressDuration)
		if err != nil || stressDuration <= 0 {
			return nil, fmt.Errorf("invalid stress_job_launcher.stress_duration: %w (must be positive duration string)", err)
		}
	}

	// Check overall config: at least one *type* of attack must be configured
	if len(cfg.TargetJobs) == 0 && !cfg.EnableNodeDrain && !cfg.StressJobLauncher.Enabled {
		return nil, errors.New("at least one attack type must be configured (target_jobs, enable_node_drain, or stress_job_launcher.enabled)")
	}

	return &cfg, nil
}
