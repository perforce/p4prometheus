# Test plan and results: issue #122 (BSW Gen 7 lab, 2026-10-15)

Environment: p4hms (monitoring hub, not yet installed), p4-01 = p4c-bos-01
(SDP instance 1, P4PORT 1999, no prior p4prometheus). Scripts copied to
`/root/t122` on p4-01. Network access to GitHub works from both.

| ID | Test | Expected | Result |
|----|------|----------|--------|
| T1 | p4hms `install_prom_graf.sh -d /nonexistent_vol` | clear error, rc=1, nothing created | PASS |
| T2 | p4hms `install_prom_graf.sh -b /nonexistent_bin` | same | PASS |
| T3 | p4hms `update_prom_graf.sh -d /nonexistent_vol` | same | PASS |
| T4 | p4hms `install_p4prom.sh -nosdp -b /nonexistent_bin ...` | same | PASS |
| T5 | p4hms `update_p4prom.sh -nosdp -b /nonexistent_bin` | preflight error | NOT REACHED: script needs a live P4PORT first (see F2); retested on p4-01 as N2 |
| U1 | `preflight_check_dirs` unit: defaults skipped, existing custom dir OK, missing dir fails | as stated | PASS |
| U2 | `preflight_check_dirs` on a non-writable dir | fail | UNTESTABLE as root (`-w` is true for root); see F1 |
| N1 | p4-01 `install_p4prom.sh 1 -b /nonexistent_bin` | error, rc=1 | PASS |
| N2 | p4-01 `update_p4prom.sh 1 -b /nonexistent_bin` | error, rc=1 | PASS |
| P1 | p4-01 `install_p4prom.sh 1 -b /opt/p4prom-bin -osuser perforce` (dir pre-created) | installs into /opt/p4prom-bin; node_exporter, p4prometheus, p4metrics active | PASS |
| P2 | p4-01 `update_p4prom.sh 1` (no flags) | reads state file, keeps /opt/p4prom-bin | PASS |
| C1 | p4-01 `install_node.sh` after `install_p4prom.sh` (combined role) | doc says: do not do this | CONFIRMED HARMFUL, see F3 |

## Findings
- F1: `-w` check is meaningless for root. Only `-d` existence is really
  verified when run as root, which is the supported way to run these scripts.
- F2: `update_p4prom.sh` needs a reachable p4d (P4PORT) before it gets to the
  preflight check, and without P4PORT set it dies on an unbound variable.
- F3: Running `install_node.sh` on a host that already has `install_p4prom.sh`
  applied overwrote `/etc/systemd/system/node_exporter.service`: binary became
  `/usr/local/bin/node_exporter` and `--collector.textfile.directory`
  changed from `/hxlogs/metrics` to `/var/metrics`. After the next
  restart `p4_*` metrics dropped from 258+ to 0. This is silent. The docs'
  "duplicate/conflicting unit definitions" wording understates it: describe
  it as "replaces the unit and silently stops P4 metrics".
- F4: Recovery: re-running `install_p4prom.sh` (and `update_p4prom.sh`) rewrote
  the unit file correctly but the running process kept stale arguments; a
  manual `systemctl daemon-reload; systemctl restart node_exporter` restored
  258 `p4_*` metrics. Needs a follow-up check of why the script's own
  restart did not take effect.
- F5: `-m`/metrics root (default `/hxlogs/metrics`) is still auto-created by
  `install_p4prom.sh` (`ensure_metrics_root_and_link`). #122 covers only
  `-d`/`-b`; decide whether `-m` should be preflighted too.

## State left on p4-01
p4prometheus, p4metrics, node_exporter, p4monitor_locks timer installed under
/opt/p4prom-bin; `/var/metrics` (empty) and `/usr/local/bin/node_exporter` left
over from C1. Scripts in /root/t122.

## Still to test
- Monitoring server: positive install on p4hms with a pre-created custom `-d`
  and `-b`, then update; confirm which `/etc/*` files stay put (doc item 3)
  and the services-per-role table (doc item 4).
- Other p4d hosts (p4-02..05), including a replica/edge.
- Scraping p4-01:9100 from the hub's Prometheus.
