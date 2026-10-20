# Draft GitHub Issue (tentative #124 — confirm actual number after filing)

**Title:** Add an uninstall/reset script for P4Prometheus components

---

## Background

Follow-up from real-world install testing after #118 (Advanced Install
Options) and #122 (preflight checks / doc gaps), captured during air-gap
trial installs on Cisco infrastructure. Testing repeatedly needed a clean
way to remove a prior P4Prometheus install (binaries, systemd units,
config, data directory) before re-running the installer, and there is
currently no supported tooling for this — it has to be done by hand,
which is error-prone and easy to get wrong (e.g., leaving a stale systemd
unit enabled, or a data directory partially cleaned).

The SDP (Perforce Server Deployment Package) has a precedent for this
kind of tooling: `DANGER_CLEAN.sh` — a deliberately loudly-named,
confirmation-gated script for fully tearing down a server instance. This
issue proposes an analogous script (or scripts) for P4Prometheus.

## Proposed scope

- [ ] Design an uninstall/reset script (or one per role: monitoring server
      vs. Helix Core server) that removes/stops/disables the systemd
      services, binaries, and config installed by the corresponding
      `install_*.sh` script.
- [ ] Decide behavior for the data directory (`-d`) and any collected
      metrics/dashboards data: default to leaving it in place (safer) with
      an explicit opt-in flag to also remove it, versus removing by
      default with an opt-out — needs a decision, default to the safer
      option unless there's a strong reason not to.
- [ ] Follow SDP's `DANGER_CLEAN.sh` precedent for safety UX: require an
      explicit confirmation (e.g., typed confirmation phrase or a
      `--force`/`--yes` flag) before performing destructive actions, and
      support a dry-run mode (consistent with this repo's existing
      `--dry.run` convention used elsewhere).
- [ ] Ensure the script correctly reverses whatever the corresponding
      `install_*.sh`/`update_*.sh` actually did — including any
      state file used by the updater scripts — so a customer can cleanly
      go from "broken/stuck install" back to "nothing installed" and
      re-run the installer from scratch.
- [ ] Document the new script(s) in `doc/P4Prometheus_Installation.adoc`
      (regenerate derived HTML/PDF per standing convention).

## Naming

Working name only — needs a final decision before implementation. Options
to consider: `uninstall_p4prom.sh` / `uninstall_prom_graf.sh` (consistent
with existing `install_*`/`update_*` naming), vs. an SDP-style loud/scary
name like `DANGER_CLEAN.sh` to reinforce that it's destructive. Given this
is a general-purpose monitoring stack (not a P4 server with irreplaceable
depot data), the risk profile is lower than SDP's use case, so a calmer
name may be more appropriate here — but keep the confirmation-gated,
dry-run-capable safety behavior regardless of naming.

## Out of scope for this issue

- The preflight/doc fixes already covered by #122.
- Air-gapped installation improvements (tracked as a separate issue).
