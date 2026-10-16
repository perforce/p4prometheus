# AGENTS.md: instructions for AI coding agents

This file is for AI coding agents (and humans) working in the p4prometheus
repo. P4Prometheus integrates Perforce Helix Core Server (p4d) with Prometheus
and Grafana for real-time metrics and monitoring.

Working files from AI-assisted sessions (session logs, issue drafts, test
plans) live in `ai_dev_support/`. **Read `ai_dev_support/README.md` first**, then
the most recent session log for the issue you are working on.

## Before you commit
- **Set the git identity first.** Check `git config --get user.name` and
  `git config --get user.email`; running as root, for example, has none by
  default. Use the human's identity, not the agent's.
- Add a `Co-Authored-By:` trailer for the agent, as your tool's attribution
  guidance directs.
- **One PR per branch.** Branch names look like `<user>-YYYY-MM-issue<N>`.
- Do not force-push or rebase a branch once it is under review or other branches
  depend on it. This repo uses merge commits.
- Never put credentials (PATs, tokens) in any file, commit, or log. See the
  secrets section of `ai_dev_support/README.md`.
- Release notes live in the GitHub release body, not in a file in the repo.
- Bug fixes found along the way may ride on the current branch, each in its own
  commit. New features need an explicit scope discussion first.

## Architecture
- **Go tools** (built from the repo root and `cmd/`):
  - `p4prometheus` (`p4prom.go`): parses p4d log files and writes Prometheus
    textfile metrics for node_exporter to collect.
  - `cmd/p4metrics`: supplementary metrics from p4d, run as a systemd service.
    Replaces the legacy shell scripts.
  - `cmd/monitor_metrics`: Go port of `monitor_metrics.py`; monitors p4 locks and metrics.
  - `cmd/p4logtail`, `cmd/p4plogtail`: tail p4d/p4p logs and emit completed
    commands as JSON (for Elastic Search and similar).
  - `cmd/p4erroranalyzer`: error-code lookup tables (generated Go files).
- **Install/update scripts** (`scripts/`):
  - On a p4d host: `install_p4prom.sh`, `update_p4prom.sh` (p4prometheus,
    p4metrics, node_exporter, lock monitoring). `install_node.sh` /
    `update_node.sh` are for a node_exporter-only host.
  - On the monitoring hub: `install_prom_graf.sh`, `update_prom_graf.sh`
    (Prometheus, VictoriaMetrics, Alertmanager, Grafana).
  - `p4prom_common.sh` is sourced by the install/update scripts (for example
    `preflight_check_dirs`, `systemd_enable_and_restart`) and is required
    alongside them, including on air-gapped hosts.
- **Data flow:** p4d logs -> Go tools -> metrics text files -> node_exporter ->
  Prometheus -> Grafana dashboards and alerts.
- **Docs:** `doc/*.adoc` (AsciiDoc) with generated `.html` and `.pdf` that are
  committed.

## Build and test
- Build: `make` (current platform) or `make dist` (all platforms, writes gzipped
  binaries to `bin/`; `*.gz` is git-ignored).
- Go tests: `go test ./...`
- Script helper tests: `python3 scripts/test_p4prom_common.py` and
  `python3 scripts/test_monitor_locks.py`.
- Container-based install tests (need podman): `scripts/tests/run_docker_tests.sh`,
  `run_docker_prom_graf_tests.sh`, `run_docker_rules_tests.sh`;
  `cmd/p4metrics/tests/run_docker_tests.sh` for p4metrics.
- Docs: after editing a `.adoc`, regenerate with `make -C doc all` (needs the
  `asciidoctor`, `asciidoctor-pdf` and `asciidoctor-diagram` gems) and commit
  the regenerated HTML/PDF with it.
- Most Go tools take `--debug` and `--dry.run` for verbose, safe testing.

## Conventions
- Prefer Go for new tools; legacy scripts may be deprecated.
- YAML for configuration; each tool has its own config file.
- Follow SDP (Perforce Server Deployment Package) directory conventions for p4d
  hosts; the tools also support non-SDP use (`-nosdp`).
- Bash scripts use `set -e`/`pipefail` patterns: beware of `grep -q` in a pipe
  under `pipefail`, and of unset variables under `set -u`.

## Agent-specific files
This `AGENTS.md` is the single shared instruction file. Vendor-specific files
(`CLAUDE.md`, `GEMINI.md`, `.cursorrules`, ...) are git-ignored, so a personal
one is fine locally. For example, a `CLAUDE.md` containing the single line
`@AGENTS.md` makes Claude Code load this file.
