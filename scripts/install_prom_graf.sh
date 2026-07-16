#!/bin/bash
# Installs the following: node_exporter, prometheus, victoriametrics, grafana and alertmanager
# This is the monitoring machine
#
# New in this version:
#   -d <data_root>            Set base directory for all runtime data (default: /var/lib)
#   -b <bin_dir>              Set directory for installed binaries (default: /usr/local/bin)
#   -r <months>               Set metrics retention period in months (default: 6)
#   -target <host:port>       Add a Prometheus scrape target (repeatable)
#   --local-tarballs-dir <p>  Use pre-staged local tarballs instead of downloading from GitHub

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

# Configurable paths - override with CLI flags
data_root="/var/lib"
bin_dir="/usr/local/bin"
retention_months=6
local_tarballs_dir=""
scrape_targets=()

# State file - records chosen paths for future upgrades
state_file="/etc/p4prometheus-monitoring/install.env"

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

    install_prom_graf.sh [-d <data_root>] [-b <bin_dir>] [-r <months>]
                         [-target <host:port>] [--local-tarballs-dir <path>]
                         [-push]

or

    install_prom_graf.sh -h

  -d <data_root>           Base directory for all runtime data.
                           Default: /var/lib
                           Example: -d /data  (puts all data under /data/)
  -b <bin_dir>             Directory for installed binaries.
                           Default: /usr/local/bin
  -r <months>              Metrics retention period in months.
                           Default: 6
  -target <host:port>      Prometheus scrape target. Repeatable.
                           Example: -target myserver:9100 -target otherserver:9100
                           If not specified, a placeholder is written to prometheus.yml.
  --local-tarballs-dir <p> Directory containing pre-staged release tarballs.
                           Skips all downloads - for air-gapped environments.
                           Files must match GitHub release asset names exactly.
  -push                    Install Pushgateway (not installed by default).

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
        (-d) data_root=$2; shiftArgs=1;;
        (-b) bin_dir=$2; shiftArgs=1;;
        (-r) retention_months=$2; shiftArgs=1;;
        (-target) scrape_targets+=("$2"); shiftArgs=1;;
        (--local-tarballs-dir) local_tarballs_dir=$2; shiftArgs=1;;
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

    if [[ -n "$local_tarballs_dir" ]]; then
        local local_file="${local_tarballs_dir}/${fname}"
        if [[ ! -f "$local_file" ]]; then
            bail "Air-gap mode: expected tarball not found: $local_file"
        fi
        msg "Using local tarball: $local_file"
        cp "$local_file" "$fname"
    else
        if [[ -f "$fname" ]]; then
            msg "File $fname already exists, removing..."
            rm -f "$fname"
        fi
        msg "Downloading $url"
        if ! wget -q --show-progress "$url"; then
            bail "Failed to download $url"
        fi
    fi

    msg "Extracting $fname"
    tar zxf "$fname" || bail "Failed to untar $fname"
}

