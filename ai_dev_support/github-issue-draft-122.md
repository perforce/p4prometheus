# Draft GitHub Issue

**Title:** Add preflight checks for -d/-b directories and close install/upgrade documentation gaps (air-gap, deployment roles)

---

## Background

Follow-up findings from real-world install testing after #118 (Advanced Install Options), captured during air-gap trial installs on Cisco infrastructure.

## Problems to address

**1. `p4prom_common.sh` dependency undocumented for monitoring-server scripts**

`install_prom_graf.sh` and `update_prom_graf.sh` also `source p4prom_common.sh` (auto-downloading it from `master` if missing), same as the Helix Core server scripts. `doc/P4Prometheus_Installation.adoc` only tells users to co-download it for `install_p4prom.sh`/`update_p4prom.sh`. This silently breaks air-gapped monitoring-server installs (no network to auto-fetch) and is confusing even for online installs.

- [ ] Document that `p4prom_common.sh` must sit alongside all four scripts (`install_prom_graf.sh`, `update_prom_graf.sh`, `install_p4prom.sh`, `update_p4prom.sh`)
- [ ] Update the air-gap section to list it as a required pre-staged file for monitoring-server air-gap installs too

**2. No preflight validation for `-d <data_root>` / `-b <bin_dir>`**

Scripts currently `mkdir -p` these paths automatically. If a customer's dedicated volume (e.g. `/data`) failed to mount, the script silently creates the directory on the root disk instead of failing loudly — defeating the purpose of a dedicated volume and risking a disk-full root partition.

- [ ] Add a preflight check requiring `-d`/`-b` paths to already exist (fail with a clear error otherwise, don't auto-`mkdir -p` the top-level path)
- [ ] Document this requirement clearly (customer must create/mount the directory before running the installer)

**3. Undocumented list of files that never move with `-d`**

Some config files always stay under `/etc/*` regardless of `-d` (e.g. `/etc/grafana/grafana.ini`, `/etc/prometheus/prometheus.yml`, `/etc/alertmanager/alertmanager.yml`). Not documented anywhere.

- [ ] Add a clear doc list of what `-d` does and does not relocate

**4. Deployment role / systemd service matrix needs to be more explicit**

The existing "Deployment Roles" table names installers and components per role but doesn't explicitly enumerate the systemd services expected to be running on each machine type.

- [ ] Expand documentation with an explicit services-per-role reference table (monitoring server / Helix Core server / other monitored host)

**5. Combined-role machine guidance (e.g., Helix Core replica + Swarm on the same host)**

Unclear whether `install_p4prom.sh` and `install_node.sh` can both be run on the same machine (both configure `node_exporter`), and how upgrades behave in that case.

- [ ] Investigate and document the supported approach for combined-role hosts

## Out of scope (tracked separately)

- Uninstall/reset script (`DANGER_CLEAN.sh`-style) — separate issue
- Air-gapped installation of packaged components (Grafana via apt/yum) and relocating `/etc/*` config into the data directory — separate issue, still being scoped
