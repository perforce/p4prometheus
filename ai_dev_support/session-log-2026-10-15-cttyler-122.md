# Session log: cttyler-122 (2026-10-15)

Tag: `cttyler-122`
Branch: `cttyler-2026-10-issue122` (created from `master` @ b309fea)
Environment: P4 BSW lab. Monitoring hub is p4hms (this machine, root).
Managed p4d hosts: p4c-bos-01 (p4-01), p4c-bos-02 (p4-02), p4c-nyc-03 (p4-03),
p4e-syd-04 (p4-04), p4e-syd-05 (p4-05). Shell aliases such as `s3` reach them.
Lab data is sample training data. Git work is the careful part (PR intended).

## Setup
- Keybindings: Ctrl-S submits, Enter and Shift-Enter insert newlines
  (`~/.claude/keybindings.json`).
- Reviewed `.github/copilot-instructions.md` as CLAUDE.md-equivalent.
- `gh` is not installed; issues #122/#123/#124 were read via web fetch.
- Created branch `cttyler-2026-10-issue122`.

## Issue #122 summary (preflight checks and doc gaps)
1. Document that all four scripts (install/update for P4 server and
   monitoring server) need `p4prom_common.sh`; list it in the air-gap section.
2. Preflight: require `-d` and `-b` paths to exist (no silent `mkdir -p`
   onto the root disk); clear error; document.
3. Document which files stay in `/etc/*` regardless of `-d`.
4. Services-per-role reference table.
5. Investigate and document combined-role hosts (`install_p4prom.sh` plus
   `install_node.sh` on one machine; upgrade behavior).
Out of scope: #123 (air-gap packages, /etc relocation), #124 (uninstall script).

## Findings
- `origin/122-preflight-checks-docs` (5 commits, 2026-09-25) already
  contains #122 work: changes to `install_p4prom.sh`, `install_prom_graf.sh`,
  `update_p4prom.sh`, `update_prom_graf.sh`, `p4prom_common.sh`, and
  `doc/P4Prometheus_Installation.*`, plus an `ai_dev_support/` folder.
- That branch's merge base with `master` is 95abb5c, and `master` has since
  moved on (b309fea), so a plain two-dot diff is very noisy. Use
  `git diff master...origin/122-preflight-checks-docs`.
- Naming differences: that branch uses `ai_dev_support/` and
  `session-log-YYYY-MM-DD-<tag>.md`; this session uses `.ai_dev_support/`
  and `session_log.<tag>.<date>.md` as requested.
- The earlier session's lab test plan was never committed (it lived under a
  gitignored `test/` dir).

## Open questions for user
- Is the plan to build on, rebase, or cherry-pick the existing remote
  branch's #122 commits, or to reimplement from `master`?

## Update: decisions and work so far
- Answers: build on `origin/122-preflight-checks-docs`; write and run a test
  plan on this Gen 7 lab; bring over the old `ai_dev_support/` folder and keep
  its naming convention. This log was moved from `.ai_dev_support/` to
  `ai_dev_support/session-log-2026-10-15-cttyler-122.md` accordingly.
- Confirmed via the GitHub API that no PR exists for the remote branch.
- Cherry-picked the 5 commits onto this branch. The HTML/PDF in
  `doc/P4Prometheus_Installation.*` conflicted (generated files); took the
  commit's version. They still need regenerating from the `.adoc`.
- Ran the lab tests; see `ai_dev_support/test_plan_122.md` for results.
  Headline: preflight works; `install_node.sh` on a p4prom host silently
  breaks P4 metrics (F3), and the doc should say so.

## Update: hub install, doc fixes
- Installed the monitoring stack on p4hms with custom -d/-b; end to end works
  (Prometheus scrapes p4-01, 350 p4_ series). Details in test_plan_122.md.
- Fixed `doc/P4Prometheus_Installation.adoc`: Alertmanager not optional,
  `p4monitor_locks.timer` name, combined-role warning, install.env row in the
  /etc table, intro line. Regenerated HTML and PDF (installed asciidoctor-pdf
  and asciidoctor-diagram gems to do so).
- Out-of-scope bugs noted (F6-F8), not fixed. Candidates for new issues.

## Update: other p4d hosts, bug fixes
- Decision (user): small unrelated bugs found in testing are fixed in this
  branch rather than filed separately; each in its own commit so they can be
  split out of the PR if a reviewer prefers.
- Tested p4-02 (forwarding-standby replica, defaults), p4-03 (custom -b plus
  update), p4-04 (negative then default install), p4-05 (install_node.sh
  only). All pass; hub Prometheus scrapes all five.
- Fixed F4/F6/F7/F8 (see test_plan_122.md). Repair candidates left open:
  F1, F2, F5, F9, install_node.sh start-vs-restart.
- Doc: combined-role recovery now says re-run `install_p4prom.sh`;
  `update_p4prom.sh` does not rewrite an existing unit file.

## Update 2026-10-16: -m preflight and remaining repairs
- Policy from user: bugs found during #122 may be fixed in this branch with
  the commit log as the trail; non-trivial fixes belong in release notes; new
  features need explicit scope discussion. Release notes live in the GitHub
  release body, so a paste-ready draft is in `release_notes_draft_122.md`.
- Added -m preflight; real write test; update_p4prom.sh ordering (F2/F9);
  node scripts restart. F10 intermittent p4prometheus exit is unresolved.
- Open candidates: F10; `install_node.sh`/`update_node.sh` do not preflight
  `-m` (they do not source p4prom_common.sh).
- p4-05 now runs install_p4prom.sh with -m /p4metrics (it previously had
  install_node.sh only), so the lab fleet state changed.

