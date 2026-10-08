# Scripts Guide

The scripts in this directory install, update, and operate the P4Prometheus
monitoring components. They target Linux hosts using systemd. Run installer and
updater scripts as `root` (normally with `sudo`); run the update checker as the
Perforce service account.

## Quick Start

Use the installer that matches the host role:

| Host role | Installer | Main components |
| --- | --- | --- |
| Monitoring server | `install_prom_graf.sh` | Prometheus, VictoriaMetrics, Grafana, Alertmanager, node_exporter, optional Pushgateway and Pint |
| Helix Core server | `install_p4prom.sh` | p4prometheus, p4metrics, lock monitor, node_exporter |
| Other monitored host | `install_node.sh` | node_exporter |

For an SDP Helix Core server with instance `1`:

```bash
sudo ./install_p4prom.sh 1
```

For a non-SDP Helix Core server, first review the required connection and path
arguments:

```bash
sudo ./install_p4prom.sh -nosdp -h
```

Use `-h` with every script before a first run. Install and update scripts check
that their effective user ID is `root`; a sudo-capable account must still invoke
them through `sudo`.

## Script Inventory

| Script | Use |
| --- | --- |
| `install_prom_graf.sh` | Install the monitoring-server stack. |
| `update_prom_graf.sh` | Update the monitoring-server stack, retaining stored paths and settings. |
| `migrate_prom_graf_data.sh` | Move monitoring-server runtime data to a different base path. Start with `--dry-run`. |
| `install_p4prom.sh` | Install all Helix Core host monitoring components. |
| `update_p4prom.sh` | Update Helix Core host monitoring components and refresh their systemd units. |
| `install_node.sh` / `update_node.sh` | Install or update node_exporter on a non-Helix-Core host. |
| `install_lslocks.sh` | Install only lock monitoring for legacy or limited deployments. |
| `check_for_updates.sh` | Download current scripts into the installed script directory. It updates and re-executes itself first. |
| `p4monitor_locks.sh` | Wrapper that prepares the Perforce environment and starts `monitor_locks.py`. |
| `monitor_locks.py` | Collect lock metrics and optionally send Slack, Teams, SMTP, or script notifications. |
| `create_dashboard.py` / `upload_grafana_dashboard.sh` | Create or upload Grafana dashboards. |
| `github_draft_release.sh` | Build release assets and create or update a GitHub draft release. |

Legacy scripts such as `push_metrics.sh`, `monitor_metrics.sh`, and
`monitor_wrapper.sh` are deprecated. The installers disable legacy lock-monitor
units when installing `p4monitor_locks`.

## Installed Paths and State

For an SDP Helix Core installation, the standard locations are:

| Purpose | Path |
| --- | --- |
| Site configuration | `/p4/common/site/config` |
| Site scripts | `/p4/common/site/bin` |
| Metrics directory | `/p4/metrics` |
| Installer state | `/p4/common/site/config/p4prom_install.env` |

For a non-SDP Helix Core installation, configuration defaults to
`/etc/p4prometheus`, binaries default to `/usr/local/bin`, and metrics default
to `/hxlogs/metrics`. `install_p4prom.sh` records selected paths in
`p4prom_install.env`; `update_p4prom.sh` reads that state automatically. Command
line path options take precedence over stored state.

The monitoring-server installer stores its selected data root, binary directory,
and retention settings in `/etc/p4prometheus-monitoring/install.env`.

## Updating Scripts and Components

On an SDP server, run the checker as the Perforce OS account. It downloads
scripts to the current directory, saving changed files as `.bak`, and records
GitHub file SHAs in `.update_config`.

```bash
sudo -u perforce -H bash -lc '
  cd /p4/common/site/bin
  ./check_for_updates.sh
'
sudo /p4/common/site/bin/update_p4prom.sh 1
```

The checker downloads a changed `check_for_updates.sh`, validates its shell
syntax, preserves the installed file inode, and re-executes the new version
before processing the remaining script list. The updater must then run as root
to install binaries, write units, and restart services.

## Generated Services

The Helix Core installer/updater writes units to `/etc/systemd/system/` and
regenerates them on each update. Do not make durable local changes directly to
these generated unit files; use configuration files or extend the generated unit
with a systemd drop-in when required.

| Unit | Execution account | Purpose |
| --- | --- | --- |
| `p4prometheus.service` | Configured Perforce OS user | Parses the p4d log and writes Prometheus metrics. |
| `p4metrics.service` | Configured Perforce OS user | Collects supplemental p4d metrics. |
| `p4monitor_locks.service` | Configured Perforce OS user | Runs one lock-monitor collection. |
| `p4monitor_locks.timer` | systemd timer | Starts the lock-monitor service every minute. |
| `node_exporter.service` | `node_exporter` | Exposes textfile metrics and system metrics. |

