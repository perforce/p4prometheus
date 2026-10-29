# Session log: cttyler-123, Gen6 (Rocky 8) lab testing (2026-10-29)

Continues `session-log-2026-10-16-cttyler-123.md` (read that and the #122 log's
"HANDOFF / RESUME NOTES" first). Branch `cttyler-2026-10-issue123`; no code changes
yet, and nothing here is committed or pushed.

## Gen6 lab layout and access
- AI session runs on p4hms in the Gen7 lab. Gen6 has no p4hms, so the jump box
  `bsw-lab-ui` (Rocky 8.10, hostname bsw-lab-ui.p4demo.com; ask the user for its
  current public IP, it changes between lab runs) stands in as the monitoring host.
- `ssh root@<bsw-lab-ui public IP>` works from here (this host's root ed25519 key was added to
  root's authorized_keys there). The AWS pem the user pointed at has a stray `! `
  before `-----BEGIN`, so ssh rejects it as-is; use a cleaned temp copy, never edit
  the original, delete the copy after.
- From bsw-lab-ui, root ssh reaches the five p4d hosts (the s1..s5 aliases are plain
  `ssh -q -l root <host>`): bos-helix-01, bos-helix-02, nyc-helix-03, syd-helix-04,
  syd-helix-05 (short names helix-01 etc. also resolve). SDP instance `1`, port 1999.
- Repo copy on bsw-lab-ui: `/root/p4prometheus/` (rsync from here; git changes only
  on this host). `/root/g/.pat` already exists there; never copy ours.
- Root volume of bsw-lab-ui grown 10G -> 20G (`growpart /dev/nvme0n1 1` then
  `xfs_growfs /`; growpart takes the DISK, not the partition).
- Grafana admin credentials are never recorded in git. A fresh Grafana install
  starts as admin/admin; if the user changes it, they keep the new password in a
  root-only file on bsw-lab-ui (ask them where). Do not copy it anywhere.
- The Gen6 hosts have SELinux disabled and firewalld inactive, so they will NOT
  expose SELinux/firewall problems. The root volumes of the helix hosts have ~5.5G
  free, and the SDP volumes (/hxdepots, /hxlogs, /hxmetadata) are 88-100% full.

## Starting state (read-only survey)
All five helix hosts had a 2021 p4prometheus v0.7.3 + node_exporter install from the
lab image, with `p4prometheus.service` enabled but failed (SIGKILL when the lab was
stopped). Decision (user): option 1, install over the top on the helix hosts; use the
jump box for clean-install and air-gap tests.

## Step 1: online install on Rocky 8 (done so far)
- bsw-lab-ui: `install_prom_graf.sh -target <each helix>:9100 -grafana-setup` ->
  exit 0, no warnings in the log, all five services active, health checks pass,
  Grafana P4 dashboard loads (user confirmed). Log: `/root/install_prom_graf.online.log`
  on bsw-lab-ui.
- helix hosts: `install_p4prom.sh 1` over the top (scripts rsynced to
  `/root/p4prom_scripts/` on each host). bos-helix-01 and bos-helix-02: exit 0,
  p4prometheus v0.7.3 -> v0.11.5, p4prometheus/node_exporter/p4metrics/
  p4monitor_locks.timer active, node_exporter serves 211-221 `p4_` metrics.
  nyc-helix-03, syd-helix-04, syd-helix-05: same result (exit 0, v0.11.5, four
  services active, 211/242/162 `p4_` metrics). Each shows the same jq error line
  (grep count 3 for error|fail = jq + 2 benign lines).
- Prometheus on bsw-lab-ui: all five node_exporter targets and prometheus itself
  report `up`. **Step 1 complete.**

## Findings (not yet fixed; bug fixes are in scope, features need discussion)
1. `install_p4prom.sh` logs `Error: Failed to find jq in path`, coming from
   `check_for_updates.sh` (needs curl + jq), and exits 0. jq is not installed on
   Rocky 8 by default. `get_volume_info.sh` also uses jq. Decide: install jq in
   install_p4prom.sh, or degrade quietly and say so. jq via dnf also matters for
   air-gap (it comes from the host's repos, or EPEL).
2. Air-gap relevant: the same install run downloads and installs AWS CLI v2 from
   the internet ("AWS CLI v2 required for EBS Throughput data; installing/upgrading
   now"), with no `--local-tarballs-dir` handling and no opt-out. It also ran
   `uv` (with a hardlink warning) to set up Python tooling, which is another
   possible network dependency to check. Add both to the "ignores the flag" list.
3. Confirmed on Rocky 8: Grafana comes from packages.grafana.com via the repo
   the script adds (the online path works; the air-gap path is not yet tested).

## Triage fixes (committed locally, not pushed, 2026-10-29)
Five commits, 1e07bfc..0373435 (one per fix below, in this order: fetch_repo_script,
ensure_jq, check_for_updates skip, AWS CLI, uv). Changes are in `p4prom_common.sh`,
`install_p4prom.sh`, `update_p4prom.sh`:
- `ensure_jq` (new): installs jq from the host's package repos; never fatal.
  Called from install_monitor_locks / update_p4monitor_locks_service.
- `fetch_repo_script` (new): wget normally; with `--local-tarballs-dir` copies
  `monitor_locks.py`/`p4monitor_locks.sh` from the installer's own directory
  (they are repo scripts, not release assets). Replaces the raw.githubusercontent wgets
  in install and update. Side effect: a failed download in install_p4prom.sh is now
  fatal (it was silently ignored before); update already bailed.
- `check_for_updates.sh`: skipped under the flag in install and update.
- `check_aws_cli_version`: skipped under the flag; a failed download is now a warning
  instead of `bail` (on AWS with no internet it used to kill the install).
  Not changed: it runs `yum/dnf remove awscli` BEFORE the download, so an offline
  run can remove a working v1 CLI. Worth reordering if we touch it again.
- `bootstrap_monitor_python_env` (uv): skipped under the flag with a message to use
  system python3 + PyYAML from the mirror. UNTESTED: whether monitor_locks.py runs
  on Rocky 8's python 3.6; check in the air-gap test.
Verified on bos-helix-01 (online re-run of `install_p4prom.sh 1`): jq installed, no
jq error, all four services active. The `--local-tarballs-dir` paths are NOT yet
exercised; that is the air-gap test. `bash -n` clean, shellcheck adds nothing new,
`test_p4prom_common.py` passes (via unittest; pytest not installed here).
Still open: `install_node.sh`/`update_node.sh` have no flag; Alertmanager template and
pint downloads in install_prom_graf.sh; Grafana repo/GPG handling.

## bsw-lab-ui reset to a "fresh" host (2026-10-29)
Removed everything `install_prom_graf.sh` (default paths, `-grafana-setup`) had
created: units for grafana-server/prometheus/alertmanager/node_exporter/
victoria-metrics, the `grafana` rpm and `/etc/yum.repos.d/grafana.repo`, `/etc/{grafana,
prometheus,alertmanager,p4prometheus-monitoring}`, `/var/lib/{grafana,prometheus,
alertmanager,victoria-metrics}`, `/var/log/grafana`, `/usr/share/grafana`, the
binaries in `/usr/local/bin` (prometheus, promtool, alertmanager, amtool, ytt,
node_exporter, victoria-metrics-prod and the vm*-prod tools) and the prometheus/
alertmanager/node_exporter/grafana users. Checked: nothing left running or listening,
no leftover dirs/users, root back to 12G free. The throwaway script is not in the repo
(an uninstall script is a separate issue). Confidence the host matches a fresh one:
the dnf history showed `grafana` as the only package added; remaining differences are
`/root/install_prom_graf.online.log` (kept for comparison), dnf metadata, and the
user's custom Grafana admin password file, which went away with `/etc/grafana` (the
changed password is moot because grafana.db is gone; a new install is admin/admin).
Prometheus on the jump box is gone, so nothing scrapes the helix hosts right now (their
new p4prometheus 0.11.5 / node_exporter stay installed and healthy).

## "What grows" audit: first data point (for the -d / data-volume work)
Right after a fresh Grafana 13.3 install + first start, `/var/lib/grafana` was already
619M (plugins 507M: cloudwatch 92M, prometheus 55M, tempo 46M, loki 42M,
elasticsearch 39M; plugins-bundled 109M; grafana.db 2.3M). `/usr/share/grafana` (the
package) is 1.4G, and the stock install put all of that on the root volume. Check
whether `-d` moves `/var/lib/grafana` (script only creates `grafana_data_dir` when
`-d` is given) and what else of this lands on root.

## Step 2: local mirror built on bsw-lab-ui (2026-10-29)
Everything lives under `/root/airgap/` on bsw-lab-ui (built while the host still had
internet, as a customer would on a connected staging host):
- `tarballs/` (317M), the `--local-tarballs-dir` content, names exactly as the scripts
  expect: `prometheus-2.54.1.linux-amd64.tar.gz`, `alertmanager-0.27.0...`,
  `node_exporter-1.12.1...`, `victoria-metrics-linux-amd64-v1.153.0.tar.gz`,
  `vmutils-linux-amd64-v1.153.0.tar.gz`, `ytt-linux-amd64`,
  `p4prometheus.linux-amd64.gz`, `p4metrics.linux-amd64.gz` (v0.11.5),
  `rules-src/{perforce_rules.yml,default-values.yml}`. Also staged `perforce.tmpl`
  (Alertmanager template) in anticipation of the fix below; it is not read yet.
  Not staged: pushgateway, pint (the test uses neither `-push` nor `-pint`).
- `repo/` (298M): grafana 13.3.0, jq 1.6, oniguruma 6.8.2 + `createrepo_c` metadata.
  Verified with `dnf --disablerepo='*' --repofrompath=airgap,file:///root/airgap/repo
  --setopt=airgap.gpgcheck=0 install --setopt=tsflags=test grafana jq` (check
  succeeded). Used a throwaway `createrepo_c`/`dnf-plugins-core` install, then removed
  with `dnf history undo`; temp `grafana.repo` removed again.
Lessons for the doc:
- The Grafana 13 rpm requires only `/bin/sh` and `systemd`, so the minimal mirror is
  three rpms. `dnf download --resolve --alldeps` pulled 164 packages / 399M (all of
  the base closure) and failed on other architectures until `--arch x86_64 --arch noarch`;
  recommend `--resolve` on a clean host (or just checking `rpm -qpR`), not `--alldeps`.
- The mirror repo file needs `gpgcheck=0` unless the customer stages Grafana's GPG key
  (packages.grafana.com/gpg.key) and points `gpgkey=file://`. `rpm -K` says OK here only
  because the key is already in this host's rpmdb from the earlier online install,
  which is a difference from a truly fresh host. Decide what the doc recommends.
- Still ungated by `--local-tarballs-dir`, will fail in the air-gap run: Alertmanager
  template (`install_prom_graf.sh` ~line 465 bails), Grafana repo + GPG key
  (`install_prom_graf.sh` ~lines 327-349), community dashboards (soft), `pint`.

## Step 3 plan: block outbound on bsw-lab-ui, then run the air-gap install
Decision (user): host firewall with a self-reverting safety, not AWS security groups.
- Use an nftables table (`inet airgap`, output hook) so it can be removed with one
  command. Allow: loopback, established/related (keeps our ssh session alive),
  the lab subnet 10.0.0.0/24, and link-local 169.254.0.0/16 (IMDS and the VPC
  resolver stay reachable in a real air-gapped VPC; `is_running_on_aws` therefore
  still says "on AWS", which is the case the AWS CLI fix targets). Everything else is
  REJECTED, not dropped, so failures are immediate instead of long timeouts.
- Safety: before loading the rules, schedule `systemd-run --on-active=15m` to delete
  the table; confirm ssh still works afterwards; cancel the timer only once the
  rules are proven. Prove the block with `curl` to github.com and
  packages.grafana.com (must fail) before installing anything.
- Run: write a repo file for `/root/airgap/repo` (`gpgcheck=0` for now), disable the
  stock repos for the install, then
  `install_prom_graf.sh --local-tarballs-dir /root/airgap/tarballs -grafana-setup` with
  the same five `-target` values as the online run. Expect the Grafana repo/GPG code,
  the Alertmanager template and the dashboards to fail; fix in separate commits.
- After it passes: compare against `/root/install_prom_graf.online.log` and the set of
  installed files, then repeat the helix-side check with `install_p4prom.sh 1
  --local-tarballs-dir ...` on one host (its scripts need the tarballs there too).

## NEXT STEPS
- Triage findings 1 and 2; then step 2 of the testing plan (minimal local mirror)
  on a clean host: the jump box needs uninstalling or a fresh Gen6 host, so ask.
