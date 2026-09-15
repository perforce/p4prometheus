# p4prometheus

![Support](https://img.shields.io/badge/Support-Community-yellow.svg)

P4Prometheus integrates Perforce Helix Core Server (`p4d`) with [Prometheus](https://prometheus.io/), Grafana, and optional VictoriaMetrics storage. It collects metrics from p4d logs, `p4 monitor`, and related sources for dashboards and alerting.

The project includes:

- `p4prometheus` for continuous p4d log parsing.
- `p4metrics` for supplementary Helix Core and SDP metrics.
- `monitor_metrics.py` for Linux lock monitoring.
- `p4logtail` and `p4plogtail` for completed-command JSON output.
- Automated installation and upgrade scripts for monitoring servers, Helix Core servers, and node-exporter-only hosts.

## Documentation

- [P4Prometheus Overview](doc/P4Prometheus.adoc): architecture, components, Grafana dashboards, metrics, and related tools.
- [Installation Guide](doc/P4Prometheus_Installation.adoc): recommended automated installation, upgrades, enterprise options, verification, troubleshooting, and manual installation procedures.
- [Runbook Alert Handling](doc/P4_RunbookAlertHandling.adoc): customizable alert response guidance.

This is a Community Supported Perforce tool.
