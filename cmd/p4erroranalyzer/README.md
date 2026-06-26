# p4erroranalyzer

`p4erroranalyzer` is a local/offline analyzer for structured Perforce `errors.csv` files.

It helps identify:
- frequent error signatures (`level + subsystem + error_id`)
- bursts/spikes in specific signatures by time bucket
- likely severe/system-impacting events from message patterns
- newly emerging signatures late in a file/window

## Build

```bash
go build ./cmd/p4erroranalyzer
```

## Usage

```bash
./p4erroranalyzer -file /p4/1/logs/errors.csv
```

Useful flags:
- `-bucket 5m` (default `5m`) for spike detection granularity
- `-min-spike-count 8` minimum per-bucket count to consider as spike
- `-z 4.0` z-score threshold for spikes
- `-top 10` top signatures/users/commands to print
- `-json` machine-readable JSON output

Example:

```bash
./p4erroranalyzer -file test/errors-16002.csv -bucket 2m -top 15 -json > anomaly-report.json
```

## Server installation pattern

1. Build and copy binary to server:

```bash
go build -o /usr/local/bin/p4erroranalyzer ./cmd/p4erroranalyzer
```

2. Run from cron/systemd timer for periodic summaries.
3. Keep output under a rotating directory, for example `/p4/metrics/error-analysis/`.
4. Optional: ship JSON output to your central log/observability stack.
