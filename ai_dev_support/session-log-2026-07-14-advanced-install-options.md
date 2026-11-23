# p4prometheus AI Session Log
**Date:** 2026-07-14  
**Repository:** https://github.com/perforce/p4prometheus  
**Session Goal:** Analysis and planning for advanced install options, enterprise features, and upgrade automation  

---

## Initial Prompt (from copilot-instructions.md)

> Please review this P4Prometheus software package in this directory, cloned from https://github.com/perforce/p4prometheus.
>
> **Task 1:** Identify all directory locations where software is installed when the `install_prom_graf.sh`, `install_p4prom.sh`, and `update_p4prom.sh` scripts are run. For each directory location, indicate what caused files to appear.
>
> **Task 2:** Evaluate the feasibility of adding a configuration option to install things in a directory other than default (e.g. `/data`).
>
> No code changes yet — analysis only, then we'll create a git branch.

---

## Task 1: Installation Directory Map

### `install_prom_graf.sh` (Monitoring Server)
Installs: node_exporter, Prometheus, Alertmanager, VictoriaMetrics, Grafana, optionally Pushgateway

#### Binaries → `/usr/local/bin/` *(downloaded from GitHub releases by script)*
| Binary | Source |
|--------|--------|
| `node_exporter` | github.com/prometheus/node_exporter |
| `prometheus`, `promtool` | github.com/prometheus/prometheus |
| `alertmanager`, `amtool` | github.com/prometheus/alertmanager |
| `victoria-metrics-prod`, `vmagent-prod`, `vmalert-prod`, `vmauth-prod`, `vmbackup-prod`, `vmrestore-prod`, `vmctl-prod` | github.com/victoriametrics/victoriametrics |
| `pushgateway` *(optional, `-push` flag)* | github.com/prometheus/pushgateway |

#### Config dirs — created/written by script
| Path | Contents |
|------|----------|
| `/etc/prometheus/` | `prometheus.yml`, `Makefile` |
| `/etc/prometheus/consoles/` | Console HTML templates (copied from tarball) |
| `/etc/prometheus/console_libraries/` | Console JS/template libs (copied from tarball) |
| `/etc/alertmanager/` | `alertmanager.yml`, `Makefile` |

#### Data dirs — created by script
| Path | Purpose |
|------|---------|
| `/var/lib/prometheus/` | Prometheus TSDB data |
| `/var/lib/alertmanager/` | Alertmanager state |
| `/var/lib/victoria-metrics/` | VictoriaMetrics data |
| `/var/lib/pushgateway/` *(optional)* | Pushgateway persistence file |

#### Systemd unit files — `/etc/systemd/system/` — created by script
`node_exporter.service`, `prometheus.service`, `alertmanager.service`, `victoria-metrics.service`, `pushgateway.service` *(optional)*

#### Grafana — **3rd-party (OS package manager, not script-controlled)**
| Path | Origin |
|------|--------|
| `/usr/share/grafana/` | apt/yum package |
| `/etc/grafana/` | apt/yum package |
| `/var/lib/grafana/` | apt/yum package |
| `/var/log/grafana/` | apt/yum package |
| `/usr/share/keyrings/grafana-archive-keyring.gpg` | Script (wget, used by apt) |
| `/etc/apt/sources.list.d/grafana.list` (Ubuntu) | Script |
| `/etc/yum.repos.d/grafana.repo` (CentOS/RHEL) | Script |

#### Temporary (cleaned up on exit)
- `/tmp/` — all downloaded tarballs

---

### `install_p4prom.sh` (p4d Server)
Installs: node_exporter, p4prometheus, p4metrics, monitor_metrics scripts, systemd services/timers

#### Binaries → `$local_bin_dir` (default `/usr/local/bin/`) — script-installed
`node_exporter`, `p4prometheus`, `p4metrics`

#### Config dirs — two modes

**SDP mode** (`./install_p4prom.sh 1`):
| Path | Contents |
|------|----------|
| `/p4/common/config/` | `p4prometheus.yaml`, `p4metrics.yaml`, `monitor_metrics.yaml` |
| `/p4/common/site/bin/` | `monitor_metrics.py`, `monitor_wrapper.sh`, `check_for_updates.sh` |
| `/p4/common/site/bin/.venv/` | Python virtualenv (uv-managed) |

