# Copilot Instructions for p4prometheus

This guide helps AI coding agents work productively in the p4prometheus codebase, which integrates Perforce Helix Core Server (p4d) with Prometheus and Grafana for real-time metrics and monitoring.

## Architecture Overview
- **Main Components:**
  - `p4prometheus`: Parses p4d log files, writes Prometheus-compatible metrics for collection via node_exporter.
  - `p4metrics`: Generates supplementary metrics from p4d, intended to run as a systemd service. Replaces legacy shell scripts.
  - `p4logtail` / `p4plogtail`: Tails p4d/p4p logs, outputs completed commands in JSON for integration with Elastic Search and similar tools.
- **External Integrations:**
  - Prometheus, VictoriaMetrics (optional), Grafana, node_exporter, windows_exporter, alertmanager.
- **Data Flow:**
  - Log files → Custom Go tools → Metrics files (text) → Prometheus node_exporter → Prometheus → Grafana dashboards/alerts.

## Developer Workflows
- **Build:**
  - Use `make` for building Go binaries. See `Makefile` for targets.
  - Binaries are output to `bin/` for multiple platforms.
- **Testing:**
  - Run Go tests with `go test ./...`.
  - Some scripts (e.g., `monitor_metrics.py`) have separate test files (e.g., `test_monitor_metrics.py`).
- **Debugging:**
  - Most Go tools support `--debug` and `--dry.run` flags for verbose output and safe testing.
  - Example: `nohup ./p4metrics --config p4metrics.yaml --debug --dry.run > out.txt &`
- **Configuration:**
  - Each tool uses its own YAML config file (e.g., `p4prometheus.yml`, `p4metrics.yaml`, `p4logtail.yaml`).
  - Config files are typically in the root or `test/` directory.

## Project-Specific Patterns
- **Metrics Output:**
  - Metrics are written in Prometheus textfile format for node_exporter collection.
  - JSON output for logtail tools is line-oriented for easy ingestion.
- **Service Management:**
  - Tools are designed to run as long-lived services (systemd recommended on Linux).
- **Cross-Platform:**
  - Go tools are built for Linux, Windows, and macOS (see `bin/`).
- **SDP Compatibility:**
  - Some tools (e.g., `p4metrics`) are compatible with Perforce SDP conventions.

## Key Files & Directories
- `p4prometheus.go`, `p4metrics/`, `cmd/p4logtail/`, `cmd/p4plogtail/`: Main Go sources.
- `bin/`: Prebuilt binaries for supported platforms.
- `config/`, `test/`: Example configs and test data.
- `scripts/`: Legacy and utility scripts (some deprecated).
- `README.md`, `INSTALL.md`: High-level documentation and setup.

## Example Usage Patterns
- Build: `make`
- Run metrics collector: `./bin/p4prometheus.linux-amd64.gz --config p4prometheus.yml`
- Debug metrics: `./p4metrics --config p4metrics.yaml --debug --dry.run`
- Tail logs: `./p4logtail --config p4logtail.yaml`

## Conventions
- Prefer Go for new tools; legacy scripts may be deprecated.
- Use YAML for configuration.
- Metrics and logs are rotated safely; tools handle log rotation.
- Follow SDP directory conventions for Perforce server setups.

---
For more details, see the main `README.md` and component-specific READMEs in `cmd/`.