## Update: F10 resolved
- Reproduced (12 back-to-back updates on p4-03) and root-caused: systemd
  start-limit (5 starts/300 s) tripped by per-run restarts, plus a PATH-based
  version check that forced a reinstall every run with a custom -b. Fixed both;
  details in test_plan_122.md. Open candidate list is now just: node scripts do
  not preflight -m.

## Update 2026-10-16: git identity, push, PR prep
- Git identity: no user.name/email was configured, so commits were `root@p4hms`.
  Set global identity to Tom Tyler <ttyler@perforce.com> (chosen over
  cttyler@gmail.com: already verified/linked on the GitHub account, matches the
  current Perforce identity; `cttyler@perforce.com` is an alias and is NOT linked
  on GitHub, as seen from commit 45a5ae6). Rewrote the 16 local commits with
  filter-branch (tree verified identical), deleted the `refs/original` backup,
  expired the reflog and gc'd, and confirmed 0 `root@p4hms` commits remain.
- Pushed `cttyler-2026-10-issue122` using the PAT via a temporary
  GIT_ASKPASS helper (token not printed or stored).
- Decision: keep the folder visible as `ai_dev_support/` (not dot-prefixed) to
  match its purpose and the existing convention; session logs are committed and
  will be mentioned in the PR description.
- PR title/description are drafted in chat only, not committed (they duplicate
  what goes into GitHub). Release notes draft now mentions `ai_dev_support/`.
- Git note: pushing more commits to the branch before or after opening the PR
  is fine and updates the PR; only force-pushes after review starts are a problem.

## HANDOFF / RESUME NOTES (read this first when picking up)

State as of 2026-10-16:
- Repo: /root/g/p4prometheus (GitHub perforce/p4prometheus, origin over HTTPS).
  Working tree should be clean. Git identity is global: Tom Tyler
  <ttyler@perforce.com> (user.name / user.email). Do not use root defaults.
- Branch `cttyler-2026-10-issue122` is pushed (tracks origin). It holds the #122
  work, F-series bug fixes, docs (HTML/PDF regenerated), `ai_dev_support/`.
  The PR for it is NOT yet opened by the user (done in the browser at
  https://github.com/perforce/p4prometheus/pull/new/cttyler-2026-10-issue122).
  Pushing more commits before or after opening the PR is fine; never force-push
  or rebase this branch once the PR is under review or other branches depend on it.
- Planned branches (stacked on the #122 tip, local only until told to push):
  `cttyler-2026-10-issue123` (air-gap packages, /etc relocation) and
  `cttyler-2026-10-issue124` (uninstall/reset script). Siblings, both cut from
  the #122 branch. Open their PRs only after #122 merges, or as drafts against
  the #122 branch. After #122 merges: `git merge master` into each (repo uses
  merge commits). If #122 is squash-merged instead:
  `git rebase --onto master <old-122-tip> <branch>`. Review changes to #122 go
  on the #122 branch and are then merged into the dependents.
- One PR per branch. Do not put #123/#124 work on the #122 branch.
- PR description for #122 was drafted in chat only (not committed). It reports
  the preflight work, the bug fixes (separate commits), the session logs, and the
  lab tests; "Closes #122". The user chose to leave out any "not in scope:
  #123/#124" line.

Reminders / TODO for the user:
- AFTER the #122 PR merges: delete the old remote branch
  `origin/122-preflight-checks-docs` (superseded; its commits live on in our
  branch with new hashes). Needs the user's go-ahead; delete with the PAT.
- Decide whether `ai_dev_support/` stays in version control long-term (currently
  committed, folder kept visible and not dot-prefixed, by decision).
- Release notes live in the GitHub release body, not a file. A paste-ready
  draft for #122 is `ai_dev_support/release_notes_draft_122.md`.

Open candidates (not done): `install_node.sh` / `update_node.sh` do not
preflight `-m` (they do not source p4prom_common.sh).

Lab (BSW Gen 7, no production data; root access is expected):
- p4hms (this host): monitoring hub. Prometheus, VictoriaMetrics, Alertmanager,
  Grafana, node_exporter installed with `-d /data/p4mon -b /opt/p4mon-bin`
  (state in /etc/p4prometheus-monitoring/install.env). Scrapes p4-01..p4-05:9100.
  Docs toolchain gems installed (asciidoctor-pdf, asciidoctor-diagram);
  `make -C doc all` regenerates HTML/PDF.
- p4-01 (p4c-bos-01): p4prom stack under /opt/p4prom-bin. p4-02 (forwarding-
  standby replica): defaults, /usr/local/bin. p4-03 (p4c-nyc-03): /opt/p4prom-bin.
  p4-04: defaults. p4-05: full stack with `-m /p4metrics` (was node-only earlier).
  Test copies of the scripts are in /root/t122 on each host; re-copy changed
  scripts with scp before re-testing. SDP instance 1, P4PORT 1999.
- Shorthands: s1..s5 aliases / p4-01..p4-05 hostnames; ssh as root works.
- Pushing needs the PAT in /root/g/.pat (authorized by the user). Use a
  temporary GIT_ASKPASS helper (username `cttyler`) and never print or store the
  token. `gh` is not installed.
- Permission note: destructive local git cleanup (reflog expire, gc --prune)
  was blocked once by the auto-mode classifier and the user ran it themselves.

Where results live: `ai_dev_support/test_plan_122.md` (tests T/N/P/U/C/H/O,
findings F1-F10, fixes) and this log. Convention: new logs are
`ai_dev_support/session-log-YYYY-MM-DD-<tag>.md`.