**Non-SDP mode** (`-nosdp`, default or `-c` override):
| Path | Contents |
|------|----------|
| `/etc/p4prometheus/` | `p4prometheus.yaml`, `p4metrics.yaml`, `monitor_metrics.yaml`, scripts, `.venv/` |

#### Metrics directory — `$metrics_root` (`-m` flag, default `/hxlogs/metrics/`)
- Runtime `.prom` files written by p4prometheus/p4metrics, read by node_exporter
- `/p4/metrics` → symlink to `$metrics_root` (SDP only)

#### Systemd files — `/etc/systemd/system/`
`node_exporter.service`, `p4prometheus.service`, `p4metrics.service`, `monitor_locks.service`, `monitor_locks.timer`

#### Optional vmagent (`-push` flag)
| Path | Contents |
|------|----------|
| `/var/vmagent/` | `vmagent.env`, `vmagent.yml`, `relabelConfig.yml`, `.vmpassword` |
| `/etc/systemd/system/vmagent.service` | systemd unit |
| `/usr/local/bin/vmagent-prod` | binary |

#### Temporary
- `/tmp/` — downloads, `/tmp/_install_mon.sh` helper, `/tmp/awscliv2-install/` (AWS only)

---

### `update_p4prom.sh`
Updates existing installation. Same paths as `install_p4prom.sh` — updates binaries, service files, config (appending new params), and optional vmagent.

---

## Task 2: Feasibility of Custom Install Prefix/Data Directory

### Currently parameterized (already have variables or flags)
| Variable | Default | CLI flag? |
|----------|---------|-----------|
| `local_bin_dir` | `/usr/local/bin` | No (hardcoded variable) |
| `metrics_root` | `/hxlogs/metrics` | Yes (`-m`) |
| `p4prom_config_dir` | `/p4/common/config` or `/etc/p4prometheus` | Yes (`-c`) |
| `vmagent_config_dir` | `/var/vmagent` | No (hardcoded variable) |

### Data directory is the cleanest case
All data-holding components already pass their data path as a CLI flag in ExecStart:

| Component | ExecStart flag | Current hardcoded path |
|-----------|---------------|------------------------|
| Prometheus | `--storage.tsdb.path` | `/var/lib/prometheus/` |
| VictoriaMetrics | `-storageDataPath` | `/var/lib/victoria-metrics/` |
| Alertmanager | `--storage.path` | `/var/lib/alertmanager/` |
| Pushgateway | `--persistence.file` | `/var/lib/pushgateway/metric.store` |

Parameterizing them is purely a variable substitution — flags already exist in the service file template.

### Grafana data dir — solvable post-install
Grafana's data dir is set via `paths.data` in `/etc/grafana/grafana.ini`. The install script can patch this after `apt/yum install`. Fully supported by Grafana.

### Cannot be relocated
| Item | Reason |
|------|--------|
| Grafana binary/app files (`/usr/share/grafana`) | Package manager controlled |
| systemd unit files (`/etc/systemd/system/`) | Fixed by systemd ABI |
| SDP paths (`/p4/common/`) if using SDP | SDP convention |

---

## Planning Discussion Summary

### Feature: `-d <data_root>` flag
A single flag on `install_prom_graf.sh` (default preserves current behavior). Enterprise customers doing:
```bash
./install_prom_graf.sh -d /data
```
...get all runtime data under `/data/` instead of `/var/lib/`.

### Feature: Install state file
Written at install time at e.g. `/etc/p4prometheus-monitoring/install.env`. Records chosen paths. Sourced by update/migration scripts so choices are sticky across upgrades. Explicit CLI flags always override.

### Feature: `update_prom_graf.sh` (new script)
Mirror of `update_p4prom.sh` for the monitoring server. Reads state file. Updates all monitoring stack components.

### Feature: `migrate_p4prom_data.sh` (new script)
Dedicated migration script for moving data to a new `<data_root>`. Features:
- `--dry-run` mode
- Preflight checks (disk space, writability, service stop capability)
- Moves data, updates service files and state file
- Leaves originals with deprecation warning until explicitly cleaned

