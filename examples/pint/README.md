# Using Prometheus Linter (pint)

The Cloudflare Prometheus linter [Github project](https://github.com/cloudflare/pint) does validation of your alerting rules.

This will validate various things which can otherwise cause alerts to "silently" fail - it makes things much more reliable.
See [their blog article](https://blog.cloudflare.com/monitoring-our-monitoring/)

This includes things like:

- incorrect syntax for rules
- invalid queries - non-existent data

The motivation was to catch rules that try to query metrics that are missing or when the query was simply mistyped. To do that pint will run each query from every alerting and recording rule to see if it returns any result, if it doesn’t then it will break down this query to identify all individual metrics and check for the existence of each of them. If any of them is missing or if the query tries to filter using labels that aren’t present on any time series for a given metric then it will report that back to us.

For compatibility with VictoriaMetrics, we have a fork of the original project (waiting for a PR to be accepted!):

- [Github fork](https://github.com/rcowham/pint/tree/victoria_metrics)

The above allows us to use VictoriaMetrics' MetricsQL syntax and functions - which would cause errors with base PromQL as used by Prometheus.

## Automated install with install_prom_graf.sh

You can install Pint and create a `systemd` service automatically via:

```bash
sudo ./scripts/install_prom_graf.sh -pint
```

This installs Pint from:

- https://github.com/rcowham/pint/releases/tag/v0.87.0

and creates:

- Binary: `/usr/local/bin/pint` (or custom `-b` directory)
- Config: `/etc/prometheus/pint_vm.hcl`
- Service: `/etc/systemd/system/pint.service`

The service runs:

```bash
pint watch glob /etc/prometheus/perforce_rules.yml --config /etc/prometheus/pint_vm.hcl
```

## Basic Configuration files

### Prometheus version - pint.hcl

This is very simple

```bash
# Point pint at prometheus
prometheus "monitor" {
  uri     = "http://localhost:9090"
  timeout = "60s"
}

# Disable smelly selectors warning in promql/regexp check.
check "promql/regexp" {
  smelly = false
}
```

### VictoriaMetrics version - pint_vm.hcl

```bash
# Point pint at VictoriaMetrics
prometheus "monitor" {
  uri     = "http://localhost:8428"
  timeout = "60s"
}

# Enable VictoriaMetrics mode to report syntax errors as warnings
# instead of fatal errors when MetricsQL extensions are used.
# This requires the version of pint: https://github.com/rcowham/pint/tree/victoria_metrics
parser {
  victoria_metrics = true
}

# Disable smelly selectors warning in promql/regexp check.
check "promql/regexp" {
  smelly = false
}

# Disable checks with don't work with Victoria Metrics (lacks config and metadata endpoints)
checks {
  disabled = ["promql/rate", "promql/range_query"]
}
```

## Makefile to run validation

Install in the same directory as your rules file etc, e.g. `/etc/prometheus`

```bash
# cat Makefile
# Makefile for prometheus rules checking

validate:
	promtool check config prometheus.yml
	vmalert-prod -dryRun -rule perforce_rules.yml
	vmauth-prod -dryRun -auth.config vmauth.yml
	# Use Cloudflare linter https://github.com/cloudflare/pint
	pint --config ./pint_vm.hcl --show-duplicates lint perforce_rules.yml

restart: validate
	sudo systemctl restart prometheus
	sudo systemctl restart alertmanager
	sudo systemctl restart vmalert
	sudo systemctl restart vmauth
```

### Sample output

```bash
# make
promtool check config prometheus.yml
Checking prometheus.yml
 SUCCESS: prometheus.yml is valid prometheus config file syntax

vmalert-prod -dryRun -rule perforce_rules.yml
2026-07-15T08:19:01.027Z	info	VictoriaMetrics/lib/logger/flag.go:12	build version: vmalert-20251201-110213-tags-v1.131.0-0-g84658e77da
2026-07-15T08:19:01.028Z	info	VictoriaMetrics/lib/logger/flag.go:13	command-line flags
2026-07-15T08:19:01.028Z	info	VictoriaMetrics/lib/logger/flag.go:20	  -dryRun="true"
2026-07-15T08:19:01.028Z	info	VictoriaMetrics/lib/logger/flag.go:20	  -rule="perforce_rules.yml"
2026-07-15T08:19:01.028Z	info	VictoriaMetrics/app/vmalert/config/log/logger.go:52	found 1 files to read from "Local FS{MatchPattern: \"perforce_rules.yml\"}"
2026-07-15T08:19:01.028Z	info	VictoriaMetrics/app/vmalert/config/log/logger.go:52	finished reading 1 files in 27.88µs from "Local FS{MatchPattern: \"perforce_rules.yml\"}"
vmauth-prod -dryRun -auth.config vmauth.yml
2026-07-15T08:19:01.038Z	info	VictoriaMetrics/lib/logger/flag.go:12	build version: vmauth-20251201-110224-tags-v1.131.0-0-g84658e77da
2026-07-15T08:19:01.038Z	info	VictoriaMetrics/lib/logger/flag.go:13	command-line flags
2026-07-15T08:19:01.039Z	info	VictoriaMetrics/lib/logger/flag.go:20	  -auth.config="vmauth.yml"
2026-07-15T08:19:01.039Z	info	VictoriaMetrics/lib/logger/flag.go:20	  -dryRun="true"
2026-07-15T08:19:01.040Z	info	VictoriaMetrics/app/vmauth/auth_config.go:712	loaded information about 51 users from -auth.config="vmauth.yml"
# Use Cloudflare linter https://github.com/cloudflare/pint
pint --config ./pint_vm.hcl --show-duplicates lint perforce_rules.yml
level=INFO msg="Loading configuration file" path=./pint_vm.hcl
level=INFO msg="Finding all rules to check" paths=["perforce_rules.yml"]
level=INFO msg="Configured new Prometheus server" name=monitor uris=1 uptime=up tags=[] include=[] exclude=[]
level=INFO msg="Checking Prometheus rules" entries=48 workers=10 online=true
level=INFO msg="VictoriaMetrics mode enabled, PromQL syntax errors will be reported as warnings"
Warning: MetricsQL-specific syntax detected (promql/syntax)
  ---> perforce_rules.yml:100-101 -> `Vmagent not responding`
100 |         lag(node_time_seconds[24h]) > 5 * 60
              ^^^ unknown function with name "lag"
101 |             and on(instance) (p4_ra_instance_active > 0)

Warning: MetricsQL-specific syntax detected (promql/syntax)
  ---> perforce_rules.yml:114-115 -> `Vmagent not responding (Critical Care)`
114 |         lag(node_time_seconds[24h]) > 5 * 60
              ^^^ unknown function with name "lag"
115 |             and on(instance) (p4_ra_instance_active > 0 and p4_ra_critical_care == 1)

level=INFO msg="Problems found" Warning=2
```

In the above we can ignore the Warnings - which are just relating to MetricsQL.

## Systemd service file

By setting this up as a service we get regular checking and thus alerting.

### Version for VictoriaMetrics

```bash
# /etc/systemd/system/pint.service
[Unit]
Description=Prometheus Pint validation tool
Wants=network-online.target
After=network-online.target

[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=/usr/local/bin/pint watch glob /etc/prometheus/perforce_rules.yml --config /etc/prometheus/pint_vm.hcl

[Install]
WantedBy=multi-user.target
```

### Version for Prometheus

```bash
# /etc/systemd/system/pint.service
[Unit]
Description=Prometheus Pint validation tool
Wants=network-online.target
After=network-online.target

[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=/usr/local/bin/pint watch rule_files monitor --config /etc/prometheus/pint.hcl

[Install]
WantedBy=multi-user.target
```
