# p4metrics

This is a version of monitor_metrics.sh in Go as part of p4prometheus
It is intended to be more reliable and cross platform than the original.
It should be run permanently as a systemd service on Linux, as it tails
the errors.csv file

Works fine on Windows!

This replaces the (now deprecated) [monitor_metrics.sh](../../scripts/monitor_metrics.sh) script - which was hard to use on Windows, and lacked some functionality.

## Release Notes

### 2026-06-03

- Added `p4_active_memory_by_cmd{cmd}` (gauge): active memory in bytes by command.
- Added `p4_active_memory_by_user{user}` (gauge): active memory in bytes by user.
- These active-memory metrics include all monitor states, including blocked commands (`B`).
- Memory limit termination behavior is unchanged.

## Usage

```[bash]
./p4metrics -h
usage: p4metrics [<flags>]

Flags:
  -h, --help                     Show context-sensitive help (also try --help-long and --help-man).
  -c, --config="p4metrics.yaml"  Config file for p4prometheus.
      --sdp.instance=""          SDP Instance, typically 1 or alphanumeric.
      --p4port=""                P4PORT to use (if sdp.instance is not set).
      --p4user=""                P4USER to use (if sdp.instance is not set).
      --p4config=""              P4CONFIG file to use (if sdp.instance is not set and no value in config file).
      --debug                    Enable debugging.
  -n, --dry.run                  Don't write metrics - but show the results - useful for debugging with --debug.
  -C, --sample.config            Output a sample config file and exit. Useful for getting started to create p4metrics.yaml. E.g. p4metrics --sample.config > p4metrics.yaml
  -V, --version                  Show application version.
```

Debugging (in bash):

```bash
nohup ./p4metrics --config p4metrics.yaml --debug --dry.run > out.txt &
# Examine the context - shows metrics being written and commands being run without actually updating any *.prom files
less out.txt
kill %1     # Kill the running task when happy
```

## Features

## Memory Limits Enforcement

p4metrics includes automatic memory limit enforcement for Perforce processes. This feature monitors running processes for memory usage violations and can automatically terminate them based on configurable thresholds.

**Key capabilities**:

- Track memory usage per process, command, and user
- Define threshold limits by command, user groups, or cumulative usage
- Optional automatic termination of violating processes
- Safe by default (enforcement disabled, dry-run mode available)
- Comprehensive logging of all enforcement actions
- Cross-platform support (Linux memory tracking via /proc filesystem)

## Config file p4metrics.yaml

Run `p4metrics --sample.config > p4metrics.yaml` to create an example.

Note that [the installer](../../scripts/install_p4prom.sh) will create a default version of this file - suitable for SDP and non-SDP

## License Update Scheduling

`p4 license -u` can trigger a costly server statistics scan when p4d's cached
figures expire on P4 Server 2026.1 and later. Use `license_updates` to restrict
potentially expensive refreshes to off-hours while still collecting cached figures:

```yaml
license_updates:
  mode: scheduled
  times: ["01:00"]           # One or more daily start times, quoted HH:MM
  timezone: Europe/London    # IANA timezone; omit to use the host timezone
  start_window: 5m           # Maximum delay after a scheduled start
```

- `mode: every_update` is the default when this section is omitted. It collects
  once per normal metrics update, unless the previous license call is still running.
- Sample and installer-generated configuration explicitly uses `scheduled` at
  `01:00` in the host timezone. Installation and updates add this section only
  when absent; existing license settings and memory-limit sections are preserved.
  Review the time before use, particularly on large servers. HMS hostname-specific
  configuration takes precedence and is not modified by the installer.
- `mode: scheduled` requires at least one daily time. Startup and SIGHUP reload
  do not force a statistics refresh. On each normal metrics update, p4metrics
  reads `p4 counter depotStats` and extracts the epoch `lastCalculated` value.
  If the statistics are less than 24 hours old, `p4 license -u` can run normally.
  Otherwise it checks `p4 monitor show -l`: an active `calculateStats` command
  (state `R` or `B`) also permits the license call outside a start window.
  With neither condition satisfied, the license call requires an unused
  scheduled start window. Exactly 24 hours old is treated as expired.
  Windows are inclusive and must be greater than zero and less than 24 hours;
  the default is `5m`.
- `mode: disabled` never starts a license call and removes the license metrics
  file. A call already running is allowed to finish; its figures are cached but
  not published while disabled.
- Scheduling is checked approximately every second, independently of
  `update_interval`, when the main monitoring loop is available. If another
  collector delays the loop past the start window, that occurrence is skipped.
- Each occurrence permits at most one potential refresh attempt. Failed
  attempts are not retried in that window, and occurrences falling while a
  call is running are skipped. Overlapping start windows result in one attempt,
  not a backlog. Safe cached reads can still run on normal metrics updates.
