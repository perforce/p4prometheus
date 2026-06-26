# Grafana Dashboards

These are the same dashboards that have been uploaded to Grafana website and from which they can be imported to create a new dashboard.

- https://grafana.com/grafana/dashboards/12278 - P4 Stats
- https://grafana.com/grafana/dashboards/15509 - P4 Stats (non-SDP)

Because they are imports, they have this special JSON section at the top:

```[yaml]
  "__inputs": [
    {
      "name": "ds_prometheus",
      "label": "Prometheus",
      "description": "Standard Prometheus source (or VictoriaMetrics)",
      "type": "datasource",
      "pluginId": "prometheus",
      "pluginName": "Prometheus"
    }
  ],
```

and then panels refer to the datasource like this:

```[yaml]
      "datasource": {
        "type": "prometheus",
        "uid": "${ds_prometheus}"
      },
```

- `__inputs` is only used during dashboard import (UI flow)
- It defines placeholders (typically datasources) that must be resolved when importing
- Grafana shows a dropdown and asks you to map them
- During the import process it resolves `${ds_prometheus}` → actual datasource UID/name

## Updates

The above used to be `"datasource": "${ds_prometheus}"` in older versions of Grafana.
