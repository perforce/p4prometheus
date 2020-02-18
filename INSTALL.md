# Installation Details for P4Prometheus and Other Components

- [Installation Details for P4Prometheus and Other Components](#installation-details-for-p4prometheus-and-other-components)
  - [Package Install of Grafana](#package-install-of-grafana)
  - [Ansible Installation](#ansible-installation)
- [Configure prometheus components](#configure-prometheus-components)
  - [Run installation](#run-installation)
- [Manual Installation](#manual-installation)
  - [Install node_exporter](#install-nodeexporter)
  - [Install p4prometheus - details](#install-p4prometheus---details)
- [Install prometheus](#install-prometheus)
  - [Prometheus config](#prometheus-config)
  - [Start and enable service](#start-and-enable-service)
- [Alerting](#alerting)
  - [Grafana Dashboard](#grafana-dashboard)
  - [Alerting rules](#alerting-rules)
  - [Alertmanager config](#alertmanager-config)
  - [Install monitor_metrics cron job](#install-monitormetrics-cron-job)
- [Troubleshooting](#troubleshooting)
  - [p4prometheus](#p4prometheus)
  - [monitor_metrics](#monitormetrics)
  - [node_exporter](#nodeexporter)
  - [prometheus](#prometheus)
  - [Grafana](#grafana)
- [Advanced config options](#advanced-config-options)
- [Windows Installation](#windows-installation)

## Package Install of Grafana

Use the appropriate link below depending if you using `apt` or `yum`:

* https://grafana.com/docs/grafana/latest/installation/debian/
* https://grafana.com/docs/grafana/latest/installation/rpm/

## Ansible Installation

This is the quickest way to install with a little bit of configuration for your setup.

Example files are in the `demo` folder of this project.

Assumptions:
* ansible installed (e.g. `pip install ansible`) - see [installation](https://docs.ansible.com/ansible/latest/installation_guide/intro_installation.html#intro-installation-guide)
* appropriate use ssh access to various machines referenced
* appropriate sudo access for current account on the various machines (to install services)

Configure the `hosts` file for your env with the 3 `groups`:
* master - the main p4d instance (node_exporter and p4prometheus)
* replicas - any replica machines to be monitored (as for master)
* monitor - the server where we will install Prometheus/Grafana and node_exporter

```ini
[master]
perforce01

[replicas]
replica_1
edge_1

[monitor]
monitor_1
```

# Configure prometheus components

    ansible-galaxy install william-yeh.prometheus

Create playbook, e.g. `install_prometheus.yml`:

```yaml
- hosts: monitor
  become: True
  roles:
    - william-yeh.prometheus

  vars:
    prometheus_components: [ "prometheus", "alertmanager", "node_exporter" ]
    prometheus_alertmanager_hostport: "localhost:9093"
    prometheus_use_systemd: True
    prometheus_use_service: False
    prometheus_conf_main: prometheus.yml
    # Review the following for latest releases - e.g. https://github.com/prometheus/alertmanager/releases
    prometheus_version:                 2.5.1
    prometheus_node_exporter_version:   0.18.1
    prometheus_alertmanager_version:    0.20.0

- hosts:
    - master
    - replicas
  become: True
  roles:
    - william-yeh.prometheus

  vars:
    prometheus_components: [ "node_exporter" ]
    prometheus_use_systemd: True
    prometheus_use_service: False
    prometheus_node_exporter_version:   0.18.1
```

Create `prometheus.yml` config file (installed by above) - pay particular attention to final list of `targets`

```yaml
global:
  scrape_interval:     15s # Set the scrape interval to every 15 seconds. Default is every 1 minute.
  evaluation_interval: 15s # Evaluate rules every 15 seconds. The default is every 1 minute.
  # scrape_timeout is set to the global default (10s).

# Alertmanager configuration
alerting:
  alertmanagers:
  - static_configs:
    - targets:
        - localhost:9093

# Load rules once and periodically evaluate them according to the global 'evaluation_interval'.
rule_files:
  # - "perforce_rules.yml"

scrape_configs:
  # The job name is added as a label `job=<job_name>` to any timeseries scraped from this config.
  - job_name: 'prometheus'
    static_configs:
    - targets: ['localhost:9090']

  - job_name: 'node_exporter'
    static_configs:
    - targets: ['localhost:9100', 'perforce01:9100', 'replica_1:9100', 'edge_1:9100']
```

Create `install_p4prometheus.yml` using example [install_p4prometheus.yml](demo/install_p4prometheus.yml)

You may need to adjust the `metrics_dir` variable. Note the script also copies over a p4prometheus config file: `p4prometheus.yml` (review this file and check it is correct).

## Run installation

    ansible-playbook -i hosts -v install_prometheus.yml

    ansible-playbook -i hosts -v install_p4prometheus.yml


# Manual Installation

Alternatively do the manual installation steps below, suitably customised for your environment.

- On monitoring server, install:
  - grafana
  - prometheus
  - node_exporter
  - alertmanager
- On your commit/master or any perforce replica servers, install:
  - p4prometheus
  - monitor_metrics.sh
  - node_exporter

## Install node_exporter

This must be done on the Perforce (Helix Core) server machine (ditto for any other servers such as replicas which are being monitored).

Run the following as root:

    sudo useradd --no-create-home --shell /bin/false node_exporter

    export PVER="0.18.1"
    wget https://github.com/prometheus/node_exporter/releases/download/v$PVER/node_exporter-$PVER.linux-amd64.tar.gz

    tar xvf node_exporter-$PVER.linux-amd64.tar.gz 
    mv node_exporter-$PVER.linux-amd64/node_exporter /usr/local/bin/

If you are installing on a Helix Core commit or replica server, then create a metrics directory, give ownership to account writing metrics, and make sure it has global read access (so `node_exporter` account can read entries)

    mkdir /hxlogs/metrics
    chown perforce:perforce /hxlogs/metrics
    ls -al /hxlogs/metrics

Ensure the above has global read access (e.g. user `perforce` will write files, user `node_exporter` will read them).

Create service file (note value for directory - may need to be adjusted).

Note that when installing on the monitoring machine, you do not need the /hxlogs/metrics directory, and you don't need the `collector.textfile.directory` parameter in the service file shown below.

```ini
cat << EOF > /etc/systemd/system/node_exporter.service
[Unit]
Description=Node Exporter
Wants=network-online.target
After=network-online.target

[Service]
User=node_exporter
Group=node_exporter
Type=simple
ExecStart=/usr/local/bin/node_exporter --collector.textfile.directory=/hxlogs/metrics

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

## Install p4prometheus - details

This must be done on the Perforce (Helix Core) server machine (and any replica machines).

This assumes SDP structure is in use on the server, and thus that user `perforce` exists.

Get latest release download link: https://github.com/rcowham/p4prometheus/releases

Run the following as `root` (using link copied from above page):

    export $PVER=0.4.0
    wget https://github.com/rcowham/p4prometheus/releases/download/v$PVER/p4prometheus.linux-amd64.gz

    gunzip p4prometheus.linux-amd64.gz
    
    chmod +x p4prometheus.linux-amd64

    mv p4prometheus.linux-amd64 /usr/local/bin/p4prometheus

As user `perforce`:

```bash
cat << EOF > /p4/common/config/p4prometheus.yaml
# sdp_instance: SDP instance - typically integer, but can be
# See: https://swarm.workshop.perforce.com/projects/perforce-software-sdp for more
# If this value is blank then it is assumed to be a non-SDP instance.
sdp_instance:   1
# log_path: Path to p4d server log
log_path:       /p4/1/logs/log
# metrics_output: Name of output file to write for processing by node_exporter.
# Ensure that node_exporter user has read access to this folder.
metrics_output: /hxlogs/metrics/p4_cmds.prom
# server_id: Optional - serverid for metrics - typically read from /p4/<sdp_instance>/root/server.id for 
# SDP installations - please specify a value if non-SDP install
server_id:      
# output_cmds_by_user: Whether to output metrics p4_cmd_user_counter/p4_cmd_user_cumulative_seconds
# Normally this should be set to true as the metrics are useful.
# If you have a p4d instance with thousands of users you may find the number
# of metrics labels is too great (one per distinct user), so set this to false.
output_cmds_by_user: true
# case_sensitive_server: if output_cmds_by_user=true then if this value is set to false
# all userids will be written in lowercase - otherwise as they occur in the log file
# If not present, this value will default to true on Windows and false otherwise.
case_sensitive_server: true
EOF
```

  chown perforce:perforce /p4/common/config/p4prometheus.yaml

As user `root`:

Create service file as below - parameters you may need to customise:

* User
* Group
* ExecStart

```ini
cat << EOF > /etc/systemd/system/p4prometheus.service
[Unit]
Description=P4prometheus
Wants=network-online.target
After=network-online.target

[Service]
User=perforce
Group=perforce
Type=simple
ExecStart=/usr/local/bin/p4prometheus --config=/p4/common/config/p4prometheus.yaml

[Install]
WantedBy=multi-user.target
EOF
```

Start and enable service:

    sudo systemctl daemon-reload
    sudo systemctl start p4prometheus
    sudo systemctl status p4prometheus
    sudo systemctl enable p4prometheus

Check logs for service in case of errors:

    journalctl -u p4prometheus --no-pager | tail

Check that metrics are being written:

    grep lines /hxlogs/metrics/p4_cmds.prom

# Install prometheus

This must be done on the monitoring machine only.

Run the following as root:

  sudo useradd --no-create-home --shell /bin/false prometheus
  sudo mkdir /etc/prometheus
  sudo mkdir /var/lib/prometheus
  sudo chown prometheus:prometheus /etc/prometheus
  sudo chown prometheus:prometheus /var/lib/prometheus

  export PVER="2.15.2"
  wget https://github.com/prometheus/prometheus/releases/download/v$PVER/prometheus-$PVER.linux-amd64.tar.gz

  tar xvf prometheus-$PVER.linux-amd64.tar.gz 
  mv prometheus-$PVER.linux-amd64 prometheus-files

  sudo cp prometheus-files/prometheus /usr/local/bin/
  sudo cp prometheus-files/promtool /usr/local/bin/
  sudo chown prometheus:prometheus /usr/local/bin/prometheus
  sudo chown prometheus:prometheus /usr/local/bin/promtool
  sudo chmod 755 /usr/local/bin/prometheus
  sudo chmod 755 /usr/local/bin/promtool

  sudo cp -r prometheus-files/consoles /etc/prometheus
  sudo cp -r prometheus-files/console_libraries /etc/prometheus
  sudo chown -R prometheus:prometheus /etc/prometheus/consoles
  sudo chown -R prometheus:prometheus /etc/prometheus/console_libraries

Create service file:

```ini
cat << EOF > /etc/systemd/system/prometheus.service
[Unit]
Description=Prometheus
Wants=network-online.target
After=network-online.target
 
[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=/usr/local/bin/prometheus \
    --config.file /etc/prometheus/prometheus.yml \
    --storage.tsdb.path /var/lib/prometheus/ \
    --web.console.templates=/etc/prometheus/consoles \
    --web.console.libraries=/etc/prometheus/console_libraries
 
[Install]
WantedBy=multi-user.target
EOF
```

## Prometheus config

```yaml
cat << EOF > /etc/prometheus/prometheus.yml
global:
  scrape_interval:     15s # Set the scrape interval to every 15 seconds. Default is every 1 minute.
  evaluation_interval: 15s # Evaluate rules every 15 seconds. The default is every 1 minute.
  # scrape_timeout is set to the global default (10s).

# Alertmanager configuration - optional
# alerting:
#   alertmanagers:
#   - static_configs:
#     - targets:
#         - localhost:9093

# Load rules once and periodically evaluate them according to the global 'evaluation_interval'.
# rule_files:
  # - "perforce_rules.yml"

# A scrape configuration containing exactly one endpoint to scrape:
# Here it's Prometheus itself.
scrape_configs:
  - job_name: 'prometheus'
    static_configs:
    - targets: ['localhost:9090']

  - job_name: 'node_exporter'
    static_configs:
    # CONFIGURE THESE VALUES FOR YOUR SERVERS!!!!
    - targets: ['p4hms:9100', 'p4main:9100', 'p4_ha:9100']

EOF
```

Make sure user has access:

  sudo chown prometheus:prometheus /etc/prometheus/prometheus.yml

## Start and enable service

    sudo systemctl daemon-reload
    sudo systemctl start prometheus
    sudo systemctl status prometheus
    sudo systemctl enable prometheus

Check logs for service in case of errors:

    journalctl -u prometheus --no-pager | tail

Check that prometheus web interface is running:

    curl http://localhost:9090/

Or open URL in a browser.

# Alerting

Done via alertmanager

Setup is very similar to the above.

Sample `/etc/systemd/system/alertmanager.service`:

```ini
[Unit]
Description=Alertmanager
Wants=network-online.target
After=network-online.target

[Service]
User=alertmanager
Group=alertmanager
Type=simple
ExecStart=/usr/local/bin/alertmanager --config.file=/etc/alertmanager/alertmanager.yml --storage.path=/var/lib/alertmanager --log.level=debug

[Install]
WantedBy=multi-user.target
```

* create alertmanager user
* create /etc/alertmanager directory

## Grafana Dashboard

See the [Sample dashboard](demo/p4_stats_dashboard.json) which is easy to import as a Grafana dashboard.

In addition we recommend one or more of the node_exporter dashboards for server stats, e.g.:

* https://grafana.com/grafana/dashboards/405
* https://grafana.com/grafana/dashboards/1860
* https://grafana.com/grafana/dashboards?search=node%20exporter

## Alerting rules

This is an example, assuming simple email and local postfix or equivalent have been setup.

It would be setup as /etc/prometheus/perforce_rules.yml

Then uncomment the relevant section in prometheus.yml

Please customize the below for things like `serverid` values and replica host names.

```yaml
groups:
- name: alert.rules
  rules:
  - alert: NoLogs
    expr: 100 > rate(p4_prom_log_lines_read{sdpinst="1",serverid="master"}[1m])
    for: 1m
    labels:
      severity: "critical"
    annotations:
      summary: "Endpoint {{ $labels.instance }} too few log lines"
      description: "{{ $labels.instance }} of job {{ $labels.job }} has been below target for more than 1 minutes."
  - alert: Replication Slow HA
    expr: p4_replica_curr_pos{instance="p4master:9100",job="node_exporter",sdpinst="1",servername="master"} - ignoring(serverid,servername) p4_replica_curr_pos{instance="p4master:9100",job="node_exporter",sdpinst="1",servername="p4d_ha_bos"} > 5e+7
    for: 10m
    labels:
      severity: "warning"
    annotations:
      summary: "Endpoint {{ $labels.instance }} replication warning"
      description: "{{ $labels.instance }} of job {{ $labels.job }} has been above target for more than 1 minutes."
  - alert: Replication Slow London
    expr: p4_replica_curr_pos{instance="p4master:9100",job="node_exporter",sdpinst="1",servername="master"} - ignoring(serverid,servername) p4_replica_curr_pos{instance="p4master:9100",job="node_exporter",sdpinst="1",servername="p4d_fr_lon"} > 5e+7
    for: 10m
    labels:
      severity: "warning"
    annotations:
      summary: "Endpoint {{ $labels.instance }} replication warning"
      description: "{{ $labels.instance }} of job {{ $labels.job }} has been above target for more than 1 minutes."
  - alert: Checkpoint slow
    expr: p4_sdp_checkpoint_duration{sdpinst="1",serverid="master"} > 50 * 60
    for: 5m
    labels:
      severity: "warning"
    annotations:
      summary: "Endpoint {{ $labels.instance }} checkpoint job duration longer than expected"
      description: "{{ $labels.instance }} of job {{ $labels.job }} has been above target for more than 1 minutes."
  - alert: Checkpoint not taken 
    expr: time() - p4_sdp_checkpoint_log_time{sdpinst="1",serverid="master"} > 25 * 60 * 60
    for: 5m
    labels:
      severity: "warning"
    annotations:
      summary: "Endpoint {{ $labels.instance }} checkpoint not taken in 25 hours warning"
      description: "{{ $labels.instance }} of job {{ $labels.job }} has been above target for more than 1 minutes."
  - alert: P4D service not running
    expr: node_systemd_unit_state{state="active",name="p4d_1.service"} != 1
    for: 5m
    labels:
      severity: "warning"
    annotations:
      summary: "Endpoint {{ $labels.instance }} p4d service not running"
      description: "{{ $labels.instance }} of job {{ $labels.job }} has been down for 5 minutes."
  - alert: DiskspaceLow
    expr: node_filesystem_free_bytes{mountpoint=~"/hx.*"} / node_filesystem_size_bytes{mountpoint=~"/hx.*"} * 100 < 10
    for: 5m
    labels:
      severity: "warning"
    annotations:
      summary: "Endpoint {{ $labels.instance }} disk space below 10%"
      description: "{{ $labels.instance }} of job {{ $labels.job }} has been below limit for 5 minutes."
```

## Alertmanager config

This is an example, assuming simple email and local postfix or equivalent setup - `/etc/alertmanager/alertmanager.yml`

```yaml
global:
  smtp_from: alertmanager@example.com
  smtp_smarthost: localhost:25
  smtp_require_tls: false
  # Hello is the local machine name
  smtp_hello: p4hms

route:
  group_by: ['alertname']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 60m
  receiver: mail

receivers:
- name: mail
  email_configs:
  - to: p4-group@example.com
```

## Install monitor_metrics cron job

Download [monitor_metrics.sh](demo/monitor_metrics.sh)

Configure it for your metrics directory (e.g. /hxlogs/metrics)

Install in crontab to run every minute:

    INSTANCE=1
    */1 * * * * /p4/common/site/bin/monitor_metrics.sh $INSTANCE > /dev/null 2>&1 ||:

For non-SDP installation:
    */1 * * * * /path/to/monitor_metrics.sh -p $P4PORT -u $P4USER -nosdp > /dev/null 2>&1 ||:

If not using SDP then please ensure that appropriate LONG TERM TICKET is setup in the environment
that this script is running.

# Troubleshooting

Make sure all firewalls are appropriate and the various components on each machine can see each other!

Port defaults are:
* Grafana: 3000
* Prometheus: 9090
* Node_exporter: 9100

## p4prometheus

If this is running correctly, it should write into the designated log file, e.g. `/hxlogs/metrics/p4_cmds.prom`

You can just grep for the most basic metric a couple of times (make sure it is increasing every minute or so):

    $ grep lines /p4/metrics/p4_cmds.prom 
    # HELP p4_prom_log_lines_read A count of log lines read
    # TYPE p4_prom_log_lines_read counter
    p4_prom_log_lines_read{serverid="master.1",sdpinst="1"} 7143

## monitor_metrics

Make sure monitor_metrics.sh is working:

```bash
bash -xv /p4/common/site/bin/monitor_metrics.sh 1
```

Or if not using SDP, copy the [monitor_metrics.sh script](demo/monitor_metrics.sh) to an appropriate place such as `/usr/local/bin` and install it in your crontab.

Check that appropriate files are listed in your metrics dir (and are being updated every minute), e.g.

    ls -l /hxlogs/metrics

## node_exporter

Make sure node_exporter is working (it is easy for there to be permissions access problems to the metrics dir).

Assuming you have installed the `/etc/systemd/system/node_exporter.service` then find the ExecStart line and run it manually and check for errors (optionally appending "--log.level=debug").

Check that you can see values:

    curl http://localhost:9100/metrics | grep p4_

## prometheus

Access page http://localhost:9090 in your browser and search for some metrics.

## Grafana

Check that a suitable data source is setup (i.e. Prometheus)

Use the `Explore` option to look for some basic metrics, e.g. just start typing p4_ and it should autocomplete if it has found p4_ metrics being collected.

# Advanced config options

For improved security:

* consider LDAP integration for Grafana
* implement appropriate authentication for the various end-points such as Prometheus/node_exporter

# Windows Installation

The above instructions are all for Linux. However, all the components have Windows binaries, with the exception of
monitor_metrics.sh. A version in Powershell is on the TODO list!

For p4prometheus you need to install as a service using for example [NSSM - Non Sucking Service Manager!](https://nssm.cc/)
