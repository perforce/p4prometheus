# p4proxyprometheus

`p4proxyprometheus` tails a Helix Proxy (`p4p`) log and writes Prometheus
textfile metrics. It uses the proxy log parser from `go-libp4dlog`; it does not
create SQL or JSON output.

## Metrics

The collector emits these metrics:

* `p4_proxy_commands_completed_total`
* `p4_proxy_command_duration_seconds_total`
* `p4_proxy_files_total{source="server|cache"}`
* `p4_proxy_bytes_total{source="server|cache"}`
* `p4_proxy_last_command_completion_timestamp_seconds`

Counters describe commands observed since the collector started. Set
`read_all: true` when starting against a historical log to establish totals
from its current contents; leave it `false` for ordinary continuous operation.

## Configuration and running

On Linux with systemd, the repository provides installers for a single proxy
collector and node_exporter. Run as root using a release that contains the
`p4proxyprometheus` assets:

```bash
sudo ./scripts/install_p4proxyprom.sh --version <version> --instance 1 \
  --p4p-log /p4/1/logs/p4p.log -u perforce
sudo ./scripts/update_p4proxyprom.sh --version <new-version>
```

The scripts require `p4prom_common.sh` alongside them. The installer refuses
to replace existing proxy or node_exporter services. It defaults to
`/var/metrics` for the textfile collector and `/etc/p4prometheus` for config;
use `-m`, `-c`, and `-b` for custom paths. Custom metrics and binary directories
must already exist. For a custom configuration directory, pass `-c` again on
updates. Both scripts accept `--local-tarballs-dir` for air-gapped installs;
run either with `--help` for the expected asset names. The updater preserves
the YAML and systemd units.

Copy and edit [p4proxyprometheus.yaml](./p4proxyprometheus.yaml), ensuring
that `metrics_output` is in the directory configured for node_exporter's
textfile collector. `sdp_instance` is required; the collector writes
`<metrics_output-base>-<sdp_instance>.prom`. The `.prom` suffix on
`metrics_output` is optional: both `/p4/metrics/p4_proxy` and
`/p4/metrics/p4_proxy.prom` produce `/p4/metrics/p4_proxy-1.prom` when
`sdp_instance` is `"1"`.

```bash
p4proxyprometheus --config p4proxyprometheus.yaml
```

Command-line overrides are available for the input and output paths:

```bash
p4proxyprometheus \
  --config /etc/p4proxyprometheus.yaml \
  --p4plog /p4/1/logs/p4p.log \
  --metrics.output /p4/metrics/p4_proxy.prom
```