write_state_file () {
    mkdir -p "$(dirname "$state_file")"
    cat << EOF > "$state_file"
# p4prometheus monitoring stack - install state
# Written by install_prom_graf.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ)
# This file is read by update_prom_graf.sh to preserve install choices across upgrades.
# CLI flags always override these values.
DATA_ROOT=${data_root}
BIN_DIR=${bin_dir}
RETENTION_MONTHS=${retention_months}
VER_NODE_EXPORTER=${VER_NODE_EXPORTER}
VER_PROMETHEUS=${VER_PROMETHEUS}
VER_ALERTMANAGER=${VER_ALERTMANAGER}
VER_VICTORIA_METRICS=${VER_VICTORIA_METRICS}
VER_PUSHGATEWAY=${VER_PUSHGATEWAY}
EOF
    chmod 644 "$state_file"
    msg "Install state written to: $state_file"
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
            isubuntu=1
            ;;
        centos|rhel|rocky|almalinux|fedora)
            isubuntu=0
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

    if [[ $isubuntu -eq 1 ]]; then
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

    # Redirect Grafana data directory if a custom data_root was requested
    if [[ "$data_root" != "/var/lib" ]]; then
        local grafana_data_dir="${data_root}/grafana"
        mkdir -p "$grafana_data_dir"
        chown grafana:grafana "$grafana_data_dir" 2>/dev/null || true
        local grafana_ini="/etc/grafana/grafana.ini"
        if [[ -f "$grafana_ini" ]]; then
            # Set paths.data if not already customized
            if grep -q '^\s*;*\s*data\s*=' "$grafana_ini"; then
                sed -i "s|^\s*;*\s*data\s*=.*|data = ${grafana_data_dir}|" "$grafana_ini"
            else
                # Inject under [paths] section
                sed -i "/^\[paths\]/a data = ${grafana_data_dir}" "$grafana_ini"
            fi
            msg "Grafana data directory set to: $grafana_data_dir"
        else
            msg "Warning: /etc/grafana/grafana.ini not found - Grafana data dir not redirected"
        fi
    fi

    systemctl daemon-reload
    systemctl enable grafana-server
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
    mkdir -p "${data_root}/alertmanager"
    chown "$userid:$userid" /etc/alertmanager
    chown "$userid:$userid" "${data_root}/alertmanager"

    cd /tmp || bail "failed to cd"
    PVER="$VER_ALERTMANAGER"
    fname="alertmanager-$PVER.linux-${arch}.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/alertmanager/releases/download/v$PVER/$fname"
    TEMP_FILES+=("alertmanager-files")

    mv alertmanager-$PVER.linux-${arch} alertmanager-files

    for base_file in alertmanager amtool; do
        bin_file="${bin_dir}/$base_file"
        cp alertmanager-files/$base_file "${bin_dir}/"
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
ExecStart=${bin_dir}/alertmanager --config.file=/etc/alertmanager/alertmanager.yml \
    --storage.path=${data_root}/alertmanager --log.level=debug

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

    mv "node_exporter-$PVER.linux-${arch}/node_exporter" "${bin_dir}/"
    chown "$userid:$userid" "${bin_dir}/node_exporter"
    chmod 755 "${bin_dir}/node_exporter"

    if [[ $SELinuxEnabled -eq 1 ]]; then
        local bin_file="${bin_dir}/node_exporter"
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
ExecStart=${bin_dir}/node_exporter --collector.systemd \
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
            local bin_file="${bin_dir}/$base_file"
            mv "$base_file" "${bin_dir}/"
            chown "$userid:$userid" "$bin_file"
            chmod 755 "$bin_file"
            if [[ $SELinuxEnabled -eq 1 ]]; then
                semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
                restorecon -vF "$bin_file"
            fi
        fi
    done

    mkdir -p "${data_root}/victoria-metrics"
    chown -R "$userid:$userid" "${data_root}/victoria-metrics"

    cat << EOF > /etc/systemd/system/victoria-metrics.service
[Unit]
Description=Victoria Metrics
Wants=network-online.target
After=network-online.target

