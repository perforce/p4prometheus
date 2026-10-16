# Session log: cttyler-126 (started 2026-10-16)

Issue: #126 - Enable multi-agent AI workflow (root AGENTS.md)
Branch: `cttyler-2026-10-issue126`, cut from the tip of `cttyler-2026-10-issue122`
(stacked; local only until the user says to push). Not yet started: no code changes.

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

## Status and dependencies
- Waiting on the user's conversation with Robert Cowham (owner of
  `.github/copilot-instructions.md`). Do not start the move until the user says so.
- Depends on #122 merging first, because `AGENTS.md` links to
  `ai_dev_support/README.md`. After #122 merges, `git merge master` into this branch.
- Tool behavior (which agents read `AGENTS.md`, Claude Code needing `CLAUDE.md` or
  an `@AGENTS.md` import) should be verified before it goes into `AGENTS.md`.
