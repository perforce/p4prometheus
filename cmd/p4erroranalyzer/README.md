# p4erroranalyzer

`p4erroranalyzer` is a local/offline analyzer for structured Perforce `errors.csv` files.

The parser is schema-driven and supports multiple `errors.csv` layouts (for example log schema versions `0`, `50`, `55`, `58`) by using field mappings generated from filtered `p4 logschema -Aa` output.

It helps identify:
- frequent error signatures (`level + subsystem + error_id`)
- bursts/spikes in specific signatures by time bucket
- likely severe/system-impacting events from message patterns
- newly emerging signatures late in a file/window

## Build

```bash
go build ./cmd/p4erroranalyzer
```

## Regenerate CSV Schema Mappings

If your Perforce server introduces a new `Error`/`FatalError` layout, refresh the generated mapping table:

```bash
cd cmd/p4erroranalyzer
p4 logschema -Aa > logschema.txt
python3 generate_errschema_go.py --in logschema.txt --out error_schema_generated.go
```

Or use:

```bash
cd cmd/p4erroranalyzer
go generate
```

## Regenerate Error Name Lookup

To map `(f_subsys, f_subcode)` from `errors.csv` to short names (for example `CLIENT_LockCheckFail`), regenerate:

```bash
cd cmd/p4erroranalyzer
python3 generate_error_lookup_go.py --errors all_errors.txt --errornum errornum.h --out error_lookup_generated.go
```

Or run `go generate` in this directory.

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
- `-triage-ollama-model deepseek-coder:6.7b` enable local LLM triage via Ollama
- `-triage-ollama-url http://localhost:11434/api/generate` Ollama endpoint
- `-triage-out /tmp/triage.json` optional triage JSON envelope output file

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

## Local LLM triage (Ollama)

Use deterministic anomaly detection first, then ask a local LLM to triage only the detected anomalies.

One-command flow (recommended):

```bash
go run ./cmd/p4erroranalyzer \
	-file test/errors-16002.csv \
	-json \
	-triage-ollama-model deepseek-coder:6.7b \
	-triage-out /tmp/p4error-triage.json
```

Two-step flow (script helper):

1. Generate analyzer JSON:

```bash
go run ./cmd/p4erroranalyzer -file test/errors-16002.csv -json > anomaly-report.json
```

2. Run local triage with Ollama:

```bash
python3 cmd/p4erroranalyzer/ollama_triage.py \
	--report anomaly-report.json \
	--model llama3:8b \
	--out anomaly-triage.json
```

3. Validate shape using schema:

```bash
cat cmd/p4erroranalyzer/triage.output.schema.json
```

Notes:
- Default Ollama endpoint is `http://localhost:11434/api/generate`.
- The integrated triage and Python helper both ask for strict JSON output with ranked findings.
- Keep this as a triage/explainer layer; anomaly detection remains statistical in `p4erroranalyzer`.

## Notes

The list of all errors was generated from P4API *.cc in msgs:

```
/p4/msgs$ ls *.cc | grep -v msghelp | grep -v msgconfig | grep -v msgspec | while read f; do perl -0777 -ne 'print "$1\n" while /(ErrorId\s+Msg.*?;)/sg' $f | grep -v DEPRECATED | grep -v E_INFO >> all.txt; done
```

Then the script `generate_error_lookup_go.py` creates `error_lookup_generated.go`
