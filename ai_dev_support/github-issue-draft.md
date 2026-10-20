# GitHub Issue Draft: Advanced Install Options

**Proposed Title:**  
Advanced install options: data directory control, upgrade automation, air-gap support, HMS/SDP compatibility

**Labels to apply:** `enhancement`, `install`  
**Branch:** `advanced_install_options`  
**Also closes:** #117

---

## Body

### Summary

This issue tracks a set of improvements to the p4prometheus install scripts
(`install_prom_graf.sh`, `install_p4prom.sh`, `update_p4prom.sh`) aimed at
enterprise deployments, multi-server fleets (HMS), and IaC/air-gapped
environments. A new migration script (`migrate_p4prom_data.sh`) and a new
monitoring-server update script (`update_prom_graf.sh`) are included.

---

### Motivation

Enterprise customers need:
- Control over where data is stored (dedicated volumes, security audits, IaC build-outs)
- Reliable, automatic upgrades that handle config file location changes without
  manual intervention
- Support for air-gapped environments where GitHub is not reachable from
  production servers
- Compatibility with SDP upgrades (`site/` directory) and HMS fleet management
  (per-host config files on a shared `/p4/common/` volume)

---

### Planned Changes

#### 1. Configurable data root (`-d <data_root>`)

Add a `-d <data_root>` flag (default: preserves existing behavior, i.e. `/var/lib`)
to `install_prom_graf.sh` controlling where all runtime data is stored:

| Old path | New path with `-d /data` |
|----------|--------------------------|
| `/var/lib/prometheus/` | `/data/prometheus/` |
| `/var/lib/victoria-metrics/` | `/data/victoria-metrics/` |
| `/var/lib/alertmanager/` | `/data/alertmanager/` |
| `/var/lib/pushgateway/` | `/data/pushgateway/` |
| `/var/lib/grafana/` (via `grafana.ini`) | `/data/grafana/` |

Also expose `$local_bin_dir` (default `/usr/local/bin`) and `$vmagent_config_dir`
(default `/var/vmagent`) as CLI flags on both install scripts.

Example enterprise usage:
```bash
# Create/mount volume first, then:
./install_prom_graf.sh -d /data
```

#### 2. Install state file

At install time, write a state file recording all chosen paths:

- Monitoring server: `/etc/p4prometheus-monitoring/install.env`
- p4d server: `/etc/p4prometheus/install.env` (non-SDP) or
  `/p4/common/site/config/install.env` (SDP)

Example state file:
```bash
DATA_ROOT=/data
BIN_DIR=/usr/local/bin
VMAGENT_CONFIG_DIR=/var/vmagent
METRICS_ROOT=/hxlogs/metrics
CONFIG_DIR=/p4/common/site/config
INSTALL_DATE=2026-07-14
INSTALL_VERSION=0.11.1
```

Update and migration scripts source this file so chosen paths are "sticky"
across upgrades. Explicit CLI flags always override.

#### 3. New `update_prom_graf.sh`

