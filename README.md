# p4prometheus
Perforce (Helix Core) interface for writing Prometheus metrics from real-time analysis of p4d log files.

Uses [go-libp4dlog](https://github.com/rcowham/go-libp4dlog) for actual parsing.

This is a cmd line utility to continuously parse log files and write a summary to 
a specified Prometheus compatible metrics file which can be handled via node_exporter
textfile collector module.

## Overview

This is a solution consisting of the following components:

* Prometheus - time series metrics management system: https://prometheus.io/
* Grafana - The leading open source software for time series analytics - https://grafana.com/
* node_exporter - Prometheus collector for basic Linux metrics - https://github.com/prometheus/node_exporter

Two custom components:

* p4prometheus - This component.
* monitor_metrics.sh - [SDP](https://swarm.workshop.perforce.com/projects/perforce-software-sdp) compatible bash script to generate simple supplementary metrics - [monitor_metrics.sh](https://swarm.workshop.perforce.com/files/guest/perforce_software/sdp/dev/Server/Unix/p4/common/site/bin/monitor_metrics.sh)

Check out the [Prometheus architecture](https://prometheus.io/assets/architecture.png) - the custom components are "Prometheus targets".

## Installation Overview

1. Download latest release and install in `/usr/local/bin`
2. Create a simple service (/etc/systemd/system/) and run the following as say user `perforce`.

    ```/usr/local/bin/p4prometheus -config /p4/common/config/p4prometheus.yaml```

See below for detailed steps.

## Config file

For standard SDP installation:

```yaml
log_path:       /p4/1/logs/log
metrics_output: /p4/metrics/cmds.prom
server_id:      
sdp_instance:   1
```

Note that server_id can be explicitly specified or will be automatically read from /p4/`instance`/root/server.id

# Detailed Installation

You need to install Prometheus and Grafana using standard methods. I recommend installing latest released binaries - haven't yet found good packages.

In addition you would normally install on a seperate VM/machine to the Perforce server itself (for security and HA reasons).

## Install node_exporter

This must be done on the Perforce (Helix Core) server machine (ditto for any other boxes to be maintained).

Run the following as root:

    sudo useradd --no-create-home --shell /bin/false node_exporter

    export PVER="0.18.0"
    wget https://github.com/prometheus/node_exporter/releases/download/v$PVER/node_exporter-$PVER.linux-amd64.tar.gz

    tar xvf node_exporter-$PVER.linux-amd64.tar.gz 
    
    mv node_exporter-$PVER.linux-amd64/node_exporter /usr/local/bin/

Create a metrics directory, give ownership to account writing metrics, and make sure it has global read access (so `node_exporter` account can read entries)

    mkdir /hxlogs/metrics

    chown perforce:perforce /hxlogs/metrics
    
    ls -al /hxlogs/metrics

Ensure the above has global read access (perforce user will write files, node_exporter will read them).

Create service file:

```bash
cat << EOF > /etc/systemd/system/node_exporter.service
[Unit]
Description=Node Exporter
Wants=network-online.target
After=network-online.target

[Service]
User=node_exporter
Group=node_exporter
Type=simple
ExecStart=/usr/local/bin/node_exporter --collector.textfile.directory="/hxlogs/metrics"

[Install]
WantedBy=multi-user.target
EOF
```

Start and enable service:

    sudo systemctl daemon-reload
    sudo systemctl start node_exporter
    sudo systemctl status node_exporter
    sudo systemctl enable node_exporter

Check logs for service in case of errors:

    journalctl -u node_exporter --no-pager | tail

Check that metrics are being exposed:

    curl http://localhost:9100/metrics | less

