# Session log: cttyler-123 (started 2026-10-16)

Issue: #123 - Improve support for air-gapped installation
Branch: `cttyler-2026-10-issue123`, cut from the tip of `cttyler-2026-10-issue122`
(stacked; local only until the user says to push). Scoped 2026-10-16; no code changes yet.

## Read first
See `ai_dev_support/session-log-2026-10-15-cttyler-122.md`, especially the
"HANDOFF / RESUME NOTES" section: git identity, branch strategy, lab layout, PAT
handling, and reminders.

## Scope (from the GitHub issue)
- Document what P4Prometheus does and what the customer must arrange for air-gapped package installs (Grafana via apt/yum needs network or an internal mirror); investigate an offline path (locally staged .deb/.rpm or an offline mode).
- Investigate relocating /etc config (grafana.ini, prometheus.yml, alertmanager.yml) under -d; the issue author prefers tool-specific config-path options (e.g. Grafana alternate config file, /etc/sysconfig variables) over symlinks. Weigh complexity against demand.
- May be split into two issues once feasibility is clear.

## Scope decisions (2026-10-16, with the user)
Everything vaguely related to air-gap support goes in #123 (one release with
#122-#126). Findings from reading the code (not yet reproduced in the lab):
`--local-tarballs-dir` already exists in all four install/update scripts, but is
ignored by: Alertmanager template download (`install_prom_graf.sh`, bails),
`pint` (`-pint`), `monitor_locks.py`/`p4monitor_locks.sh` (`install_p4prom.sh`),
and `install_node.sh`/`update_node.sh` have no such option. Grafana's repo and
GPG key come from packages.grafana.com. Community dashboards come from
grafana.com (soft failure).

Design:
- Air gap means the customer maintains a local package mirror (or hand-managed
  repo) for OS packages; the result should be identical to an online install.
  GitHub release artifacts still come from `--local-tarballs-dir`.
- Proposed rule: with `--local-tarballs-dir`, never add Grafana's repo or key;
  just `apt/dnf install grafana` from the host's configured repos (skip if
  already installed), with a clear error pointing to the doc if unavailable.
  Open for review: downsides of tying this to the one flag (see the PR).
- Fix every place that ignores the flag (separate commits), and add the flag to
  the node scripts.
- Drop "relocate /etc config under -d" and symlinks. The user's reasoning:
  symlinks break silently across package upgrades, and the real need is that
  everything that can GROW lives on the data volume. Replace with: an audit of
  what grows when `-d` is used (suspects: `/var/log/grafana`, Grafana plugins and
  dashboard JSON, journald, p4d-host metrics/log files); fix leaks; document that
  the `/etc` files are small, static, package-managed, and list the ones to back
  up; state that read-only-root layouts are not supported.
- Docs: rewrite the air-gap section as a checklist (what P4Prometheus does vs.
  what the customer arranges).

## Testing plan
- **Target platform is RHEL 8 family**: the first customer is on RHEL 8, so test
  on Rocky 8 (a Gen6 lab) first, then Ubuntu 24 (Gen7, this lab). The scripts have
  only been run on Ubuntu 24 so far; expect RHEL-specific bugs unrelated to air
  gap (yum branch, SELinux, firewalld, systemd 239, python3 availability,
  `/etc/sysconfig/grafana-server`). Those bug fixes are in scope.
- Order: (1) normal online install on Rocky 8; (2) build a minimal local mirror
  (`dnf download --resolve --alldeps` + `createrepo_c`, default repos disabled; on
  Ubuntu `apt-get install --download-only` + `apt-ftparchive`, `[trusted=yes]
  file:` source); (3) block outbound traffic except the lab subnet and run the
  documented air-gap steps on a fresh host; (4) repeat on Ubuntu 24.

## NEXT STEP (state when work paused, 2026-10-16)
The user will set up a **Gen6 (Rocky 8) lab** and make it reachable by ssh from
this host (p4hms in the Gen7 lab), which remains the host this AI session runs
on. Nothing else is started. When it exists:
- Ask the user for hostnames and access; ssh from here.
- Git changes are made ONLY here. To test there, rsync the whole repo (small),
  e.g. `rsync -a --delete /root/g/p4prometheus/ <gen6-host>:/root/p4prometheus/`.
  Do not commit or push from the Gen6 copy, and do not copy `/root/g/.pat`.

## Notes
- One PR per branch, serial: open the PR after #122 (and earlier PRs) merge, then
  `git merge master` into this branch. See the #122 session log for identity,
  PAT handling, and the branch plan.
- Scope creep: bug fixes found along the way are fine (commit log is the trail;
  non-trivial ones go in the release notes). New features need an explicit
  scope discussion with the user first.
