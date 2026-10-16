# Session log: cttyler-123 (started 2026-10-16)

Issue: #123 - Improve support for air-gapped installation
Branch: `cttyler-2026-10-issue123`, cut from the tip of `cttyler-2026-10-issue122`
(stacked; local only until the user says to push). Not yet started: no code changes.

## Read first
See `ai_dev_support/session-log-2026-10-15-cttyler-122.md`, especially the
"HANDOFF / RESUME NOTES" section: git identity, branch strategy, lab layout, PAT
handling, and reminders.

## Scope (from the GitHub issue)
- Document what P4Prometheus does and what the customer must arrange for air-gapped package installs (Grafana via apt/yum needs network or an internal mirror); investigate an offline path (locally staged .deb/.rpm or an offline mode).
- Investigate relocating /etc config (grafana.ini, prometheus.yml, alertmanager.yml) under -d; the issue author prefers tool-specific config-path options (e.g. Grafana alternate config file, /etc/sysconfig variables) over symlinks. Weigh complexity against demand.
- May be split into two issues once feasibility is clear.

## Notes
- One PR per branch. Open the PR after #122 merges, or as a draft against the
  #122 branch. After #122 merges, `git merge master` into this branch.
- Scope creep: bug fixes found along the way are fine (commit log is the trail;
  non-trivial ones go in the release notes). New features need an explicit
  scope discussion with the user first.
