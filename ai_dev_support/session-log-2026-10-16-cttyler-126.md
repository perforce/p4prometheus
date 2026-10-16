# Session log: cttyler-126 (started 2026-10-16)

Issue: #126 - Enable multi-agent AI workflow (root AGENTS.md)
Branch: `cttyler-2026-10-issue126`, cut from the tip of `cttyler-2026-10-issue122`
(stacked on the #122 tip). Implemented 2026-10-16 after Robert Cowham (owner of
the old file) agreed; see Status.

## Read first
See `ai_dev_support/session-log-2026-10-15-cttyler-122.md` (environment, git
identity, PAT handling, branch plan).

## Scope (from the issue)
- `git mv .github/copilot-instructions.md AGENTS.md` (root), then refresh the
  content in a separate commit. The old content is stale (`p4metrics/`,
  `monitor_locks.py`, `bin/` paths).
- `AGENTS.md` links to `ai_dev_support/README.md` and covers contributor basics:
  set git identity first, one PR per branch, release notes in the GitHub release
  body, build/test/doc-regeneration commands.
- `.gitignore`: allow a tracked root `AGENTS.md`; keep ignoring vendor-specific
  files (`CLAUDE.md`, `GEMINI.md`, `.cursorrules`, ...). Remove `/AGENTS.md` from the
  ignore list and update the comment (lines ~21-38).
- `ai_dev_support/README.md`: link back to `AGENTS.md`; add a short "what belongs
  in a session log" section (decisions and reasons, workarounds for released
  versions, current state and open items; not history that no longer helps).
- Open: whether to leave a one-line stub at `.github/copilot-instructions.md`
  for older Copilot versions.

## Status (2026-10-16)
Done, one commit each: (1) pure `git mv` to root `AGENTS.md`; (2) content refresh
(layout, build/test/doc commands, git identity, one PR per branch, release notes
location, link to `ai_dev_support/README.md`); (3) `.gitignore` no longer ignores
`/AGENTS.md`, other vendor files still ignored; (4) `ai_dev_support/README.md`
links back and gains a "What belongs in a session log" section.

Decisions:
- No stub left at `.github/copilot-instructions.md`: one source of truth. Current
  Copilot reads `AGENTS.md`; I did not test this, so if an older Copilot ignores
  it, add a one-line stub then.
- Claude Code reads `CLAUDE.md`, not `AGENTS.md`. Since `CLAUDE.md` stays ignored,
  `AGENTS.md` suggests a local `CLAUDE.md` containing `@AGENTS.md`. Not verified
  against Claude Code docs in this session.
- The refreshed content was checked against the tree (cmd/ layout, Makefile
  targets, test script names) but the commands were not all executed.

## Dependencies
- The PR is stacked on #122 (it links to `ai_dev_support/README.md`, which #122
  adds). After #122 merges, `git merge master` into this branch, then open the PR.
