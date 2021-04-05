#!/bin/bash
# Installs the following: node_exporter, prometheus, victoriametrics, grafana and alertmanager
# This is the monitoring machine

if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# ============================================================
# Configuration section

VER_NODE_EXPORTER="1.1.2"
VER_PROMETHEUS="2.23.0"
VER_ALERTMANAGER="0.21.0"
VER_PUSHGATEWAY="1.4.0"
VER_VICTORIA_METRICS="1.48.0"

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage
{
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for install_prom_graf.sh:
 
install_prom_graf.sh <instance>
 
   or
 
monitor_metrics.sh -h
"
}

# Command Line Processing
 
declare -i shiftArgs=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h;;
        # (-man) usage -man;;
        (-*) usage -h "Unknown command line option ($1)." && exit 1;;
    esac
 
    # Shift (modify $#) the appropriate number of times.
    shift; while [[ "$shiftArgs" -gt 0 ]]; do
        [[ $# -eq 0 ]] && usage -h "Incorrect number of arguments."
        shiftArgs=$shiftArgs-1
        shift
    done
done
set -u

if [[ $(id -u) -ne 0 ]]; then
   echo "$0 can only be run as root or via sudo"
   exit 1
fi

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    wget -q "$url"
    tar zxvf "$fname"
}

install_grafana () {

    cat << EOF > /etc/yum.repos.d/grafana.repo
[grafana]
name=grafana
baseurl=https://packages.grafana.com/oss/rpm
repo_gpgcheck=1
enabled=1
gpgcheck=1
gpgkey=https://packages.grafana.com/gpg.key
sslverify=1
sslcacert=/etc/pki/tls/certs/ca-bundle.crt
EOF

    yum install -y grafana
    systemctl daemon-reload
    systemctl start grafana-server
    systemctl status grafana-server

}

install_alertmanager () {

    userid="alertmanager"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    mkdir /etc/alertmanager
    mkdir /var/lib/alertmanager
    chown $userid:$userid /etc/alertmanager
    chown $userid:$userid /var/lib/alertmanager

    cd /tmp
    PVER="$VER_ALERTMANAGER"
    fname="alertmanager-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/alertmanager/releases/download/v$PVER/$fname"

    mv alertmanager-$PVER.linux-amd64 alertmanager-files

    cp alertmanager-files/alertmanager /usr/local/bin/
    cp alertmanager-files/amtool /usr/local/bin/
    chown $userid:$userid /usr/local/bin/alertmanager
    chown $userid:$userid /usr/local/bin/amtool
    chmod 755 /usr/local/bin/alertmanager
    chmod 755 /usr/local/bin/amtool

    cat << EOF > /etc/systemd/system/alertmanager.service
[Unit]
Description=Alertmanager
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=/usr/local/bin/alertmanager --config.file=/etc/alertmanager/alertmanager.yml \
    --storage.path=/var/lib/alertmanager --log.level=debug

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable alertmanager
    systemctl start alertmanager
    systemctl status alertmanager

}

install_node_exporter () {

    userid="node_exporter"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp
    PVER="$VER_NODE_EXPORTER"
    fname="node_exporter-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/node_exporter/releases/download/v$PVER/$fname"

    mv node_exporter-$PVER.linux-amd64/node_exporter /usr/local/bin/

    cat << EOF > /etc/systemd/system/node_exporter.service
[Unit]
Description=Node Exporter
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=/usr/local/bin/node_exporter --collector.systemd \
        --collector.systemd.unit-include="(p4.*|node_exporter)\.service"

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable node_exporter
    systemctl start node_exporter
    systemctl status node_exporter
}

install_victoria_metrics () {

    userid="prometheus"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp
    PVER="$VER_VICTORIA_METRICS"
    for fname in victoria-metrics-v$PVER.tar.gz zxvf vmutils-v$PVER.tar.gz; do
        download_and_untar "$fname" "https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v$PVER/$fname"
    done

    mv victoria-metrics-prod /usr/local/bin/
    mv vmagent-prod /usr/local/bin/
    mv vmalert-prod /usr/local/bin/
    mv vmauth-prod /usr/local/bin/
    mv vmbackup-prod /usr/local/bin/
    mv vmrestore-prod /usr/local/bin/

    cat << EOF > /etc/systemd/system/victoria-metrics.service
[Unit]
Description=Victoria Metrics
Wants=network-online.target
After=network-online.target
 
[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=/usr/local/bin/victoria-metrics-prod \
    -storageDataPath /var/lib/victoria-metrics/ \
    -retentionPeriod=6
 
[Install]
WantedBy=multi-user.target
EOF

    mkdir /var/lib/victoria-metrics
    chown -R $userid:$userid /var/lib/victoria-metrics

    systemctl daemon-reload
    systemctl enable victoria-metrics
    systemctl start victoria-metrics
    systemctl status victoria-metrics
}

install_prometheus () {

    userid="prometheus"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    mkdir /etc/prometheus
    mkdir /var/lib/prometheus
    chown $userid:$userid /etc/prometheus
    chown $userid:$userid /var/lib/prometheus

    cd /tmp
    PVER="$VER_PROMETHEUS"
    fname="prometheus-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/prometheus/releases/download/v$PVER/prometheus-$PVER.linux-amd64.tar.gz"

    mv prometheus-$PVER.linux-amd64 prometheus-files

    cp prometheus-files/prometheus /usr/local/bin/
    cp prometheus-files/promtool /usr/local/bin/
    chown $userid:$userid /usr/local/bin/prometheus
    chown $userid:$userid /usr/local/bin/promtool
    chmod 755 /usr/local/bin/prometheus
    chmod 755 /usr/local/bin/promtool

    cp -r prometheus-files/consoles /etc/prometheus
    cp -r prometheus-files/console_libraries /etc/prometheus
    chown -R $userid:$userid /etc/prometheus/consoles
    chown -R $userid:$userid /etc/prometheus/console_libraries

    cat << EOF > /etc/systemd/system/prometheus.service
[Unit]
Description=Prometheus
Wants=network-online.target
After=network-online.target
 
[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=/usr/local/bin/prometheus \
    --config.file /etc/prometheus/prometheus.yml \
    --storage.tsdb.path /var/lib/prometheus/ \
    --web.console.templates=/etc/prometheus/consoles \
    --web.console.libraries=/etc/prometheus/console_libraries
 
[Install]
WantedBy=multi-user.target
EOF

    cat << EOF > /etc/prometheus/prometheus.yml
global:
  scrape_interval:     15s # Set the scrape interval to every 15 seconds. Default is every 1 minute.
  evaluation_interval: 15s # Evaluate rules every 15 seconds. The default is every 1 minute.
  # scrape_timeout is set to the global default (10s).

# Alertmanager configuration - optional
alerting:
  alertmanagers:
  - static_configs:
    - targets:
        - localhost:9093

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
    # CONFIGURE THESE VALUES AS APPROPRIATE FOR YOUR SERVERS!!!!
    - targets:
        - localhost:9100

  - job_name: 'pushgateway'
    honor_labels: true
    static_configs:
      - targets:
          - localhost:9091

# Send to VictoriaMetrics
remote_write:
  - url: http://localhost:8428/api/v1/write

EOF

    chown "$userid:$userid" /etc/prometheus/prometheus.yml

    systemctl daemon-reload
    systemctl enable prometheus
    systemctl start prometheus
    systemctl status prometheus
}


install_pushgateway () {

    userid="pushgateway"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp
    PVER="$VER_PUSHGATEWAY"
    fname="pushgateway-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/pushgateway/releases/download/v$PVER/$fname"

    mv pushgateway-$PVER.linux-amd64/pushgateway /usr/local/bin/

    cat << EOF > /etc/systemd/system/pushgateway.service
[Unit]
Description=Node Exporter
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=/usr/local/bin/pushgateway \
    --web.listen-address=:9091 \
    --web.telemetry-path=/metrics \
    --persistence.file=/tmp/metric.store \
    --persistence.interval=5m \
    --log.level=info

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable pushgateway
    systemctl start pushgateway
    systemctl status pushgateway
}

install_node_exporter
# install_alertmanager
install_victoria_metrics
install_prometheus
install_pushgateway
install_grafana

echo "

Should have installed node_exporter, prometheus and friends.

"
