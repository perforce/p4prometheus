# Test Plan: Advanced Install Options (Issue #118 / #117)

Branch: `118-advanced_install_options`

This document defines acceptance criteria and test procedures for the new enterprise
deployment features added to the p4prometheus install/update scripts.

---

## Table of Contents

1. [Test Environments](#1-test-environments)
2. [Script Under Test Matrix](#2-script-under-test-matrix)
3. [TC-01: Standard Install (monitoring server, no flags)](#tc-01-standard-install-monitoring-server-no-flags)
4. [TC-02: Custom Data Root (monitoring server)](#tc-02-custom-data-root-monitoring-server)
5. [TC-03: Air-Gap Install (monitoring server)](#tc-03-air-gap-install-monitoring-server)
6. [TC-04: Standard Install (p4d server, SDP)](#tc-04-standard-install-p4d-server-sdp)
7. [TC-05: Standard Install (p4d server, non-SDP)](#tc-05-standard-install-p4d-server-non-sdp)
8. [TC-06: SDP Config Migration (upgrade scenario)](#tc-06-sdp-config-migration-upgrade-scenario)
9. [TC-07: HMS Hostname-Based Config Lookup](#tc-07-hms-hostname-based-config-lookup)
10. [TC-08: Update Monitoring Server (update_prom_graf.sh)](#tc-08-update-monitoring-server-update_prom_grafsh)
11. [TC-09: Alert Rules Split-File Management (Issue #117)](#tc-09-alert-rules-split-file-management-issue-117)
12. [TC-10: Data Migration (migrate_p4prom_data.sh)](#tc-10-data-migration-migrate_p4prom_datash)
13. [TC-11: Air-Gap Update (monitoring server)](#tc-11-air-gap-update-monitoring-server)
14. [TC-12: Air-Gap Install/Update (p4d server)](#tc-12-air-gap-installupdate-p4d-server)
15. [TC-13: State File Persistence Across Multiple Upgrades](#tc-13-state-file-persistence-across-multiple-upgrades)
16. [TC-14: Custom Retention and Scrape Targets](#tc-14-custom-retention-and-scrape-targets)
17. [Regression: Existing Tests Pass](#regression-existing-tests-pass)

---

## 1. Test Environments

| ID | OS | Notes |
|----|----|----|
| ENV-A | Ubuntu 22.04 LTS | Monitoring server (Grafana/Prometheus/VM) |
| ENV-B | Rocky Linux 9 / RHEL 9 | Monitoring server (yum/dnf path) |
| ENV-C | Ubuntu 22.04 LTS | p4d server with SDP layout |
| ENV-D | Ubuntu 22.04 LTS | p4d server without SDP |

All test environments should be clean VMs unless otherwise noted.
Upgrade scenarios require a prior install (see individual test cases).

---

## 2. Script Under Test Matrix

| Script | New flags | State file | Air-gap |
|--------|-----------|-----------|---------|
| `install_prom_graf.sh` | `-d`, `-b`, `-r`, `-target`, `--local-tarballs-dir`, `-push` | writes `/etc/p4prometheus-monitoring/install.env` | ✓ |
| `update_prom_graf.sh`  | `-d`, `-b`, `-r`, `--local-tarballs-dir`, `-push` | reads + writes state | ✓ |
| `install_p4prom.sh`    | `-b`, `--local-tarballs-dir` | writes `p4prom_install.env` | ✓ |
| `update_p4prom.sh`     | `-b`, `--local-tarballs-dir` | reads + writes state | ✓ |
| `migrate_p4prom_data.sh` | `-d` (required), `--dry-run`, `--cleanup-old` | reads + writes state | n/a |

---

## TC-01: Standard Install (monitoring server, no flags)

**Purpose:** Verify the default install path still works correctly (no regressions).

**Setup:** Clean ENV-A or ENV-B VM.

**Steps:**
```bash
sudo ./install_prom_graf.sh
```

**Acceptance Criteria:**
- [ ] All services start: `prometheus`, `alertmanager`, `victoria-metrics`, `grafana-server`, `node_exporter`
- [ ] Default data dirs created under `/var/lib/` (`prometheus`, `alertmanager`, `victoria-metrics`, `grafana`)
- [ ] Binaries installed under `/usr/local/bin/`
- [ ] State file written at `/etc/p4prometheus-monitoring/install.env` with `DATA_ROOT=/var/lib`
- [ ] Health check URLs respond (HTTP 200):
  - `http://localhost:9090/-/healthy` (Prometheus)
  - `http://localhost:9093/-/healthy` (Alertmanager)
  - `http://localhost:8428/health` (VictoriaMetrics)
  - `http://localhost:3000/api/health` (Grafana)
  - `http://localhost:9100/metrics` (node_exporter, contains `node_time_seconds`)
- [ ] Prometheus scrapes node_exporter (`localhost:9100/metrics` contains `p4_` metrics after p4prom is installed)
- [ ] `/etc/prometheus/perforce_rules.yml` present
- [ ] `/etc/prometheus/perforce_rules_local.yml` present (created with comment header, never overwritten)
- [ ] Script output shows firewall port summary
- [ ] Re-running the script (idempotent) does not error

---

## TC-02: Custom Data Root (monitoring server)

**Purpose:** Verify `-d` places all data under the specified root.

**Setup:** Clean VM. Optionally mount a separate volume at `/data` (or just `mkdir /data`).

**Steps:**
```bash
sudo ./install_prom_graf.sh -d /data -r 12
```

**Acceptance Criteria:**
- [ ] Data directories created:
  - `/data/prometheus/`
  - `/data/alertmanager/`
  - `/data/victoria-metrics/`
  - `/data/grafana/`
- [ ] Grafana: `/etc/grafana/grafana.ini` has `data = /data/grafana`
- [ ] Prometheus service file ExecStart contains `--storage.tsdb.path /data/prometheus/`
- [ ] Alertmanager service file ExecStart contains `--storage.path=/data/alertmanager`
- [ ] VictoriaMetrics service file ExecStart contains `-storageDataPath /data/victoria-metrics/`
- [ ] Retention set to 12 months: Prometheus ExecStart contains `--storage.tsdb.retention.time=360d`
  - VictoriaMetrics ExecStart contains `-retentionPeriod=360d`
- [ ] State file: `DATA_ROOT=/data`, `RETENTION_MONTHS=12`
- [ ] All health checks pass (same URLs as TC-01)
- [ ] Subsequent `./update_prom_graf.sh` (no flags) reads `/data` from state file and uses it

---

## TC-03: Air-Gap Install (monitoring server)

**Purpose:** Verify install succeeds with no internet access using `--local-tarballs-dir`.

**Setup:** Download all required tarballs on an internet-connected machine. Transfer to ENV-A/ENV-B.
Required files (filenames must match GitHub release asset names exactly):
```
node_exporter-<VER>.linux-amd64.tar.gz
prometheus-<VER>.linux-amd64.tar.gz
alertmanager-<VER>.linux-amd64.tar.gz
victoria-metrics-linux-amd64-<VER>.tar.gz
pushgateway-<VER>.linux-amd64.tar.gz
```
Block internet access: `sudo iptables -P OUTPUT DROP && sudo iptables -A OUTPUT -d 127.0.0.0/8 -j ACCEPT`

**Steps:**
```bash
sudo ./install_prom_graf.sh -d /data --local-tarballs-dir /opt/tarballs
```

**Acceptance Criteria:**
- [ ] No network connection attempts to GitHub (verify with `strace` or by confirming iptables block causes no errors)
- [ ] All tarballs extracted from `/opt/tarballs/`
- [ ] All services start and health checks pass (same as TC-02)
- [ ] Script does not fail with "curl: could not resolve host" or similar network errors

---

## TC-04: Standard Install (p4d server, SDP)

**Purpose:** Verify `install_p4prom.sh` SDP path with new `site/config` default.

**Setup:** Clean ENV-C VM with SDP layout (p4d running on instance 1, `/p4/common/` present).

**Steps:**
```bash
sudo ./install_p4prom.sh -sdp 1
```

**Acceptance Criteria:**
- [ ] Config files written to `/p4/common/site/config/`:
  - `p4prometheus.yml`
  - `p4metrics.yaml`
  - `monitor_metrics.yaml`
- [ ] **Not** written to `/p4/common/config/` (SDP-upgrade-safe)
- [ ] State file written: `/p4/common/site/config/p4prom_install.env`
- [ ] HMS wrapper scripts generated:
  - `/p4/common/site/bin/p4prometheus-start.sh` (executable)
  - `/p4/common/site/bin/p4metrics-start.sh` (executable)
- [ ] Systemd `p4prometheus.service` ExecStart points to wrapper (`/p4/common/site/bin/p4prometheus-start.sh`), not direct binary
- [ ] Systemd `p4metrics.service` ExecStart points to wrapper
- [ ] p4prometheus and p4metrics services start and produce `.prom` files in metrics dir
- [ ] `curl localhost:9100/metrics | grep p4_` returns metrics

---

## TC-05: Standard Install (p4d server, non-SDP)

**Purpose:** Verify `install_p4prom.sh` without SDP uses per-machine paths.

**Setup:** Clean ENV-D VM (no SDP, p4d running directly).

**Steps:**
```bash
sudo ./install_p4prom.sh
```

**Acceptance Criteria:**
- [ ] Config files written to `/etc/p4prometheus/`
- [ ] State file written: `/etc/p4prometheus/p4prom_install.env`
- [ ] **No** HMS wrapper scripts generated
- [ ] Systemd service ExecStart points directly to binary (not a wrapper)
- [ ] p4prometheus and p4metrics services start and produce `.prom` files

---

## TC-06: SDP Config Migration (upgrade scenario)

**Purpose:** Verify `update_p4prom.sh` automatically migrates configs from old `/p4/common/config/` to new `/p4/common/site/config/` with no user action.

**Setup:** ENV-C VM with a prior install that placed configs at `/p4/common/config/` (simulate by copying files there; or use the actual old version of `install_p4prom.sh`).

**Steps:**
```bash
# Precondition: p4prometheus.yml at /p4/common/config/p4prometheus.yml
# (simulating old install)
sudo ./update_p4prom.sh -sdp 1
```

**Acceptance Criteria:**
- [ ] `/p4/common/site/config/p4prometheus.yml` created with same content as old file
- [ ] `/p4/common/site/config/p4metrics.yaml` created (if present at old location)
- [ ] Old file at `/p4/common/config/p4prometheus.yml` has deprecation notice appended (original content preserved above it)
- [ ] Deprecation notice reads something like `# DEPRECATED: This file has been migrated to /p4/common/site/config/`
- [ ] Script logs migration actions to stdout
- [ ] After migration, services restart and continue to work
- [ ] Re-running `update_p4prom.sh` again does **not** re-migrate or error (idempotent)

---

## TC-07: HMS Hostname-Based Config Lookup

**Purpose:** Verify the HMS wrapper selects the host-specific config when present.

**Setup:** ENV-C VM after TC-04 (SDP install with wrapper scripts).

**Steps:**
```bash
HOSTNAME_SHORT=$(hostname -s)
# Copy and modify the shared config with a unique marker:
cp /p4/common/site/config/p4prometheus.yml \
   /p4/common/site/config/p4prometheus.${HOSTNAME_SHORT}.yml
echo "# HMS host-specific config for ${HOSTNAME_SHORT}" >> \
   /p4/common/site/config/p4prometheus.${HOSTNAME_SHORT}.yml

sudo systemctl restart p4prometheus
```

**Acceptance Criteria:**
- [ ] `journalctl -u p4prometheus` shows the binary was started with the host-specific config path
- [ ] Removing the host-specific file and restarting falls back to the shared `p4prometheus.yml`
- [ ] Removing the shared file too falls back to `/p4/common/config/p4prometheus.yml` (legacy path)

---

## TC-08: Update Monitoring Server (update_prom_graf.sh)

**Purpose:** Verify the update script reads the state file and behaves correctly when versions are already current vs. when newer versions are available.

**Setup:** ENV-A with a prior install (TC-01 or TC-02 completed).

**Steps:**
```bash
# Simulate newer version available by lowering the stored version in the state file:
sudo sed -i 's/VER_NODE_EXPORTER=.*/VER_NODE_EXPORTER=1.7.0/' \
    /etc/p4prometheus-monitoring/install.env

sudo ./update_prom_graf.sh
```

**Acceptance Criteria:**
- [ ] Script reads `DATA_ROOT` from state file (no `-d` flag required)
- [ ] node_exporter updated (version in state file was lower than script default)
- [ ] Other components report "already up to date" or are skipped
- [ ] State file updated with new `VER_NODE_EXPORTER`
- [ ] All health checks pass after update
- [ ] Running again immediately is idempotent (no re-download)

**Sub-case: CLI flags override state file**
```bash
sudo ./update_prom_graf.sh -d /data2   # should override DATA_ROOT from state file
```
- [ ] Script uses `/data2` for paths in this run (not whatever is in the state file)
- [ ] State file updated to `DATA_ROOT=/data2` after the run

---

## TC-09: Alert Rules Split-File Management (Issue #117)

**Purpose:** Verify `update_prom_graf.sh` handles the three cases for `perforce_rules.yml`.

**Setup:** ENV-A with a prior monitoring server install.

### Sub-case A: Upstream unchanged, no local edits → silent no-op
```bash
sudo ./update_prom_graf.sh
```
- [ ] `perforce_rules.yml` content unchanged
- [ ] No backup file created
- [ ] No warning in output

### Sub-case B: Upstream changed, no local edits → silent overwrite
```bash
# Simulate upstream change by manually modifying stored checksum:
sudo sed -i 's/PERFORCE_RULES_UPSTREAM_CHECKSUM=.*/PERFORCE_RULES_UPSTREAM_CHECKSUM=aaaaaaa/' \
    /etc/p4prometheus-monitoring/install.env
sudo ./update_prom_graf.sh
```
- [ ] `perforce_rules.yml` overwritten with latest upstream version
- [ ] No backup file created (no local edits present)
- [ ] State file `PERFORCE_RULES_UPSTREAM_CHECKSUM` updated

### Sub-case C: Upstream changed AND local edits present → backup + warn
```bash
# Start from state where upstream checksum is stale:
sudo sed -i 's/PERFORCE_RULES_UPSTREAM_CHECKSUM=.*/PERFORCE_RULES_UPSTREAM_CHECKSUM=aaaaaaa/' \
    /etc/p4prometheus-monitoring/install.env
# Make a local edit to the file:
echo "# local edit" | sudo tee -a /etc/prometheus/perforce_rules.yml
sudo ./update_prom_graf.sh
```
- [ ] Backup file created: `/etc/prometheus/perforce_rules.yml.YYYYMMDD`
- [ ] Backup contains the locally-edited content
- [ ] `/etc/prometheus/perforce_rules.yml` now contains latest upstream
- [ ] Script output warns operator to review and merge into `perforce_rules_local.yml`
- [ ] `perforce_rules_local.yml` is **not** modified

### Sub-case D: perforce_rules_local.yml bootstrapped on first run
```bash
# Fresh install, no perforce_rules_local.yml yet:
sudo rm -f /etc/prometheus/perforce_rules_local.yml
sudo ./update_prom_graf.sh
```
- [ ] `perforce_rules_local.yml` created with comment-only header (no actual rules)
- [ ] Script suggests adding it to `prometheus.yml` rule_files

---

## TC-10: Data Migration (migrate_p4prom_data.sh)

**Purpose:** Verify data can be migrated from default `/var/lib` to `/data` after an existing install.

**Setup:** ENV-A with TC-01 (default install) complete and some Prometheus data accumulated.

### Sub-case A: Dry run
```bash
mkdir -p /data
sudo ./migrate_p4prom_data.sh -d /data --dry-run
```
- [ ] Output describes each planned move
- [ ] No files moved, no services stopped, no state file changed
- [ ] Script exits 0

### Sub-case B: Actual migration
```bash
sudo ./migrate_p4prom_data.sh -d /data
```
- [ ] Prompted for confirmation (answer y)
- [ ] All affected services stopped before any data is moved
- [ ] Data moved:
  - `/var/lib/prometheus/` → `/data/prometheus/`
  - `/var/lib/alertmanager/` → `/data/alertmanager/`
  - `/var/lib/victoria-metrics/` → `/data/victoria-metrics/`
  - `/var/lib/grafana/` → `/data/grafana/` (**Note:** only if `-d` was used at install time; if grafana.ini never had paths.data set, grafana data may be at `/var/lib/grafana` still)
- [ ] Breadcrumb directories left at old paths: `*.migrated-YYYYMMDD/README.txt`
- [ ] Service files updated with new paths
- [ ] `/etc/grafana/grafana.ini` `data =` updated
- [ ] All services restarted
- [ ] Health checks pass 20 seconds after restart
- [ ] State file `DATA_ROOT=/data`
- [ ] Historical Prometheus data visible in Grafana (data not lost)

### Sub-case C: Cleanup old dirs after confirmed migration
```bash
sudo ./migrate_p4prom_data.sh -d /data --cleanup-old
```
- [ ] Breadcrumb `*.migrated-YYYYMMDD` directories removed
- [ ] No data directories present at `/var/lib/prometheus`, etc.

### Sub-case D: Cross-device migration (requires rsync)
```bash
# Mount a different filesystem at /mnt/newdisk, then:
sudo ./migrate_p4prom_data.sh -d /mnt/newdisk
```
- [ ] `rsync` used when `mv` fails across filesystems
- [ ] File count verified after rsync
- [ ] Error if rsync not installed (informative message)

### Sub-case E: Insufficient disk space
```bash
# Fill /data until < required space, then run:
sudo ./migrate_p4prom_data.sh -d /data
```
- [ ] Script aborts before stopping any service
- [ ] Error message shows required vs. available MB

### Sub-case F: Idempotency (destination already exists)
```bash
# Run migration a second time after it succeeded:
sudo ./migrate_p4prom_data.sh -d /data
```
- [ ] "same source and destination" message or "already at destination"
- [ ] No services stopped, no data moved, no errors

---

## TC-11: Air-Gap Update (monitoring server)

**Purpose:** Verify `update_prom_graf.sh --local-tarballs-dir` skips downloads.

**Setup:** ENV-A with prior install. Stage new tarballs in `/opt/tarballs/`. Block internet.

**Steps:**
```bash
sudo ./update_prom_graf.sh --local-tarballs-dir /opt/tarballs
```

**Acceptance Criteria:**
- [ ] No network connection attempts
- [ ] Tarballs extracted from local directory
- [ ] All services running after update
- [ ] Health checks pass

---

## TC-12: Air-Gap Install/Update (p4d server)

**Purpose:** Verify `install_p4prom.sh` and `update_p4prom.sh` work offline.

**Setup:** ENV-D (non-SDP) with no internet. Pre-stage `p4prometheus.linux-amd64.gz` and `p4metrics.linux-amd64.gz` plus `p4prom_common.sh` in `/opt/p4prom-files/`.

**Steps:**
```bash
# Both p4prom_common.sh and install script must be present locally:
sudo ./install_p4prom.sh --local-tarballs-dir /opt/p4prom-files
```

**Acceptance Criteria:**
- [ ] Binaries extracted from local `.gz` files
- [ ] No wget/curl to GitHub
- [ ] Services start and produce metrics

---

## TC-13: State File Persistence Across Multiple Upgrades

**Purpose:** Verify state file survives multiple consecutive update runs and values persist correctly.

**Setup:** ENV-A, install with custom flags:
```bash
sudo ./install_prom_graf.sh -d /data -r 9 -b /opt/bin
```

**Steps:**
```bash
# Run update twice with no flags:
sudo ./update_prom_graf.sh
sudo ./update_prom_graf.sh
```

**Acceptance Criteria:**
- [ ] After each update run, state file still contains `DATA_ROOT=/data`, `RETENTION_MONTHS=9`, `BIN_DIR=/opt/bin`
- [ ] Service files still reference `/data/` paths
- [ ] No flags required to be re-specified

---

## TC-14: Custom Retention and Scrape Targets

**Purpose:** Verify `-r` and `-target` flags are applied correctly.

**Setup:** Clean ENV-A.

**Steps:**
```bash
sudo ./install_prom_graf.sh -r 3 -target myhost1:9100 -target myhost2:9100
```

**Acceptance Criteria:**
- [ ] Prometheus ExecStart contains `--storage.tsdb.retention.time=90d`
- [ ] VictoriaMetrics ExecStart contains `-retentionPeriod=90d`
- [ ] `/etc/prometheus/prometheus.yml` contains scrape configs for `myhost1:9100` and `myhost2:9100`
- [ ] State file: `RETENTION_MONTHS=3`

---

## Regression: Existing Tests Pass

The existing Python testinfra tests in `scripts/tests/` must continue to pass.

```bash
cd scripts/tests
./build_docker.sh
./run_docker_tests.sh
```

**Acceptance Criteria:**
- [ ] `test_sdp.py` all tests pass
- [ ] `test_nosdp.py` all tests pass
- [ ] No regressions in core metrics collection

---

## Notes

- All test cases should be run on at least one apt-based (Ubuntu) and one yum-based (Rocky/RHEL) environment.
- Test cases TC-04 through TC-07 and TC-12 require SDP to be pre-installed. SDP can be obtained from [workshop.perforce.com](https://swarm.workshop.perforce.com/projects/perforce-software-sdp).
- The HMS tests (TC-07) can be approximated on a single machine since the wrapper script logic is host-name-driven.
- Document any environment-specific variations found during testing in a `test/notes.md` file (not committed; use `test/` for test artifacts).
