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

## Metrics

p4metrics emits the following key metrics:

### Memory Limit Metrics (when enabled)

- **p4_memory_pct_by_cmd** (gauge, label: cmd) - Memory percentage used by processes running each command
- **p4_memory_pct_by_user** (gauge, label: user) - Memory percentage used by processes running as each user
- **p4_memlimit_kill_candidates** (gauge) - Current count of processes exceeding memory thresholds
- **p4_memlimit_kills_total** (counter) - Cumulative count of processes killed by memory limit enforcement

### Journal Metrics

- **p4_journal_records_count{table,record}** (counter) - Cumulative count of parsed P4JOURNAL records by table and record type.
- `record` label values are `rv`, `pv`, and `dv`.
- `table` label values are parsed from journal table names such as `db.domain` and emitted without the `db.` prefix (for example `domain`).
- Controlled by config option `parse_journal` (default: `true`).

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
