# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Test Commands

```bash
# Build p4prometheus binary for current platform
make build

# Build distribution binaries for all platforms (linux/windows/darwin, amd64/arm64)
make dist

# Run all Go tests
go test ./...

# Run tests for a specific package
go test ./config/...
go test ./cmd/p4metrics/...

# Run p4metrics tests (Python integration tests via Docker)
cd cmd/p4metrics && make test

# Build p4metrics binary
cd cmd/p4metrics && make build

# Debug run (dry-run mode, no writes)
./p4metrics --config p4metrics.yaml --debug --dry.run

# Generate a sample config file
./p4prometheus --sample.config > p4prometheus.yaml
```

## Architecture Overview

This project exposes Perforce Helix Core (p4d) server metrics to Prometheus/Grafana. There are three distinct Go tools plus supporting Python/shell scripts:

### Go Components

**`p4prometheus` (root `p4prom.go`)** — The core daemon. Tails the p4d structured log file using `go-libtail`, feeds lines into `go-libp4dlog`'s metrics parser, and writes Prometheus-format `.prom` files for `node_exporter`'s textfile collector. Runs as a long-lived systemd service. Config: `p4prometheus.yaml`.

**`cmd/p4metrics/p4metrics.go`** — Replacement for legacy `monitor_metrics.sh`. Collects supplementary metrics by running `p4` commands (monitor, info, license, pull, servers), tailing `errors.csv`, checking SSL certs, SDP checkpoint logs, Swarm status, etc. Also handles log/journal rotation. Runs as a systemd service. Config: `p4metrics.yaml`.

**`cmd/p4logtail/` and `cmd/p4plogtail/`** — Tails p4d/p4proxy logs and outputs completed commands as line-oriented JSON for ingestion into Elasticsearch or similar tools.

### Lock Monitoring (Python/Shell)

**`scripts/monitor_metrics.py`** — Correlates `lslocks` output with `p4 monitor show` to identify which p4d processes are holding DB locks and which processes they're blocking. Outputs `locks.prom` metrics. Requires `lslocks` (Linux only).

**`scripts/monitor_wrapper.sh`** — Wrapper for `monitor_metrics.py`. Designed to run from cron every minute. Handles SDP environment setup or non-SDP `-nosdp` mode.

### Data Flow

```
p4d log file → p4prometheus (go-libp4dlog) → cmds.prom → node_exporter → Prometheus → Grafana
p4 commands  → p4metrics                   → *.prom    → node_exporter → Prometheus → Grafana
lslocks      → monitor_metrics.py          → locks.prom → node_exporter → Prometheus → Grafana
```

### Configuration

Each tool uses its own YAML config. Key config files:
- `p4prometheus.yaml` / `p4prometheus.yaml.sample` — p4prometheus config (log path, metrics output, SDP instance, server ID)
- `cmd/p4metrics/p4metrics.yaml` — p4metrics config (p4 connection, SDP, metrics paths, rotation settings)
- `scripts/prometheus.yml` — Prometheus scrape config
- `examples/prometheus/perforce_rules.yml` — Prometheus alerting rules (disk, CPU, memory, replication, license, SSL)
- `examples/alertmanager/alertmanager.yml` — Alertmanager config (Slack/PagerDuty integrations)

### Metrics Files

All tools write `.prom` files atomically (write to `.tmp`, then rename) to a directory collected by `node_exporter --collector.textfile.directory`. Metrics files must end in `.prom`.

### Key Dependencies

- `github.com/rcowham/go-libp4dlog` — p4d structured log parsing and metric generation
- `github.com/rcowham/go-libtail` — File tailing with fsnotify/polling support
- `github.com/bitfield/script` — Shell-like scripting in Go (used by p4metrics)
- `github.com/sirupsen/logrus` — Structured logging

## Project Goals (Blizzard-specific)

This fork is being extended to provide **proactive alerting** for Perforce performance issues. Alert severity determines delivery: non-critical → Slack notification, critical → Jira ticket.

### Priority: Database Lock Alerts
- Identify p4 processes blocking other p4 processes (via `monitor_metrics.py` / `p4_locks_cmds_blocked` metric)
- Determine root cause: IO wait (memory/storage/CPU), inefficient command, or other
- Graylog integration: structured output with blocking PID, user, command; blocked PIDs/users/commands; duration; estimated reason (IOWAIT, inefficient command, etc.)

### Additional Alert Types
- **IO Performance**: iowait on perforce processes (memory/storage/CPU)
- **Network Performance**: latency, throttling
- **Perforce Limits**: any server-reported limits from logs

### Key Upstream Files for DB Lock Work
- `scripts/monitor_wrapper.sh` — Compare against Blizzard's version for improvements
- `scripts/monitor_metrics.py` — May have improved lock logging for SDP collection

### Deployment Notes
- Alert/monitoring configs live in the separate `docker-monitoring` repo
- Changes can be added to SDP or deployed via ansible monitoring deployment
- `prometheus_scrapes.md` in this repo compares metrics between upstream (this repo) and the Blizzard-deployed version