[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=${bin_dir}/victoria-metrics-prod \
    -storageDataPath ${data_root}/victoria-metrics/ \
    -retentionPeriod=${retention_months}

[Install]
WantedBy=multi-user.target
EOF

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
    mkdir -p "${data_root}/prometheus"
    chown "$userid:$userid" /etc/prometheus
    chown "$userid:$userid" "${data_root}/prometheus"

    cd /tmp || bail "failed to cd"
    PVER="$VER_PROMETHEUS"
    fname="prometheus-$PVER.linux-${arch}.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/prometheus/releases/download/v$PVER/prometheus-$PVER.linux-${arch}.tar.gz"
    TEMP_FILES+=("prometheus-files")

    mv prometheus-$PVER.linux-${arch} prometheus-files

    for base_file in prometheus promtool; do
        local bin_file="${bin_dir}/$base_file"
        cp "prometheus-files/$base_file" "${bin_dir}/"
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
ExecStart=${bin_dir}/prometheus \
 --config.file /etc/prometheus/prometheus.yml \
 --storage.tsdb.path ${data_root}/prometheus/ \
 --storage.tsdb.retention.time=$(( retention_months * 30 ))d \
 --web.console.templates=/etc/prometheus/consoles \
 --web.console.libraries=/etc/prometheus/console_libraries

[Install]
WantedBy=multi-user.target
EOF

    # Build the node_exporter targets block
    local targets_block=""
    if [[ ${#scrape_targets[@]} -gt 0 ]]; then
        targets_block="    - targets:"$'\n'
        for t in "${scrape_targets[@]}"; do
            targets_block+="        - ${t}"$'\n'
        done
    else
        targets_block="    # ==========================================================\n"
        targets_block+="    # CONFIGURE THESE VALUES AS APPROPRIATE FOR YOUR SERVERS!\n"
        targets_block+="    # Note: names appear as labels in metrics - avoid raw IPs.\n"
        targets_block+="    # Default port for node_exporter is 9100.\n"
        targets_block+="    # Re-run with -target flags to populate this automatically.\n"
        targets_block+="    # ==========================================================\n"
        targets_block+="    - targets:\n"
        targets_block+="        - localhost:9100\n"
        targets_block+="        - my_p4_server:9100\n"
    fi

    cat << EOF > /etc/prometheus/prometheus.yml
global:
  scrape_interval:     15s
  evaluation_interval: 15s

# Alertmanager configuration - optional
alerting:
  alertmanagers:
  - static_configs:
    - targets:
        - localhost:9093

# Alert rules - see https://github.com/perforce/p4prometheus for perforce_rules.yml
# rule_files:
#   - "perforce_rules.yml"
#   - "perforce_rules_local.yml"  # local customizations - never overwritten by updates

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
    - targets: ['localhost:9090']

  - job_name: 'node_exporter'
    static_configs:
$(echo -e "$targets_block")
  # ==========================================================
  # This section SHOULD BE DELETED if pushgateway not in use
  # ==========================================================
  - job_name: 'pushgateway'
    honor_labels: true
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

    mv "pushgateway-$PVER.linux-${arch}/pushgateway" "${bin_dir}/"
    chown "$userid:$userid" "${bin_dir}/pushgateway"
    chmod 755 "${bin_dir}/pushgateway"

    if [[ $SELinuxEnabled -eq 1 ]]; then
        local bin_file="${bin_dir}/pushgateway"
        semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
        restorecon -vF "$bin_file"
    fi

    mkdir -p "${data_root}/pushgateway"
    chown "$userid:$userid" "${data_root}/pushgateway"

    cat << EOF > /etc/systemd/system/pushgateway.service
[Unit]
Description=Prometheus Pushgateway
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=${bin_dir}/pushgateway \
 --web.listen-address=:9091 \
 --web.telemetry-path=/metrics \
 --persistence.file=${data_root}/pushgateway/metric.store \
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

check_health () {
    msg ""
    msg "Running health checks..."
    local all_ok=1

    # Service status checks
    local services=(node_exporter alertmanager victoria-metrics prometheus grafana-server)
    [[ $InstallPushgateway -eq 1 ]] && services+=(pushgateway)

    for service in "${services[@]}"; do
        if systemctl is-active --quiet "$service" 2>/dev/null; then
            msg "  ✓ $service is running"
        else
            msg "  ✗ $service is NOT running - check logs with: journalctl -u $service"
            all_ok=0
        fi
    done

    # HTTP endpoint checks
    msg ""
    msg "Checking HTTP endpoints (allow a few seconds for startup)..."
    sleep 3
    local endpoints=(
        "Prometheus:localhost:9090/-/healthy"
        "VictoriaMetrics:localhost:8428/health"
        "Alertmanager:localhost:9093/-/healthy"
        "Grafana:localhost:3000/api/health"
        "NodeExporter:localhost:9100/metrics"
    )
    [[ $InstallPushgateway -eq 1 ]] && endpoints+=("Pushgateway:localhost:9091/-/healthy")

    for entry in "${endpoints[@]}"; do
        local name="${entry%%:*}"
        local url="http://${entry#*:}"
        if curl -sf --max-time 5 "$url" > /dev/null 2>&1; then
            msg "  ✓ $name responding at $url"
        else
            msg "  ✗ $name not responding at $url"
            all_ok=0
        fi
    done

    if [[ $all_ok -eq 1 ]]; then
        msg ""
        msg "All health checks passed."
    else
        msg ""
        msg "One or more health checks failed. Review the output above."
    fi
}

msg "Starting installation process..."
msg "Architecture: $arch"
msg "Data root:    $data_root"
msg "Bin dir:      $bin_dir"
msg "Retention:    ${retention_months} months"
[[ -n "$local_tarballs_dir" ]] && msg "Air-gap mode: using tarballs from $local_tarballs_dir"

check_os

msg "Installing components..."
install_node_exporter
install_alertmanager
install_victoria_metrics
install_prometheus
[[ $InstallPushgateway -eq 1 ]] && install_pushgateway
install_grafana

write_state_file

check_health

echo "
======================================================================
Installation complete.

Data directories:
  Prometheus:        ${data_root}/prometheus/
  VictoriaMetrics:   ${data_root}/victoria-metrics/
  Alertmanager:      ${data_root}/alertmanager/
  Grafana:           ${data_root}/grafana/  (if -d was specified)
  Pushgateway:       ${data_root}/pushgateway/  (if installed)

Config files to review:
  /etc/prometheus/prometheus.yml     (scrape targets, rule files)
  /etc/alertmanager/alertmanager.yml (email/slack notifications)

$(if [[ ${#scrape_targets[@]} -eq 0 ]]; then echo "  ⚠  No -target flags were specified.
  Edit /etc/prometheus/prometheus.yml to set your actual scrape targets:
    cd /etc/prometheus && vi prometheus.yml && make && make restart
"; fi)
Ports that may need to be opened in your firewall:
  9090  Prometheus UI
  9093  Alertmanager UI
  8428  VictoriaMetrics
  9100  Node Exporter (metrics)
  3000  Grafana UI
$(if [[ $InstallPushgateway -eq 1 ]]; then echo "  9091  Pushgateway"; fi)

Install state saved to: $state_file
  (Future upgrades via update_prom_graf.sh will use these paths automatically)
======================================================================"