Mirror of `update_p4prom.sh` for the monitoring server side. Updates
node_exporter, prometheus, alertmanager, victoria-metrics, grafana, and
optionally pushgateway/vmagent. Reads state file for data paths. Handles
the perforce_rules.yml split-file migration (see #117 below).

#### 4. New `migrate_p4prom_data.sh` (with `--dry-run`)

Dedicated migration script to move data directories to a new `<data_root>`.

Features:
- `--dry-run` mode: shows exactly what would happen, makes zero changes
- Preflight checks:
  - Sufficient free space at destination
  - Destination is writable
  - All affected services can be stopped
  - Source data directories exist and are readable
- Stops affected services
- Moves data (handles cross-device: uses `rsync` if `mv` fails)
- Updates service ExecStart flags and state file atomically
- Restarts services
- Leaves originals in place with a clear deprecation warning until
  manually cleaned up

Example usage:
```bash
./migrate_p4prom_data.sh --dry-run -d /data   # preview
./migrate_p4prom_data.sh -d /data              # execute
```

#### 5. Upgrade automation for config file location changes

`update_p4prom.sh` automatically handles the config path transition from
`/p4/common/config/` (old default) to `/p4/common/site/config/` (new
SDP-safe default) without requiring user action:

- If state file exists: uses recorded paths
- If no state file (pre-feature upgrade): detects existing config at known
  locations in order, uses what's found, writes state file for future upgrades
- If migrating from old location: copies config to new location (never deletes
  old — adds deprecation comment), logs all actions taken

The goal: customers who haven't read release notes still get a clean upgrade.

#### 6. SDP-upgrade compatibility

Change default `p4prom_config_dir` for SDP installs from `/p4/common/config/`
to `/p4/common/site/config/`.

Files in `site/` are guaranteed not to be modified by future SDP upgrades.
Full backward compatibility maintained via the detection logic in item 5.

#### 7. HMS hostname-based config lookup

For fleet environments where `/p4/common/` is a shared volume (HMS), p4prometheus
and p4metrics resolve their config file at startup via a generated wrapper script:

```
Priority order:
  1. /p4/common/site/config/p4prometheus.<ShortHostname>.yml  ← per-machine (HMS)
  2. /p4/common/site/config/p4prometheus.yml                  ← site-wide default
  3. /p4/common/config/p4prometheus.yml                       ← backward compat
```

First match wins. Same pattern for `p4metrics`. The wrapper is generated by the
install script and referenced in the systemd `ExecStart`.

#### 8. Air-gap / offline install support

Add `--local-tarballs-dir <path>` flag to all install scripts. When set, skips
all `wget`/`curl` downloads and uses pre-staged local files from the specified
directory instead. File naming follows the same convention as the GitHub release
asset names so operators can stage them in advance.

Example:
```bash
# Stage files manually on an internet-connected machine, then copy to target
./install_prom_graf.sh --local-tarballs-dir /mnt/staged-binaries
```

#### 9. Fix #117 — perforce_rules.yml merge workflow (split-file approach)

Resolves the problem of upstream alert rule updates overwriting customer
customizations. Solution:

- `perforce_rules.yml` — owned by p4prometheus, always overwritten on update
  (upstream source of truth, never edit this file)
- `perforce_rules_local.yml` — customer customizations, never touched by updates
  (created as empty/example file on first install)
- `prometheus.yml` `rule_files:` section references both files

`update_prom_graf.sh` handles one-time migration for existing installs:
detects whether the existing `perforce_rules.yml` has local modifications
(via checksum comparison stored in state file), and if so preserves customized
content as `perforce_rules_local.yml` before overwriting with upstream version.

#### 10. Quality-of-life additions

- `-r <months>` retention period flag for VictoriaMetrics (`-retentionPeriod`)
  and Prometheus (`--storage.tsdb.retention.time`). Currently hardcoded to 6.
- `-target <host:port>` (repeatable) sets Prometheus scrape targets at install
  time, eliminating the current TODO placeholder in `prometheus.yml`
- End-of-install health checks: `curl` endpoint verification in addition to
  `systemctl is-active` checks (e.g. `localhost:9090/-/healthy`,
  `localhost:8428/health`, `localhost:3000/api/health`)
- Post-install firewall port summary: prints the ports that need to be open
  (9090 Prometheus, 9093 Alertmanager, 8428 VictoriaMetrics, 9100 node_exporter,
  3000 Grafana) so operators know what to configure in their firewall

#### 11. Documentation updates

- `INSTALL.md`: cover all new flags, state file, air-gap procedure, upgrade
  paths, HMS config file naming convention
- `README.md`: update overview to reflect new enterprise capabilities
- Inline script usage (`-h` output): update to reflect all new flags

---

### Out of Scope (deferred)

- Uninstall scripts — valid use case but lower priority; physical hardware
  deployments can benefit, VM deployments typically just reprovision
- Moving hostname config resolution into the Go binary — current wrapper
  script approach is sufficient; Go change is a clean follow-on

---

### Acceptance Criteria

- [ ] `install_prom_graf.sh -d /data` installs all data under `/data/`
- [ ] Re-running install or update on an existing system with a state file
      uses the previously-chosen paths without re-specifying flags
- [ ] `update_p4prom.sh` on a pre-feature install (no state file, config at
      `/p4/common/config/`) works cleanly without user intervention
- [ ] `migrate_p4prom_data.sh --dry-run` completes without modifying anything
- [ ] `migrate_p4prom_data.sh` with insufficient destination disk space exits
      with a clear error before stopping any services
- [ ] Air-gap install using `--local-tarballs-dir` works with no network access
- [ ] HMS: p4prometheus on machine `myhost` uses `p4prometheus.myhost.yml` if
      present, otherwise falls back correctly
- [ ] `perforce_rules_local.yml` is never overwritten by updates
- [ ] All new flags appear in `-h` usage output
- [ ] INSTALL.md and README.md updated
