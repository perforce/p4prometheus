# Prometheus Rule Templating Ideas

## Goal

Generate standard Prometheus rule YAML from shared specifications and per-environment values. This can reduce duplication between the general, custom, and P4RA rule sets while keeping the generated files usable by Prometheus, VictoriaMetrics, and Pint.

The first implementation renders the general and custom rule sets. P4RA remains a
real-world reference only and is not part of this repository's ytt build.

## Recommended Tool

Use [ytt](https://carvel.dev/ytt/) as a YAML-aware renderer invoked by `make`.

Why ytt:

- It understands YAML structure instead of performing blind text replacement.
- Its directives begin with `#@`, so Prometheus and Alertmanager templates such as `{{ $labels.instance }}` pass through unchanged.
- It supports data values, conditional rules, loops, and reusable YAML fragments.
- Generated files remain ordinary Prometheus YAML with no runtime dependency on ytt.

Avoid generic text templating such as Jinja or Go templates for this use case. Alert annotations already use `{{ ... }}`, which makes escaping and review more error-prone.

## Suggested Layout

```text
examples/prometheus/
  rules-src/
    perforce_rules.yml       # General rule template
    default-values.yml       # Default values for general rules
    custom-values.yml        # Custom values for the example rules
    custom_perforce_rules.yml # Custom rule template
  perforce_rules.yml         # Generated general rules
  custom_perforce_rules.yml  # Generated custom rules
  Makefile
```

Keep generated YAML committed. This lets reviewers inspect the exact deployed PromQL and lets consumers that do not install ytt use the generated files directly.

## Example: Parameterized License Rule

Template fragment (`rules-src/perforce_rules.yml`):

```yaml
#@ load("@ytt:data", "data")

- alert: P4D license expiry
  expr: >-
    (p4_license_time_remaining{
      serverid!~"#@ data.values.labels.license_excluded_serverids"
    } / (24 * 60 * 60)) < #@ data.values.license_warning_days
  for: 6h
  labels:
    severity: low
  annotations:
    summary: 'Endpoint {{ $labels.instance }} license due to expire (in {{ $value | printf "%.02f" }} days)'
```

Values (`rules-src/default-values.yml`):

```yaml
#@data/values
---
labels:
  license_excluded_serverids: ".*ffr.*|.*edge.*|.*EDGE.*|.*[-_]ha.*"
license_warning_days: 14
```

Generated expression:

```promql
(p4_license_time_remaining{
  serverid!~".*ffr.*|.*edge.*|.*EDGE.*|.*[-_]ha.*"
} / (24 * 60 * 60)) < 14
```

The current two exclusions on `serverid` can be combined safely into one regex, simplifying the template and the generated rule.

## Example: Parameterized Checkpoint Rule

Template fragment (`rules-src/perforce_rules.yml`):

```yaml
- alert: Checkpoint Not Taken
  expr: >-
    ((time() - p4_sdp_checkpoint_log_time{
      serverid=~"#@ data.values.labels.checkpoint_serverids",
      instance!~"#@ data.values.labels.checkpoint_excluded_instances"
    }) / 3600) > #@ data.values.checkpoint_max_age_hours
  for: #@ data.values.checkpoint_for
  labels:
    severity: warning
  annotations:
    summary: 'Endpoint {{ $labels.instance }} checkpoint missing warning ({{ $value | printf "%.02f" }} hours)'
```

General values:

```yaml
#@data/values
---
labels:
  checkpoint_serverids: ".*master.*|.*edge.*|.*EDGE.*"
  checkpoint_excluded_instances: ".*p4p.*|.*proxy.*|.*[-_]ha.*"
checkpoint_max_age_hours: 25
checkpoint_for: 1h
```

## Example: Custom Values Override Defaults

Pass the default values file before a site's custom file. `ytt` merges the maps,
and the later custom value wins while unmentioned values are inherited.

```yaml
# rules-src/default-values.yml
#@data/values
---
license:
  urgent_days: 5
  warning_days: 14
```

```yaml
# rules-src/custom-values.yml
#@data/values
---
license:
  warning_days: 30
custom:
  checkpoint:
    maximum_duration_minutes: 200
```

The rendered rules use `30` for `license.warning_days` and retain `5` for
`license.urgent_days`. The custom checkpoint example similarly uses `200`
instead of its default duration.

```make
custom_perforce_rules.yml: $(RULES_SRC)/perforce_rules.yml $(RULES_SRC)/default-values.yml $(RULES_SRC)/custom-values.yml
	ytt -f $(RULES_SRC)/perforce_rules.yml -f $(RULES_SRC)/default-values.yml -f $(RULES_SRC)/custom-values.yml > $@
```

## Example: Custom Log-Rate Rule

Template fragment:

```yaml
#@ if data.values.enable_log_rate_alert:
- alert: NoLogs
  expr: >-
    rate(p4_prom_log_lines_read{
      sdpinst="#@ data.values.sdp_instance",
      serverid="#@ data.values.server_id"
    }[1m]) < #@ data.values.minimum_log_lines_per_second
  for: 10m
  labels:
    severity: critical
  annotations:
    summary: "Endpoint {{ $labels.instance }} too few log lines"
#@ end
```

Custom values:

```yaml
#@data/values
---
enable_log_rate_alert: true
sdp_instance: "1"
server_id: "master-1666"
minimum_log_lines_per_second: 100
```

For the general rule set, set `enable_log_rate_alert: false`; no `NoLogs` rule is generated.

## Example Make Targets

```make
RULES_SRC := rules-src
GENERATED := perforce_rules.yml custom_perforce_rules.yml

render: $(GENERATED)

perforce_rules.yml: $(RULES_SRC)/perforce_rules.yml $(RULES_SRC)/default-values.yml
  ytt -f $(RULES_SRC)/perforce_rules.yml -f $(RULES_SRC)/default-values.yml > $@

custom_perforce_rules.yml: $(RULES_SRC)/custom_perforce_rules.yml $(RULES_SRC)/default-values.yml $(RULES_SRC)/custom-values.yml
  ytt -f $(RULES_SRC)/custom_perforce_rules.yml -f $(RULES_SRC)/default-values.yml -f $(RULES_SRC)/custom-values.yml > $@

validate: render
  promtool check rules $(GENERATED)
  pint --config ./pint_vm.hcl --show-duplicates lint $(GENERATED)

check-generated: render
  git diff --exit-code -- $(GENERATED)
```

Use `make render` to regenerate the files, `make check-generated` in CI to detect stale output, and `make validate` to run Prometheus, VictoriaMetrics, and Pint checks when those tools are installed.

## Rules for Shared and Site-Specific Policy

- Generate one complete rule set per deployment profile; do not load a generated base file and a generated override file that define the same alert.
- Make each generated alert name unique within a deployed rules configuration.
- Use values for true policy differences: thresholds, durations, label selectors, mountpoint patterns, service names, and whether an optional rule exists.
- Keep unusual site-only rules in a small profile file, for example custom `NoLogs` and `CheckpointSlow` rules.
- Do not parameterize every literal. A template should make recurring policy decisions clearer, not obscure straightforward PromQL.

## Open Decisions

1. Is ytt acceptable as a build-time dependency for maintainers and CI?
2. Should generated YAML be committed, or only built in release/installation packaging?
3. Should custom rules be an overlay loaded after general rules, or become one fully generated profile to avoid duplicate-alert concerns?
4. Which differences are supported profiles versus one-off site policy that should remain local only?
5. Should the installer distribute templates, generated files, or only the current general rules plus a local customization file?
6. Can CI install and run `promtool`, Pint, and optionally `vmalert-prod` for every generated profile?
