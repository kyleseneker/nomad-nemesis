# Nomad Nemesis

Nomad Nemesis is a chaos engineering tool inspired by Chaos Monkey, specifically designed for HashiCorp Nomad clusters. It periodically and intentionally introduces failures into your cluster, such as terminating allocations or draining nodes, to help you build more resilient systems.

## Features

*   **Allocation Killing:** Terminates running allocations based on:
    *   Targeting specific jobs or task groups.
    *   Excluding specific jobs or jobs with certain suffixes (e.g., `-dev`, `-canary`).
    *   Configurable probability or mean time between kills (MTBK).
    *   Limits on the maximum number or percentage of allocations killed per cycle.
    *   Cooldown periods (`min_time_between_kills`) to prevent excessive disruption.
    *   Job-specific overrides for fine-grained control.
*   **Node Draining:** Drains client nodes based on:
    *   Targeting nodes with specific attributes.
    *   Excluding nodes by name.
    *   Configurable probability.
    *   Limits on the maximum number or percentage of nodes drained per cycle.
    *   Cooldown periods (`min_time_between_drains`).
*   **Scheduling:** Configure specific days of the week and time windows (including timezone) during which Nemesis is allowed to run.
*   **Safety Features:**
    *   **Dry Run Mode:** See what actions Nomad Nemesis *would* take without actually performing them (`--dry-run` flag or config option).
    *   **Outage Detection:** Automatically pauses attacks if the Nomad cluster itself appears unhealthy (based on server health checks).
    *   **Circuit Breaker:** Automatically pauses attacks if multiple consecutive execution cycles encounter errors, preventing runaway failures.
    *   **Global Enable/Disable:** A top-level switch to quickly enable or disable all attacks.
*   **History Tracking:** Records performed actions to a file (`nomad-nemesis-history.json` by default) or a PostgreSQL database for auditing and cooldown checks.
*   **Configurable Logging:** Set log level (DEBUG, INFO, WARN, ERROR) and format (text, json).

## Installation / Building

You need Go installed (check `go.mod` for the required version).

```bash
# Clone the repository (if you haven't already)
# git clone <repository-url>
# cd nomad-nemesis

# Build the binary
go build .
```

This will create a `nomad-nemesis` executable in the current directory.

## Configuration

Nomad Nemesis is configured primarily via a TOML file.

1.  **Copy the Example:** Start by copying the example configuration:
    ```bash
    cp nomad-nemesis.toml.example nomad-nemesis.toml
    ```
2.  **Edit `nomad-nemesis.toml`:** Modify the settings according to your needs. See the comments within the file for detailed explanations of each option.

**Configuration Loading:**

*   By default, Nomad Nemesis looks for `nomad-nemesis.toml` in `/etc/nomad-nemesis/`, `$HOME/.nomad-nemesis/`, and the current directory (`.`).
*   You can specify a different path using the `--config` command-line flag:
    ```bash
    ./nomad-nemesis --config /path/to/your/config.toml run
    ```
*   Settings can be overridden using environment variables prefixed with `NOMAD_NEMESIS_`. For nested keys, use underscores (e.g., `NOMAD_NEMESIS_ALLOC_KILLER_KILL_PROBABILITY=0.5`).
*   The `--dry-run` flag overrides the `dry_run` setting in the configuration file.

**Key Configuration Sections:**

*   `[general]`: Top-level enable/disable, dry run, run interval, Nomad address, outage check settings.
*   `[alloc_killer]`: Settings for terminating allocations (targets, exclusions, probability/MTBK, limits, cooldown).
*   `[alloc_killer.job_overrides]`: Define specific settings for individual jobs.
*   `[node_drainer]`: Settings for draining nodes (enable, targets, exclusions, probability, limits, cooldown).
*   `[schedule]`: Define the allowed run window (days, start/end time, timezone).
*   `[tracker]`: Configure history tracking (type: "file" or "sql", history directory or DSN).
*   `[circuit_breaker]`: Configure automatic pausing on errors.
*   `[logging]`: Configure log level and format.

**Important:** Nomad Nemesis defaults to `enabled = false` for safety. You **must** explicitly set `enabled = true` in your configuration file or via environment variables to allow attacks.

## Usage

Nomad Nemesis provides two main commands:

1.  **`run`**: Starts the main Nomad Nemesis runner process. It will periodically check the schedule, cluster health, and configuration to execute chaos attacks.
    ```bash
    # Run with configuration in default location
    ./nomad-nemesis run

    # Run with specific config and in dry-run mode
    ./nomad-nemesis --config myconfig.toml --dry-run run
    ```
    The runner will continue until interrupted (e.g., Ctrl+C), performing graceful shutdown.

2.  **`eligible`**: Lists the allocations and nodes that are currently considered eligible targets based *only* on the targeting and exclusion rules in the configuration. It does **not** consider scheduling, probability, limits, or cooldowns. This is useful for debugging your configuration.
    ```bash
    ./nomad-nemesis eligible

    ./nomad-nemesis --config myconfig.toml eligible
    ```

## Development

*   Format code: `go fmt ./...`
*   Check for issues: `go vet ./...` 