- A nonexistent daylight-saving time is skipped; a repeated time runs only
  once per local date. Avoid transition hours if a daily call is essential.

License commands run asynchronously so a lengthy scan does not block the other
collectors. The counter and monitor preflight commands also run asynchronously.
Missing or zero timestamps are treated as unknown; malformed or future timestamps
are logged and treated as unknown. Unknown statistics require either an active
`calculateStats` or an unused start window. Failed counter/monitor commands are
logged and skip the license call, retaining the previous metrics. On older
servers without `depotStats`, scheduled mode consequently remains window-only.
`every_update` explicitly retains the legacy behaviour without these checks.
The normal server-info calls use `p4 info -s` to avoid triggering statistics scans.

Between calls, the last successful figures are retained, including
after failures. `p4_license_time_remaining` is recalculated from cached expiry
timestamps on each normal metrics update. The gauge
`p4_license_last_success_timestamp_seconds` reports the last successful
collection time (Unix seconds), or `0` before any successful collection.
For example, alert on stale figures with
`time() - p4_license_last_success_timestamp_seconds > 3 * 86400`.

Cached figures and attempted slots are saved atomically in a `p4_license*.json`
state file beside the corresponding `.prom` file. This prevents a restart or
reload from repeating an attempted slot and restores cached figures after a
restart. Keep the state file writable by p4metrics. State read/parse errors
are logged and prevent collection; failure to save a scheduled attempt also
prevents that call. `--dry.run` does not write state or metrics files and still
runs eligible read commands.

Choose a start time allowing the scan to finish before working hours. This
controls only calls made by p4metrics: other clients may trigger scans, and a
start window is not a completion deadline. Counter and monitor checks are
snapshots, not an atomic server-side guarantee; p4metrics and p4d clocks should
be synchronized. The start window is rechecked after preflight before starting
an expired-statistics refresh. With a server cache expiring 24 hours
after a four-hour scan finishes, a daily 01:00 call may trigger an expensive
scan only every other night. Changing or disabling the schedule does not
terminate a server-side scan already in progress.

## Memory Limits Configuration

Memory limit enforcement is configured via the `memlimits` section in p4metrics.yaml:

```yaml
memlimits:
  enabled: true                    # Enable memory tracking (default: false)
  enforce_kills: false             # Terminate violating processes (default: false - safe)
  candidate_cmds: "sync|edit|submit"  # Only these commands eligible for termination (optional filter)
  
  groups:
    - description: "standard_users"
      users: "^(?!svc_)"            # Regex: regular users, exclude service accounts
      
      # Per-command limits (evaluated individually)
      cmd_max_percentage: 30        # Single command max 30% of system memory
      cmd_max_value: "2G"           # Single command max 2GB
      
      # Per-user cumulative limits (all user's processes combined)
      user_cumulative_max_percentage: 50   # User's processes max 50% of system
      user_cumulative_max_value: "5G"     # User's processes max 5GB
    
    - description: "service_accounts"
      users: "^svc_"                # Service account pattern
      cmd_max_percentage: 80        # Higher limits for service accounts
      user_cumulative_max_percentage: 90
```

**Threshold Types**:

1. **cmd_max_percentage**: Individual command's memory as % of total system memory
2. **cmd_max_value**: Individual command's absolute memory limit (supports K/M/G/T suffixes)
3. **user_cumulative_max_percentage**: All processes by a user combined as % of system
4. **user_cumulative_max_value**: All processes by a user combined in absolute bytes

**Safety Features**:

- **enforce_kills: false** by default - violations detected but processes not terminated
- Use `--dry.run` flag to preview what would be killed without actually terminating
- Evaluation always runs if enabled, enforcement is opt-in
- Individual kill failures are logged but don't stop processing
- Only running processes (State='R') are evaluated

### OOM Notification Rate Limiting

```yaml
notifications:
  cooldown_seconds: 300
  slack:
    enabled: false
    mode: webhook
```

`notifications.cooldown_seconds` defaults to `300` (five minutes), including
for existing configurations that omit it. Set it to `0` to disable rate limiting;
negative values are rejected.

The first OOM alert is sent immediately. After a successful send, further alerts
are suppressed during the cooldown even if PIDs, users, or memory usage change.
Candidate and actual-kill alerts have separate cooldowns, as do direct Slack
delivery (webhook or bot) and the vmagent alert gateway. Failed sends are logged
and do not start a cooldown, allowing retries on the next monitoring update.
After the cooldown, the next detected violation can send another alert.

This limits notifications only: memory metrics and kill enforcement continue on
every update. Cooldowns are kept in memory, survive SIGHUP configuration reloads,
and reset when p4metrics restarts. Apply configuration changes with SIGHUP or
`systemctl restart p4metrics`.

