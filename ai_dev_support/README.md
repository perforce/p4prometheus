# ai_dev_support/

This folder holds working files produced by AI coding assistants (GitHub
Copilot CLI, Claude CLI, etc.) while developing P4Prometheus: session logs,
issue drafts, scratch test plans, and similar artifacts.

## Why this is versioned

Unlike a future `ai/dev/` or `ai/ops/` tree (not yet created), the contents
of this folder are **not** part of the shipped product and are **not**
required to build, install, or operate P4Prometheus. They are versioned
anyway, deliberately, for two reasons:

- **Training**: session logs capture the reasoning, trade-offs, and dead
  ends behind a change, which is often more useful to a future contributor
  (human or AI) than the diff alone.
- **Audit trail**: a durable record of what an AI assistant did, when, and
  why, alongside the code changes it produced.

The size of this folder is expected to stay small (markdown text), so the
extra content in a full clone is negligible.

## Naming convention

Session logs use:

```
session-log-YYYY-MM-DD-<short-tag>.md
```

- `YYYY-MM-DD` is the date the session was conducted (not necessarily the
  date of the commit).
- `<short-tag>` is a brief, descriptive, kebab-case tag for the session's
  main topic — ideally matching the related branch name or issue tag where
  applicable (e.g. `preflight-checks-docs` for issue #122).

Other files (issue drafts, test plans, etc.) should be named descriptively,
e.g. `github-issue-draft-<issue-number>.md`, `test_plan_<issue-number>.md`.

## IMPORTANT: Keep secrets out of session logs

Session logs are committed to the repository and will be visible to anyone
with read access, including in the full commit history forever (removing a
secret in a later commit does **not** remove it from history). Before
committing anything in this folder:

- **Never paste actual token/credential values** into a session log, issue
  draft, or any file here — not even "for reference", not even partially
  redacted in a way that could be reconstructed.
- **Do** document the *mechanism* for using credentials (env var names,
  file paths, command patterns) without including the credential itself.
- If a secret is accidentally committed, treat it as compromised: revoke/
  rotate it immediately (don't rely on rewriting history as the fix).

### Specific guidance for GitHub Personal Access Tokens (PATs)

- Store the token in a local file **outside any git working tree**, or at
  minimum ensure the file/directory is covered by `.gitignore` (e.g.
  `~/g/.pat`, one line, no trailing content).
- Read it into a shell variable at use time rather than hardcoding it
  anywhere:
  ```bash
  PAT=$(tr -d '[:space:]' < ~/g/.pat)
  ```
- When embedding a token in a URL for a single `git push`/`curl` command,
  do so only in that one command, and immediately restore any remote URL
  that had the token embedded in it:
  ```bash
  git -c credential.helper= push "https://<user>:${PAT}@github.com/<org>/<repo>.git" <branch>
  git remote set-url origin https://github.com/<org>/<repo>.git
  ```
- Prefer a **fine-grained PAT** scoped to the specific repo(s) needed, or a
  **classic PAT** only when required (e.g. some org SAML/SSO-enforced repos
  currently only expose "Configure SSO" for classic tokens).
- Never `export` a token in a way that leaks into shell history, CI logs,
  or a subprocess whose output gets captured/logged verbatim.
- When documenting *how* a token was used (as in a session log), describe
  the command pattern and the file path holding the token — never the
  token's value, prefix, or length.