### Feature: SDP-upgrade compatibility
Change default `p4prom_config_dir` for SDP installs from `/p4/common/config/` to `/p4/common/site/config/`. The `site/` directory is guaranteed not to be touched by SDP upgrades. Full backward compat via detection in update script.

### Feature: HMS hostname-based config lookup
For fleet environments where `/p4/common/` is shared (HMS), p4prometheus and p4metrics services use a wrapper script resolving config at startup:
1. `/p4/common/site/config/p4prometheus.<ShortHostname>.yml`
2. `/p4/common/site/config/p4prometheus.yml`
3. `/p4/common/config/p4prometheus.yml` ← backward compat fallback

First match wins. Same pattern for p4metrics.yml.

### Feature: Upgrade automation for config file location moves
`update_p4prom.sh` handles the `/p4/common/config/` → `/p4/common/site/config/` transition automatically:
- Detects old location via state file or filesystem check
- Copies to new location if migrating (never deletes old file — adds deprecation comment)
- Logs all actions taken
- No user intervention required; audit trail preserved

### Feature: Air-gap / offline install support
`--local-tarballs-dir <path>` flag on all install scripts. Skips all `wget`/`curl` downloads and uses pre-staged local files. Enables installs on networks without GitHub access.

### Feature: Issue #117 — perforce_rules.yml merge workflow
Fix via split-file approach:
- `perforce_rules.yml` — owned by p4prometheus, overwritten on every update (upstream source of truth)
- `perforce_rules_local.yml` — customer customizations, never touched by updates
- `prometheus.yml` `rule_files:` section references both
- `update_prom_graf.sh` handles one-time migration: detects customized existing file, preserves as `_local` variant

### Quality-of-life additions (low effort, high value)
- `-r <months>` retention period flag for VictoriaMetrics/Prometheus
- `-target <host:port>` (repeatable) for Prometheus scrape target config at install time
- End-of-install health checks (curl endpoint verification, not just `systemctl status`)
- Post-install firewall port summary printed to stdout

### Deferred to follow-on PR
- Uninstall scripts (`uninstall_prom_graf.sh`, `uninstall_p4prom.sh`)
- Moving hostname config resolution into Go binary (currently: wrapper shell script)

---

## Planned Work Items

| # | Item | Effort |
|---|------|--------|
| 1 | `-d <data_root>` flag + state file in `install_prom_graf.sh` | Low-Med |
| 2 | Expose `local_bin_dir`, `vmagent_config_dir` as CLI flags | Trivial |
| 3 | `update_prom_graf.sh` (new script) | Medium |
| 4 | `migrate_p4prom_data.sh` with `--dry-run` and preflight checks | Medium |
| 5 | Upgrade automation: auto-handle `/p4/common/config/` → `site/config/` | Low |
| 6 | SDP-upgrade compat: change default to `site/config` | Trivial |
| 7 | HMS wrapper script for hostname-based config lookup | Low |
| 8 | Air-gap / `--local-tarballs-dir` flag | Moderate |
| 9 | Fix #117: split-file rule approach + migration in update script | Low-Med |
| 10 | `-r <months>` retention period flag | Trivial |
| 11 | `-target` scrape target flag | Low |
| 12 | End-of-install health checks | Low |
| 13 | Post-install firewall port summary | Trivial |
| 14 | Documentation updates (INSTALL.md, README.md) | Low-Med |

**Branch:** `advanced_install_options`  
**Issues:** New issue (this plan) + closes #117

---

## Implementation Progress

### Branch: `118-advanced_install_options`
GitHub issue #118 filed. Branch created via GitHub UI and checked out locally.
Branch pushed to origin on 2026-07-14.

### All 7 commits (on top of `1ffbdfc`)

