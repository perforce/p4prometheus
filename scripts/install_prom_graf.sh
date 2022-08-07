#!/bin/bash
# Installs the following: node_exporter, prometheus, victoriametrics, grafana and alertmanager
# This is the monitoring machine

# shellcheck disable=SC2128
if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# ============================================================
# Configuration section

VER_NODE_EXPORTER="1.3.1"
VER_PROMETHEUS="2.33.5"
VER_ALERTMANAGER="0.23.0"
VER_PUSHGATEWAY="1.4.2"
VER_VICTORIA_METRICS="1.74.0"

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

function usage
{
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for install_prom_graf.sh:
 
    install_prom_graf.sh [-push]

or

    install_prom_graf.sh -h

  -push Means install pushgateway (otherwise it won't be installed)

"
   if [[ "$style" == -man ]]; then
       # Add full manual page documentation here.
      true
   fi

   exit 2
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i InstallPushgateway=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h;;
        (-push) InstallPushgateway=1;;
        (-man) usage -man;;
        (-*) usage -h "Unknown command line option ($1).";;
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

declare -i SELinuxEnabled=0

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "Downloading $url"
    wget -q "$url" || bail "Failed to download $url"
    tar zxvf "$fname" || bail "Failed to untar $fname"
}

check_os () {
    grep ubuntu /proc/version > /dev/null 2>&1
    isubuntu="${?}"
    grep centos /proc/version > /dev/null 2>&1
    # shellcheck disable=SC2034
    iscentos="${?}"
    grep redhat /proc/version > /dev/null 2>&1
    # shellcheck disable=SC2034
    isredhat="${?}"
}

install_grafana () {

    if [[ $isubuntu -eq 0 ]]; then

        apt-get install -y apt-transport-https software-properties-common wget
        wget -q -O - https://packages.grafana.com/gpg.key | sudo apt-key add -
        echo "deb https://packages.grafana.com/oss/deb stable main" | sudo tee -a /etc/apt/sources.list.d/grafana.list

        apt-get update
        apt-get install -y grafana

    else    # Assume CentOS

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
    fi

    systemctl daemon-reload
    systemctl start grafana-server
    systemctl status grafana-server --no-pager

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

    cd /tmp || bail "failed to cd"
    PVER="$VER_ALERTMANAGER"
    fname="alertmanager-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/alertmanager/releases/download/v$PVER/$fname"

    mv alertmanager-$PVER.linux-amd64 alertmanager-files

    for base_file in alertmanager amtool; do
        bin_file=/usr/local/bin/$base_file
        cp alertmanager-files/$base_file /usr/local/bin/
        chown $userid:$userid $bin_file
        chmod 755 $bin_file
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t $bin_file
            restorecon -vF $bin_file
        fi
    done

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

    cat << EOF > /etc/alertmanager/alertmanager.yml
global:
  smtp_from: alertmanager@example.com
  smtp_smarthost: localhost:25
  smtp_require_tls: false
  # Hello is the local machine name
  smtp_hello: localhost

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
EOF

    echo -e "
validate:
\\tamtool check-config alertmanager.yml

restart:
\\tsystemctl restart alertmanager
" >  /etc/alertmanager/Makefile

    chown $userid:$userid /etc/alertmanager/alertmanager.yml /etc/alertmanager/Makefile

    systemctl daemon-reload
    systemctl enable alertmanager
    systemctl start alertmanager
    systemctl status alertmanager --no-pager

}

install_node_exporter () {

    userid="node_exporter"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    PVER="$VER_NODE_EXPORTER"
    fname="node_exporter-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/node_exporter/releases/download/v$PVER/$fname"

    mv node_exporter-$PVER.linux-amd64/node_exporter /usr/local/bin/
    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=/usr/local/bin/node_exporter
        semanage fcontext -a -t bin_t $bin_file
        restorecon -vF $bin_file
    fi

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
  --collector.systemd.unit-include=(p4.*|node_exporter)\.service

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable node_exporter
    systemctl start node_exporter
    systemctl status node_exporter --no-pager
}

install_victoria_metrics () {

    userid="prometheus"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    PVER="$VER_VICTORIA_METRICS"
    for fname in victoria-metrics-amd64-v$PVER.tar.gz vmutils-amd64-v$PVER.tar.gz; do
        download_and_untar "$fname" "https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/v$PVER/$fname"
    done

    for base_file in victoria-metrics-prod vmagent-prod vmalert-prod vmauth-prod vmbackup-prod vmrestore-prod vmctl-prod; do
        bin_file=/usr/local/bin/$base_file
        mv $base_file /usr/local/bin/
        chown $userid:$userid $bin_file
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t $bin_file
            restorecon -vF $bin_file
        fi
    done

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
    systemctl status victoria-metrics --no-pager
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

    cd /tmp || bail "failed to cd"
    PVER="$VER_PROMETHEUS"
    fname="prometheus-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/prometheus/releases/download/v$PVER/prometheus-$PVER.linux-amd64.tar.gz"

    mv prometheus-$PVER.linux-amd64 prometheus-files

    for base_file in prometheus promtool; do
        bin_file=/usr/local/bin/$base_file
        cp prometheus-files/$base_file /usr/local/bin/
        chown $userid:$userid $bin_file
        chmod 755 $bin_file
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t $bin_file
            restorecon -vF $bin_file
        fi
    done

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
    # ==========================================================
    # CONFIGURE THESE VALUES AS APPROPRIATE FOR YOUR SERVERS!!!!
    # ==========================================================
    - targets:
        - localhost:9100

  # ==========================================================
  # This section can be deleted if pushgateway not in use
  # ==========================================================
  - job_name: 'pushgateway'
    honor_labels: true
    # Optional auth settings if this is configured for security
    # basic_auth:
    #   username: admin
    #   password: SomeSecurePassword
    static_configs:
      - targets:
          - localhost:9091

# Send to VictoriaMetrics - better for performance and long term storage
remote_write:
  - url: http://localhost:8428/api/v1/write

EOF

    echo -e "
validate:
\\tpromtool check config prometheus.yml

restart:
\\tsystemctl restart prometheus
" >  /etc/prometheus/Makefile

    chown "$userid:$userid" /etc/prometheus/prometheus.yml /etc/prometheus/Makefile

    systemctl daemon-reload
    systemctl enable prometheus
    systemctl start prometheus
    systemctl status prometheus --no-pager
}


install_pushgateway () {

    userid="pushgateway"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    PVER="$VER_PUSHGATEWAY"
    fname="pushgateway-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/pushgateway/releases/download/v$PVER/$fname"

    mv pushgateway-$PVER.linux-amd64/pushgateway /usr/local/bin/
    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=/usr/local/bin/pushgateway
        semanage fcontext -a -t bin_t $bin_file
        restorecon -vF $bin_file
    fi

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
 --persistence.interval=15m \
 --log.level=info

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable pushgateway
    systemctl start pushgateway
    systemctl status pushgateway --no-pager
}

check_os
install_node_exporter
install_alertmanager
install_victoria_metrics
install_prometheus
[[ $InstallPushgateway -eq 1 ]] && install_pushgateway
install_grafana

echo "

Should have installed node_exporter, prometheus and friends.

Please review config files, and adjust as necessary (reloading/restarting services as appropriate):

    /etc/prometheus/prometheus.yml
    /etc/alertmanager/alertmanager.yml

"
