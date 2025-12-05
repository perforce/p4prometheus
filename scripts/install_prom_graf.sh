#!/bin/bash
# Installs the following: node_exporter, prometheus, victoriametrics, grafana and alertmanager
# This is the monitoring machine

set -e  # Exit on error
set -o pipefail  # Catch errors in pipes

# shellcheck disable=SC2128
if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# ============================================================
# Configuration section - Updated to current versions as of Dec 2025

VER_NODE_EXPORTER="1.8.2"
VER_PROMETHEUS="2.54.1"
VER_ALERTMANAGER="0.27.0"
VER_PUSHGATEWAY="1.9.0"
VER_VICTORIA_METRICS="1.105.0"

# Default to amd but allow arm architecture
arch="amd64"
[[ $(uname -p) == 'aarch64' ]] && arch="arm64"

# Cleanup function
TEMP_FILES=()
cleanup() {
    if [[ ${#TEMP_FILES[@]} -gt 0 ]]; then
        echo "Cleaning up temporary files..."
        for f in "${TEMP_FILES[@]}"; do
            [[ -f "$f" ]] && rm -f "$f"
            [[ -d "$f" ]] && rm -rf "$f"
        done
    fi
}

trap cleanup EXIT

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; cleanup; exit "${2:-1}"; }

function check_service_exists() {
    local service=$1
    systemctl list-unit-files | grep -q "^${service}.service" && return 0 || return 1
}

function usage
{
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
}

# Command Line Processing

declare -i shiftArgs=0
declare -i InstallPushgateway=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 1;;
        (-push) InstallPushgateway=1;;
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

declare -i SELinuxEnabled=0

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi

download_and_untar () {
    local fname=$1
    local url=$2
    
    TEMP_FILES+=("$fname")
    
    if [[ -f "$fname" ]]; then
        msg "File $fname already exists, removing..."
        rm -f "$fname"
    fi
    
    msg "Downloading $url"
    if ! wget -q --show-progress "$url"; then
        bail "Failed to download $url"
    fi
    
    msg "Extracting $fname"
    tar zxf "$fname" || bail "Failed to untar $fname"
}

check_os () {
    # Use /etc/os-release for better OS detection
    if [[ -f /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        OS_ID="$ID"
        OS_VERSION="$VERSION_ID"
        msg "Detected OS: $NAME $VERSION_ID"
    else
        # Fallback to old method
        if grep -q ubuntu /proc/version 2>/dev/null; then
            OS_ID="ubuntu"
        elif grep -q centos /proc/version 2>/dev/null; then
            OS_ID="centos"
        elif grep -q redhat /proc/version 2>/dev/null; then
            OS_ID="rhel"
        else
            bail "Unable to detect operating system"
        fi
    fi
    
    # Set compatibility flags
    case "$OS_ID" in
        ubuntu|debian)
            isubuntu=0
            ;;
        centos|rhel|rocky|almalinux|fedora)
            isubuntu=1
            ;;
        *)
            bail "Unsupported OS: $OS_ID. This script supports Ubuntu, Debian, CentOS, RHEL, Rocky Linux, AlmaLinux, and Fedora."
            ;;
    esac
}

install_grafana () {
    msg "Installing Grafana..."
    
    # Check if already installed
    if check_service_exists grafana-server; then
        msg "Grafana already installed"
    fi

    if [[ $isubuntu -eq 0 ]]; then
        # Modern approach for Ubuntu/Debian - no deprecated apt-key
        apt-get install -y apt-transport-https software-properties-common wget gnupg
        
        # Add GPG key the modern way
        wget -q -O - https://packages.grafana.com/gpg.key | gpg --dearmor | sudo tee /usr/share/keyrings/grafana-archive-keyring.gpg > /dev/null
        
        # Add repository with signed-by
        echo "deb [signed-by=/usr/share/keyrings/grafana-archive-keyring.gpg] https://packages.grafana.com/oss/deb stable main" | sudo tee /etc/apt/sources.list.d/grafana.list

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
    msg "Installing Alertmanager..."
    
    # Check if already installed and stop if running
    if check_service_exists alertmanager && systemctl is-active --quiet alertmanager; then
        msg "Stopping existing alertmanager service..."
        systemctl stop alertmanager
    fi

    userid="alertmanager"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    mkdir -p /etc/alertmanager
    mkdir -p /var/lib/alertmanager
    chown "$userid:$userid" /etc/alertmanager
    chown "$userid:$userid" /var/lib/alertmanager

    cd /tmp || bail "failed to cd"
    PVER="$VER_ALERTMANAGER"
    fname="alertmanager-$PVER.linux-${arch}.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/alertmanager/releases/download/v$PVER/$fname"
    TEMP_FILES+=("alertmanager-files")

    mv alertmanager-$PVER.linux-${arch} alertmanager-files

    for base_file in alertmanager amtool; do
        bin_file=/usr/local/bin/$base_file
        cp alertmanager-files/$base_file /usr/local/bin/
        chown "$userid:$userid" "$bin_file"
        chmod 755 "$bin_file"
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
            restorecon -vF "$bin_file"
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
# Makefile for alertmanager - default rule is validate
validate:
\\tamtool check-config alertmanager.yml

restart: validate
\\tsystemctl restart alertmanager
" >  /etc/alertmanager/Makefile

    chown $userid:$userid /etc/alertmanager/alertmanager.yml /etc/alertmanager/Makefile

    systemctl daemon-reload
    systemctl enable alertmanager
    systemctl start alertmanager
    systemctl status alertmanager --no-pager

}

install_node_exporter () {
    msg "Installing Node Exporter..."
    
    # Check if already installed and stop if running
    if check_service_exists node_exporter && systemctl is-active --quiet node_exporter; then
        msg "Stopping existing node_exporter service..."
        systemctl stop node_exporter
    fi

    userid="node_exporter"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    PVER="$VER_NODE_EXPORTER"
    fname="node_exporter-$PVER.linux-${arch}.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/node_exporter/releases/download/v$PVER/$fname"
    TEMP_FILES+=("node_exporter-$PVER.linux-${arch}")

    mv "node_exporter-$PVER.linux-${arch}/node_exporter" /usr/local/bin/
    chown "$userid:$userid" /usr/local/bin/node_exporter
    chmod 755 /usr/local/bin/node_exporter
    
    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=/usr/local/bin/node_exporter
        semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
        restorecon -vF "$bin_file"
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
    msg "Installing Victoria Metrics..."
    
    # Check if already installed and stop if running
    if check_service_exists victoria-metrics && systemctl is-active --quiet victoria-metrics; then
        msg "Stopping existing victoria-metrics service..."
        systemctl stop victoria-metrics
    fi

    userid="prometheus"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    PVER="$VER_VICTORIA_METRICS"
    for fname in victoria-metrics-linux-${arch}-v$PVER.tar.gz vmutils-linux-${arch}-v$PVER.tar.gz; do
        download_and_untar "$fname" "https://github.com/victoriametrics/victoriametrics/releases/download/v$PVER/$fname"
        TEMP_FILES+=("$fname")
    done

    for base_file in victoria-metrics-prod vmagent-prod vmalert-prod vmauth-prod vmbackup-prod vmrestore-prod vmctl-prod; do
        if [[ -f "$base_file" ]]; then
            bin_file=/usr/local/bin/$base_file
            mv "$base_file" /usr/local/bin/
            chown "$userid:$userid" "$bin_file"
            chmod 755 "$bin_file"
            if [[ $SELinuxEnabled -eq 1 ]]; then
                semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
                restorecon -vF "$bin_file"
            fi
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

    mkdir -p /var/lib/victoria-metrics
    chown -R "$userid:$userid" /var/lib/victoria-metrics

    systemctl daemon-reload
    systemctl enable victoria-metrics
    systemctl start victoria-metrics
    systemctl status victoria-metrics --no-pager
}

install_prometheus () {
    msg "Installing Prometheus..."
    
    # Check if already installed and stop if running
    if check_service_exists prometheus && systemctl is-active --quiet prometheus; then
        msg "Stopping existing prometheus service..."
        systemctl stop prometheus
    fi

    userid="prometheus"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    mkdir -p /etc/prometheus
    mkdir -p /var/lib/prometheus
    chown "$userid:$userid" /etc/prometheus
    chown "$userid:$userid" /var/lib/prometheus

    cd /tmp || bail "failed to cd"
    PVER="$VER_PROMETHEUS"
    fname="prometheus-$PVER.linux-${arch}.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/prometheus/releases/download/v$PVER/prometheus-$PVER.linux-${arch}.tar.gz"
    TEMP_FILES+=("prometheus-files")

    mv prometheus-$PVER.linux-${arch} prometheus-files

    for base_file in prometheus promtool; do
        bin_file=/usr/local/bin/$base_file
        cp "prometheus-files/$base_file" /usr/local/bin/
        chown "$userid:$userid" "$bin_file"
        chmod 755 "$bin_file"
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
            restorecon -vF "$bin_file"
        fi
    done

    cp -r prometheus-files/consoles /etc/prometheus
    cp -r prometheus-files/console_libraries /etc/prometheus
    chown -R "$userid:$userid" /etc/prometheus/consoles
    chown -R "$userid:$userid" /etc/prometheus/console_libraries

    cat << EOF > /etc/systemd/system/prometheus.service
[Unit]
Description=Prometheus
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
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
    # Note that the names here will appear as labels in your metrics.
    # So recommend not using IP address as not very user friendly!
    # The port is going to be 9100 by default for node_exporter unless using Windows Exporter targets are specified
    # ==========================================================
    - targets:
        - localhost:9100
        - my_p4_server:9100

  # ==========================================================
  # This section SHOULD BE DELETED if pushgateway not in use
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
# Makefile for prometheus - default rule is validate
validate:
\\tpromtool check config prometheus.yml

# Validate before restarting
restart: validate
\\tsystemctl restart prometheus
" >  /etc/prometheus/Makefile

    chown "$userid:$userid" /etc/prometheus/prometheus.yml /etc/prometheus/Makefile

    systemctl daemon-reload
    systemctl enable prometheus
    systemctl start prometheus
    systemctl status prometheus --no-pager
}


install_pushgateway () {
    msg "Installing Pushgateway..."
    
    # Check if already installed and stop if running
    if check_service_exists pushgateway && systemctl is-active --quiet pushgateway; then
        msg "Stopping existing pushgateway service..."
        systemctl stop pushgateway
    fi

    userid="pushgateway"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    PVER="$VER_PUSHGATEWAY"
    fname="pushgateway-$PVER.linux-${arch}.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/pushgateway/releases/download/v$PVER/$fname"
    TEMP_FILES+=("pushgateway-$PVER.linux-${arch}")

    mv "pushgateway-$PVER.linux-${arch}/pushgateway" /usr/local/bin/
    chown "$userid:$userid" /usr/local/bin/pushgateway
    chmod 755 /usr/local/bin/pushgateway
    
    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=/usr/local/bin/pushgateway
        semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
        restorecon -vF "$bin_file"
    fi
    
    # Create directory for persistence file
    mkdir -p /var/lib/pushgateway
    chown "$userid:$userid" /var/lib/pushgateway

    cat << EOF > /etc/systemd/system/pushgateway.service
[Unit]
Description=Prometheus Pushgateway
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=/usr/local/bin/pushgateway \
 --web.listen-address=:9091 \
 --web.telemetry-path=/metrics \
 --persistence.file=/var/lib/pushgateway/metric.store \
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

msg "Starting installation process..."
msg "Architecture: $arch"

check_os

msg "Installing components..."
install_node_exporter
install_alertmanager
install_victoria_metrics
install_prometheus
[[ $InstallPushgateway -eq 1 ]] && install_pushgateway
install_grafana

msg ""
msg "Verifying installations..."
for service in node_exporter alertmanager victoria-metrics prometheus grafana-server; do
    if systemctl is-active --quiet "$service" 2>/dev/null; then
        msg "✓ $service is running"
    else
        msg "✗ $service is NOT running - check logs with: journalctl -u $service"
    fi
done

if [[ $InstallPushgateway -eq 1 ]]; then
    if systemctl is-active --quiet pushgateway 2>/dev/null; then
        msg "✓ pushgateway is running"
    else
        msg "✗ pushgateway is NOT running - check logs with: journalctl -u pushgateway"
    fi
fi

echo "

Should have installed node_exporter, prometheus and friends.

Please review the following config files, and adjust as necessary (reloading/restarting services as appropriate):

AT THE VERY LEAST YOU WILL NEED TO CHANGE THE TARGETS PROMETHEUS IS SCRAPING!!!

    cd /etc/prometheus
    vi prometheus.yml
    make
    make restart

    # Ditto for alertmanager config file if you are using it
    /etc/alertmanager/alertmanager.yml

"