| Commit | Files | Summary |
|--------|-------|---------|
| `11e7209` | `install_prom_graf.sh`, `.gitignore` | `-d`, `-b`, `-r`, `-target`, `--local-tarballs-dir` flags; state file; health checks; firewall summary; Grafana data dir; air-gap mode; AI agent instruction filename patterns in .gitignore |
| `621abf2` | `install_p4prom.sh`, `p4prom_common.sh` | `-b`, `--local-tarballs-dir`; SDP site/config default; HMS wrapper scripts; `download_gz`; state file; `write_p4d_state_file` |
| `5a88c3e` | `update_p4prom.sh` | State file load; automatic `/p4/common/config`→`site/config` migration; CLI-vs-state precedence tracking; air-gap; HMS wrapper refresh |
| `a7f4868` | `update_prom_graf.sh` (new) | Full monitoring stack update script; fixes #117 split-file perforce_rules.yml; health checks; state file |
| `e9923a8` | `migrate_p4prom_data.sh` (new) | Live data migration with `--dry-run`, preflight disk space check, cross-device rsync fallback, service stop/start, service file patching, health checks, breadcrumb trail, `--cleanup-old` |
| `4430044` | `INSTALL.md`, `README.md` | Enterprise Deployment Options section; new scripts in inventory; split alerting rules docs; README overview updated |
| `4e06778` | `test/test_plan_118.md`, `.gitignore` | Comprehensive test plan (14 test cases, 4 environments, acceptance criteria); removed stale `test/` from .gitignore |

---

## Links

### Branch on GitHub
https://github.com/perforce/p4prometheus/tree/118-advanced_install_options

### Compare view (all changes vs. main)
https://github.com/perforce/p4prometheus/compare/main...118-advanced_install_options

### GitHub Issue #118
https://github.com/perforce/p4prometheus/issues/118

### GitHub Issue #117 (also addressed by this branch)
https://github.com/perforce/p4prometheus/issues/117

---

## Checking Out the Branch for Testing

```bash
# Clone fresh (if not already cloned):
git clone https://github.com/perforce/p4prometheus.git
cd p4prometheus

# Or, in an existing clone, fetch and check out:
git fetch origin
git checkout 118-advanced_install_options
```

### Key scripts to test

```bash
# Monitoring server (full stack: Prometheus, Grafana, VM, Alertmanager)
scripts/install_prom_graf.sh -h
scripts/update_prom_graf.sh -h

# p4d server
scripts/install_p4prom.sh -h
scripts/update_p4prom.sh -h

# Data migration (after an existing install)
scripts/migrate_p4prom_data.sh -h

# Test plan
cat test/test_plan_118.md
```

### Quick test invocations

```bash
# Standard monitoring install
sudo scripts/install_prom_graf.sh

# Enterprise: custom data volume, 12-month retention, two scrape targets
sudo scripts/install_prom_graf.sh -d /data -r 12 -target p4server1:9100 -target p4server2:9100

# Air-gap install
sudo scripts/install_prom_graf.sh -d /data --local-tarballs-dir /opt/tarballs

# Dry-run data migration
sudo scripts/migrate_p4prom_data.sh -d /data --dry-run

# p4d server install (SDP instance 1)
sudo scripts/install_p4prom.sh -sdp 1

# p4d server install (no SDP)
sudo scripts/install_p4prom.sh
```

---

## GitHub Push — How-To (for future reference)

GitHub no longer accepts password auth for HTTPS Git operations.
A **Classic PAT** (personal access token) with `repo` scope is required,
and must be authorized for the `perforce` org via SSO.

### Setup steps (one-time)

1. Go to https://github.com/settings/tokens (Classic tokens page — not fine-grained)
2. **Generate new token (classic)** → check `repo` scope → set expiry → copy token
3. Save token to `~/g/.pat` (chmod 600)
4. On the tokens list page: **Configure SSO** → **Authorize** next to `perforce`

### Push command

```bash
PAT=$(cat ~/g/.pat | tr -d '[:space:]')
git -c credential.helper= push \
    "https://cttyler:${PAT}@github.com/perforce/p4prometheus.git" \
    118-advanced_install_options
```

Notes:
- `cttyler` is the GitHub username; token goes in the URL (not stored in git config)
- Keep the remote URL itself clean: `git remote set-url origin https://github.com/perforce/p4prometheus.git`
- Fine-grained PATs do **not** support SAML SSO org authorization — use Classic tokens only

---

## Status: COMPLETE for this session

All planned work items implemented and committed. Branch pushed. Ready for peer review before PR.

### Deferred to follow-on PR
- Uninstall scripts (`uninstall_prom_graf.sh`, `uninstall_p4prom.sh`)
- Moving HMS hostname config resolution into the Go binary (currently: shell wrapper)