Cumulative user-limit alerts group candidates by user, reason, and threshold,
listing their process count and PIDs on one line. `candidate_rss` is the summed
RSS of those candidates, not the user's total memory usage. Each summary also
reports `user_commands` (all entries for that user in the current monitor
snapshot), counts by every observed state (including idle `I` and `T`), and an
`IDLE` command count. IDLE is a subset of the total, not an additional state.
Counts include non-candidates and entries whose memory could not be read.
Individual command-limit alerts retain their per-process format.

Gateway JSON retains the existing `candidates` array and adds
`candidate_groups` for cumulative limits. Each group contains `user`, `pids`,
`process_count`, `rss_bytes`, `threshold`, `reason`, and `user_commands`
(`total`, `by_state`, `idle`). The gateway's Slack renderer must consume these
groups to display the compact format; direct Slack notifications use it
automatically. Grouping does not change process selection or enforcement.

## Metrics

p4metrics emits the following key metrics:

### Memory Limit Metrics (when enabled)

- **p4_memory_pct_by_cmd** (gauge, label: cmd) - Memory percentage used by processes running each command
- **p4_memory_pct_by_user** (gauge, label: user) - Memory percentage used by processes running as each user
- **p4_memlimit_kill_candidates** (gauge) - Current count of processes exceeding memory thresholds
- **p4_memlimit_kills_total** (counter) - Cumulative count of processes killed by memory limit enforcement

### Journal Metrics

- **p4_journal_records_count{table,record}** (counter) - Cumulative count of parsed P4JOURNAL records by table and action type.
- `action` label values are `rv`, `pv`, and `dv`.
- `table` label values are parsed from journal table names such as `db.domain` and emitted without the `db.` prefix (for example `domain`).
- Controlled by config option `parse_journal` (default: `true`).

### SDP Directory Content Metrics

When `sdp_instance` is configured, p4metrics scans `$LOGS`, `$P4TMP`, and `$CHECKPOINTS` and emits these gauges with a `directory` label (`logs`, `p4tmp`, or `checkpoints`):

- **p4_sdp_directory_scan_success** - `1` when the scan completes; `0` for an unreadable, missing, or over-limit directory.
- **p4_sdp_directory_file_count** - Count of regular files.
- **p4_sdp_directory_size_bytes** - Total size of regular files in bytes.
- **p4_sdp_directory_oldest_file_age_seconds** - Age of the oldest regular file; omitted for an empty directory.
- **p4_sdp_directory_file_count_by_age{age}** - File count by age range: `lt_1h`, `lt_1d`, `lt_7d`, `lt_30d`, or `gte_30d`.

Scans recurse without following symlinks and stop at 100,000 regular files. A failed or incomplete scan emits only `p4_sdp_directory_scan_success` with a value of `0`; it does not publish partial counts.

### Other Metrics

