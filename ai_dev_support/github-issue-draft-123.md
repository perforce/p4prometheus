# Draft GitHub Issue (tentative #123 — confirm actual number after filing)

**Title:** Improve support for air-gapped installation

---

## Background

Follow-up from real-world install testing after #118 (Advanced Install
Options) and #122 (preflight checks / doc gaps), captured during air-gap
trial installs on Cisco infrastructure. #122 fixed the immediate
"`p4prom_common.sh` isn't documented for all four scripts" gap for
air-gapped use, but two bigger, less-defined air-gap pain points remain
that deserve their own issue rather than being bundled in.

This issue is intentionally scoped loosely at filing time — the exact
extent of what P4Prometheus can/should own here (vs. relying on
customer/OS-level package mirroring) needs more investigation before
committing to specific deliverables.

## Problems to explore

**1. Packaged components (e.g. Grafana) are installed via `apt`/`yum`,
which assumes network access to public package repos**

`install_prom_graf.sh` installs Grafana via the OS package manager
(`apt-get install grafana` / `yum install grafana`, depending on distro),
which requires the customer's package manager to already be pointed at a
repo that has internet access or an internal mirror/proxy configured with
the Grafana package available. For a fully air-gapped host, this either
fails outright or requires the customer to have already solved this
out-of-band (e.g., a pre-configured internal APT/YUM mirror).

- [ ] Document current air-gap expectations/limitations for package-manager-
      installed components explicitly (what P4Prometheus does vs. what the
      customer must arrange themselves)
- [ ] Investigate feasibility of an alternative install path (e.g., a
      pre-downloaded `.deb`/`.rpm` the customer stages locally, or a
      generic "offline package" install mode) — scope/decide after
      investigation, not committed to a specific approach yet

**2. Relocating `/etc/*` configuration files into the data directory**

Some config files (`/etc/grafana/grafana.ini`, `/etc/prometheus/prometheus.yml`,
`/etc/alertmanager/alertmanager.yml`, etc.) always stay under `/etc/*`
regardless of the `-d <data_root>` option (documented as a known limitation
in #122). For some air-gapped/locked-down deployment models, customers may
want *all* P4Prometheus-managed state — config included — under a single
managed volume (e.g., for easier backup, snapshotting, or read-only root
filesystem designs).

- [ ] Investigate whether/how config could optionally be relocated under
      `-d` (e.g., via symlinks from `/etc/*` into the data dir, or native
      config-path flags supported by the underlying tools)
- [ ] Weigh complexity/risk against actual customer demand before
      committing to an implementation approach

## Out of scope for this issue

- The specific preflight/doc fixes already covered by #122.
- The uninstall/reset script (tracked as a separate issue).

## Notes

This issue may be split into two more specific issues once the
investigation above clarifies feasible approaches for each problem.
