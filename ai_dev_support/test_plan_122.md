# Test plan and results: issue #122 (BSW Gen 7 lab, 2026-10-15/16)

Hub: p4hms. p4d hosts: p4-01..p4-05 (SDP instance 1, P4PORT 1999). Scripts were
copied to `/root/t122` on each p4d host. All results below are against the final
scripts on this branch unless noted.

## Preflight checks (`-d`, `-b`, `-m`)
| ID | Test | Expected | Result |
|----|------|----------|--------|
| T1 | `install_prom_graf.sh -d /nonexistent_vol` (p4hms) | clear error, rc=1, nothing created | PASS |
| T2 | `install_prom_graf.sh -b /nonexistent_bin` | same | PASS |
| T3 | `update_prom_graf.sh -d /nonexistent_vol` | same | PASS |
| T4 | `install_p4prom.sh -nosdp -b /nonexistent_bin ...` | same | PASS |
| N1 | p4-01 `install_p4prom.sh 1 -b /nonexistent_bin` | same | PASS |
| N2 | p4-01 `update_p4prom.sh 1 -b /nonexistent_bin` | same | PASS |
| N3 | `install_p4prom.sh 1 -m /nonexistent_metrics` | same | PASS |
| U1 | `preflight_check_dirs`: defaults skipped, existing dir OK, missing dir fails | as stated | PASS |
| U2 | `preflight_check_dirs` on a read-only mount (tmpfs) | "not writable" error | PASS (a real test write; `[[ -w ]]` is always true for root) |

## Installs and updates
| ID | Host | Test | Result |
|----|------|------|--------|
| H1 | p4hms | `install_prom_graf.sh -d /data/p4mon -b /opt/p4mon-bin -target p4-01:9100 -grafana-setup` (dirs pre-created) | PASS, 83 s. Data under `/data/p4mon/{prometheus,victoria-metrics,alertmanager,grafana}`, binaries in `/opt/p4mon-bin` |
| H2 | p4hms | Services running | node_exporter, prometheus, victoria-metrics, alertmanager, grafana-server |
| H3 | p4hms | Files that stay put with `-d` | `/etc/grafana`, `/etc/prometheus`, `/etc/alertmanager`, `/etc/p4prometheus-monitoring/install.env`, unit files |
| H4 | p4hms | `update_prom_graf.sh` with no flags | PASS: loads `-d`/`-b` from install.env; runs the Grafana step; rc=0 on first and later runs |
| E1 | hub | End to end | PASS: Prometheus scrapes all five hosts (`up`); `p4_*` series: p4-01 258, p4-02 329, p4-03 320, p4-04 347 |
| P1 | p4-01 | `install_p4prom.sh 1 -b /opt/p4prom-bin -osuser perforce` (dir pre-created) | PASS |
| P2 | p4-01 | `update_p4prom.sh 1`, no flags | PASS: keeps `/opt/p4prom-bin` from the state file |
| O1 | p4-02 (forwarding-standby replica) | `install_p4prom.sh 1`, defaults | PASS: 4 units active, binaries in `/usr/local/bin` |
| O2 | p4-03 | `install_p4prom.sh 1 -b /opt/p4prom-bin`, then `update_p4prom.sh 1` | PASS |
| O3 | p4-04 | `update_p4prom.sh 1` before any install | rc=1, "run install_p4prom.sh" |
| O4 | p4-04 | `install_p4prom.sh 1`, defaults | PASS |
| O5 | p4-05 | `install_node.sh`, then `update_node.sh` (node-only role) | PASS: node_exporter only, textfile dir `/var/metrics` |
| O6 | p4-05 | then `install_p4prom.sh 1 -m /p4metrics` (dir pre-created) | PASS: replaces the node-only unit, node_exporter uses `/p4metrics`, 308 `p4_*` series |
| C1 | p4-01 | `install_node.sh` after `install_p4prom.sh` (combined role) | HARMFUL: see Findings |
| S1 | p4-03 | 12 consecutive `update_p4prom.sh 1` | PASS: p4prometheus and p4metrics stay active, all components "up-to-date" |

## Findings and fixes
- **Combined role (C1):** `install_node.sh` replaces
  `/etc/systemd/system/node_exporter.service` (binary `/usr/local/bin/node_exporter`,
  textfile dir `/var/metrics`). After the next restart `p4_*` metrics drop from
  258 to 0, with no error. Documented in the installation doc. Recovery: re-run
  `install_p4prom.sh` with the original options; `update_p4prom.sh` does not
  rewrite an existing unit.
- **Stale process after a unit change:** `systemd_enable_and_restart` used
  `systemctl start`, a no-op on a running service. Fixed: it restarts.
  Verified on p4-01: after C1 (0 series), re-running `install_p4prom.sh` alone
  restored 258 series. `install_node.sh` and `update_node.sh` had the same flaw
  and were fixed.
- **start-limit-hit (S1):** the units set `StartLimitBurst=5` per 300 s. Repeated
  `update_p4prom.sh` runs tripped it and left `p4prometheus` failed while the
  script returned rc=0. Fixed: `reset-failed` before restart, and the script fails
  if the service is not active afterwards.
- **Version checks:** `update_p4prom.sh` ran `<component> --version` from `PATH`, so
  a custom `-b` gave an empty version and forced a reinstall every run. Fixed: it
  uses `${local_bin_dir}/<component>`.
- **`update_prom_graf.sh`:** "Grafana service not found" while Grafana was running
  (`systemctl list-unit-files | grep -q` under `pipefail`), and rc=1 on the first
  update (missing `PERFORCE_RULES_RENDERED_CHECKSUM` under `set -e`). Both fixed.
- **Health checks:** the final check reported Grafana down because it ran before
  Grafana finished starting. Now retries for up to 30 s.
- **`update_p4prom.sh` messages (O3, T5):** on a never-installed host it now says to
  run `install_p4prom.sh`. For `-nosdp` it no longer needs a reachable p4d or
  `P4PORT` before reporting configuration problems.
- **Doc corrections:** Alertmanager is installed by default; the lock timer is
  `p4monitor_locks.timer`; `install.env` stays in `/etc`.
- **Not covered:** `install_node.sh` and `update_node.sh` do not preflight `-m`.