See [p4prometheus main documentation](../../README.md#metrics) for complete metrics list including license, filesys, process counts, verify, and other monitoring metrics.

## Design

The basics are:

- Run as systemd service (as constantly tailing the structured error log (errors.csv))
- Poll p4d, swarm etc as per the delay in the config file
- Handle p4d service being down (so don't die but wait for next cycle!)
- Optionally monitor and enforce memory limits on running processes
- Log all enforcement actions for operational visibility

## Safety & Operational Notes

### Memory Limits: Safe by Default

The memory limits feature is designed with safety in mind:

1. **Enforcement Disabled by Default**: Set `enforce_kills: false` in config (the default) to detect violations without terminating processes
2. **Dry-Run Testing**: Use `--dry.run` flag to preview memory limit enforcement without actually killing processes
3. **Non-Fatal Errors**: If a process termination fails, the failure is logged but processing continues
4. **State Filtering**: Only running processes (State='R') are evaluated; sleeping/idle processes are not affected
5. **Graceful Degradation**: Memory limit evaluation doesn't prevent other metrics collection

### Logging & Observability

When memory limits are enabled, p4metrics logs:

- **INFO level**: All process terminations and summary counts
- **DEBUG level**: Detailed per-candidate information (PID, user, command, reason, memory usage)
- **WARN level**: Any failures (e.g., if a kill command fails)

Enable debug logging with `--debug` flag to see full details:

```bash
./p4metrics --config p4metrics.yaml --debug --dry.run
```

### Performance Considerations

Memory limit evaluation has minimal overhead:

- Reads from /proc filesystem (Linux only, not macOS/Windows)
- Pure function evaluation, no external calls (unless enforce_kills enabled)
- Skips processes not in "R" (running) state
- Respects `candidate_cmds` filter to reduce evaluation scope

## Troubleshooting

**Q: I've set `enforce_kills: true` but processes aren't being killed. Why?**

A: Check these in order:

1. Is `memlimits.enabled: true` in your config?
2. Are processes actually exceeding the thresholds? Check logs with `--debug`
3. Is the `p4 monitor terminate` command available to the user running p4metrics?
4. Are you using `--dry.run`? This prevents actual termination

**Q: How do I safely test memory limit enforcement?**

A: Use `--dry.run` mode:

```bash
./p4metrics --config p4metrics.yaml --debug --dry.run > test.log &
tail -f test.log
```

This shows what would be killed without actually terminating processes.

**Q: My memory limits never trigger. How do I verify they're configured correctly?**

A: Check the parsed limits in debug output:

```bash
./p4metrics --config p4metrics.yaml --debug 2>&1 | grep -i "memlimit\|memory"
```

Look for lines showing memory percentages per command/user.

**Q: Can I kill specific commands but not others?**

A: Yes, use the `candidate_cmds` filter:
```yaml
memlimits:
  enabled: true
  enforce_kills: true
  candidate_cmds: "sync|edit"    # Only these commands can be killed
  groups: [...]                   # Groups still apply to all processes for tracking
```

**Q: Do memory limits work on Windows?**

A: Memory tracking is Linux-only (uses /proc filesystem). On Windows/macOS, the metrics are skipped with debug logging. Other p4metrics functionality works normally.

**Q: What happens if `p4 monitor terminate` fails?**

A: The failure is logged at WARN level with error details, but processing continues. The process is not counted as successfully killed, so the `p4_memlimit_kills_total` counter won't increment.

## P4DAM/Search Metrics

Notes for future implementation!

```bash
$ p4 property -l | grep Search
P4.P4Search.URL = https://p4search.das.perforce.com:443

$ curl https://p4search.das.perforce.com:443/api/v1/status
{
  "status": {
    "code": 200,
    "message": "OK"
  },
  "data": [
    {
      "name": "license",
      "title": "Helix Core Search license",
      "status": "SUCCESS",
      "description": "License valid."
    },
    {
      "name": "p4-status",
      "title": "Helix Core server connection",
      "status": "SUCCESS",
      "description": "P4PORT: ssl:p4poke.perforce.com:1667"
    },
    {
      "name": "p4-auth",
      "title": "Helix Core Service authentication",
      "status": "SUCCESS",
      "description": "P4USER: svc-p4poke-p4search-auth"
    },
    {
      "name": "p4-index",
      "title": "Helix Core Index authentication",
      "status": "SUCCESS",
      "description": "P4USER: svc-p4poke-p4search-auth"
    },
    {
      "name": "es-status",
      "title": "Elasticsearch Hosts",
      "status": "WARNING",
      "description": "Using insecure Elasticsearch."
    },
    {
      "name": "es-plugin",
      "title": "Elasticsearch Protect plugin",
      "status": "SUCCESS",
      "description": "Plugin: p4search-filter(enabled)"
    },
    {
      "name": "auto-detect",
      "title": "Image detection service",
      "status": "SUCCESS",
      "description": "Azure Computer Vision - Content tagging"
    },
    {
      "name": "OCR",
      "title": "Optical Character Recognition service",
      "status": "SUCCESS",
      "description": "Azure Computer Vision - Optical Character Recognition"
    },
    {
      "name": "Speech",
      "title": "Speech to Text service",
      "status": "SUCCESS",
      "description": "Azure Speech to Text"
    },
    {
      "name": "tess-status",
      "title": "Tesseract",
      "status": "SUCCESS",
      "description": "Tesseract installed and enabled"
    },
    {
      "name": "url-status",
      "title": "Helix Search External Url",
      "status": "SUCCESS",
      "description": "OK: 200"
    },
    {
      "name": "render-status",
      "title": "Render Service Status",
      "status": "FAIL",
      "description": "Local - Rendering service. No workers registered."
    }
  ]
}%

$ curl https://p4search.das.perforce.com:443/api/v1/build
{
  "status": {
    "code": 200,
    "message": "OK"
  },
  "data": "2024.3.2686736"
}%
```

## Swarm Redis metrics

Possible for future:

```bash
$ redis-cli -p 7379 --scan --pattern 'Swarm^dev:*' | cut -d^ -f 2 | sort | uniq -c | grep -vE 'status$'
    136 dev:group
      1 dev:GroupsModelGroup
      2 dev:includes
 116790 dev:path
    135 dev:project
      1 dev:ProjectsModelProject
      2 dev:starts_with
   5166 dev:user
      1 dev:UsersModelUser
      2 dev:workflow
      1 dev:WorkflowModelWorkflow
```
