# Release notes draft: issue #122 work (paste into the next GitHub release body)

This project keeps release notes in the GitHub release body (generated with
`--generate-notes` by `scripts/github_draft_release.sh`, then hand-edited), not
in a file in the repo. This is a draft section to merge into that body.

### Installation And Upgrade Improvements

- Installers now fail early, with a clear message, when a custom `-d`, `-b`
  (or `-m` for `install_p4prom.sh`) directory does not exist or is not
  writable, instead of silently creating it on the root filesystem.
- Documented `p4prom_common.sh` as a required file for all four install/update
  scripts (including air-gapped installs), the systemd services expected per
  role, which files stay in `/etc/*` regardless of `-d`, and combined-role hosts.

### Bug Fixes

- `update_prom_graf.sh` no longer reports "Grafana service not found" when
  Grafana is running, and no longer exits 1 on the first update after install.
- `install_prom_graf.sh` / `update_prom_graf.sh` health checks wait for slow
  services (Grafana) instead of reporting a false failure.
- Installers now restart `node_exporter` and other services when their unit
  file changes; previously a changed unit was not picked up until a manual
  restart (running `install_node.sh` on a Helix Core host silently stopped
  `p4_*` metrics).
- `update_p4prom.sh` on a host without an install now says to run
  `install_p4prom.sh`, and with `-nosdp` no longer needs a reachable p4d just
  to report configuration problems.
- Repeated `install_p4prom.sh` / `update_p4prom.sh` runs within five minutes
  could leave `p4prometheus` or `p4metrics` stopped (systemd start-rate limit)
  while the script reported success. The scripts now clear the limit before
  restarting and fail loudly if a service does not come up.
- `update_p4prom.sh` now detects installed component versions in a custom `-b`
  directory instead of re-downloading and reinstalling them on every run.