Inspect the generated unit rather than assuming a path or instance value:

```bash
sudo systemctl cat p4prometheus.service
sudo systemctl cat p4metrics.service
sudo systemctl cat p4monitor_locks.service
sudo systemctl status p4monitor_locks.timer --no-pager
```

The updater also starts or restarts the units after writing them. Typical
diagnostics are:

```bash
sudo journalctl -u p4monitor_locks.service --since '15 minutes ago' --no-pager
sudo systemctl list-timers p4monitor_locks.timer
curl -fsS http://localhost:9100/metrics | grep '^p4_'
```

## Configuration and Secrets

`p4metrics.service` and `p4monitor_locks.service` load root-only environment
files through systemd `EnvironmentFile` directives. For SDP installations the
files are normally:

| Component | YAML configuration | Root-only secrets file |
| --- | --- | --- |
| p4metrics | `/p4/common/site/config/p4metrics.yaml` | `/p4/common/site/config/p4metrics.env` |
| Lock monitor | `/p4/common/site/config/p4monitor_locks.yaml` | `/p4/common/site/config/p4monitor_locks.env` |

On a non-SDP installation, substitute the configured directory, normally
`/etc/p4prometheus`.

New secret files are created as `root:root`, mode `0600`, with commented
examples. Add values only as shell-style `NAME=value` lines; do not quote a
value unless the receiving service expects the quote to be part of the secret.

```bash
sudoedit /p4/common/site/config/p4monitor_locks.env
sudo stat -c '%U:%G %a %n' /p4/common/site/config/p4monitor_locks.env
```

The lock monitor supports these environment variable references:

| YAML field | Environment variable |
| --- | --- |
| `notifications.slack.webhook_url_env` | `P4MONITOR_SLACK_WEBHOOK_URL` |
| `notifications.slack.bot_token_env` | `P4MONITOR_SLACK_BOT_TOKEN` |
| `notifications.teams.webhook_url_env` | `P4MONITOR_TEAMS_WEBHOOK_URL` |
| `notifications.email.password_env` | `P4MONITOR_SMTP_PASSWORD` |

`update_p4prom.sh` migrates safe inline legacy lock-monitor credential values
from the YAML file into the environment file and changes the YAML to the
corresponding `*_env` setting. If a secret variable already exists, its existing
value is kept and the YAML is still changed to reference it. Review the YAML and
the `.env` file after an upgrade; do not leave credential literals in YAML.

`p4metrics.env` similarly supports `P4METRICS_SLACK_WEBHOOK_URL` and
`P4METRICS_SLACK_BOT_TOKEN`.

## Testing Lock Notifications

First enable and configure the desired notification channel in
`p4monitor_locks.yaml`, then add its secret to `p4monitor_locks.env`. The
`--notify-test` flag bypasses lock-count and cooldown checks, but it sends a
real notification and may update lock-monitor log, state, and metrics files.

The wrapper used by `p4monitor_locks.service` intentionally accepts only its
normal service arguments. To test `--notify-test` with the same `perforce` user
and root-only `EnvironmentFile` loading as the service, run a transient systemd
unit. Replace `1` if your SDP instance differs:

```bash
sudo systemctl stop p4monitor_locks.timer

sudo systemd-run --wait --collect --pipe \
  --unit=p4monitor-lock-notification-test \
  -p User=perforce \
  -p EnvironmentFile=/p4/common/site/config/p4monitor_locks.env \
  /bin/bash -lc '
    source /p4/common/bin/p4_vars 1
    source /p4/common/site/bin/.venv/bin/activate
    exec /p4/common/site/bin/monitor_locks.py \
      -i 1 \
      -m /p4/metrics \
      -c /p4/common/site/config/p4monitor_locks.yaml \
      --notify-test
  '

sudo systemctl start p4monitor_locks.timer
```

If the test command fails, restart the timer before investigating logs. Review
the transient unit output and the service logs:

```bash
sudo journalctl -u p4monitor-lock-notification-test.service --no-pager
sudo journalctl -u p4monitor_locks.service --since '15 minutes ago' --no-pager
```

For a normal, non-forced collection, run the generated service directly:

```bash
sudo systemctl start p4monitor_locks.service
sudo systemctl status p4monitor_locks.service --no-pager
```

## Development Checks

Run the focused regression test for shell helpers after changing secret
migration behavior:

```bash
python3 scripts/test_p4prom_common.py
bash -n scripts/p4prom_common.sh
```

The wider Go suite can be run from the repository root:

```bash
go test ./...
```
