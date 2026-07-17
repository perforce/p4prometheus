#!/bin/bash
# Installs the following: node_exporter, prometheus, victoriametrics, grafana and alertmanager
# This is the monitoring machine
#
# New in this version:
#   -d <data_root>            Set base directory for all runtime data (default: /var/lib)
#   -b <bin_dir>              Set directory for installed binaries (default: /usr/local/bin)
#   -r <months>               Set metrics retention period in months (default: 6)
#   -target <host:port>       Add a Prometheus scrape target (repeatable)
#   -pint                     Install Pint (Prometheus linter) and create systemd service
#   -grafana-setup            Provision Grafana datasource and import recommended dashboards
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
VER_PINT="0.87.0"

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
            if [[ -f "$f" ]]; then
                rm -f "$f"
            fi
            if [[ -d "$f" ]]; then
                rm -rf "$f"
            fi
        done
    fi
    return 0
}

trap cleanup EXIT

# ============================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON_LIB="${SCRIPT_DIR}/p4prom_common.sh"
if [[ ! -f "$COMMON_LIB" ]]; then
    COMMON_LIB_URL="https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/p4prom_common.sh"
    echo "Common library missing: $COMMON_LIB"
    echo "Attempting download from $COMMON_LIB_URL"
    if command -v wget >/dev/null 2>&1; then
        wget -q -O "$COMMON_LIB" "$COMMON_LIB_URL" || {
            echo "Error: Failed to download common library with wget"
            exit 1
        }
    elif command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$COMMON_LIB" "$COMMON_LIB_URL" || {
            echo "Error: Failed to download common library with curl"
            exit 1
        }
    else
        echo "Error: Missing common library and neither wget nor curl is available"
        exit 1
    fi
fi
# shellcheck source=p4prom_common.sh
source "$COMMON_LIB" || { echo "Error: Failed to source common library $COMMON_LIB"; exit 1; }

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
                         [-target <host:port>] [-grafana-setup]
                         [--local-tarballs-dir <path>]
                         [-push] [-pint]

or

    install_prom_graf.sh -h

  -d <data_root>            Base directory for all runtime data.
                            Default: /var/lib
                            Example: -d /data  (puts all data under /data/)
  -b <bin_dir>              Directory for installed binaries.
                            Default: /usr/local/bin
  -r <months>               Metrics retention period in months (for use with VictoriaMetrics).
                            Not used by Prometheus itself, which has a short retention setting.
                            Default: 6
  -target <host:port>       Prometheus scrape target. Repeatable.
                            Example: -target myserver:9100 -target otherserver:9100
                            If not specified, a placeholder is written to prometheus.yml.
  -grafana-setup            Create Grafana datasource for Victoria Metrics and
                            import recommended dashboards from INSTALL.md.
  --local-tarballs-dir <p>  Directory containing pre-staged release tarballs.
                            Skips all downloads - for air-gapped environments.
                            Files must match GitHub release asset names exactly.
  -push                     Install Pushgateway (not installed by default).
                            This is generally deprecated in favor of using VMAgent remotely.
    -pint                     Install Pint linter and create pint.service
                                                        for continuous validation of /etc/prometheus/perforce_rules.yml.

Example:

    sudo ./install_prom_graf.sh -d /data -b /opt/bin -r 12 -target myserver:9100 -grafana-setup
    sudo ./install_prom_graf.sh -grafana-setup
    sudo ./install_prom_graf.sh -d /data -target myp4:9100 -target myreplica:9100 -grafana-setup
    sudo ./install_prom_graf.sh -pint
"
}

# Command Line Processing

declare -i shiftArgs=0
declare -i InstallPushgateway=0
declare -i InstallPint=0
declare -i SetupGrafanaProvisioning=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 1;;
        (-push) InstallPushgateway=1;;
        (-pint) InstallPint=1;;
        (-grafana-setup) SetupGrafanaProvisioning=1;;
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

setup_grafana_datasource_and_dashboards () {
    msg "Setting up Grafana datasource and dashboards..."

    local ds_dir="/etc/grafana/provisioning/datasources"
    local dash_provider_dir="/etc/grafana/provisioning/dashboards"
    local dash_json_dir="/var/lib/grafana/dashboards/p4prometheus"
    local ds_file="${ds_dir}/p4prometheus-victoria-metrics.yaml"
    local provider_file="${dash_provider_dir}/p4prometheus-dashboards.yaml"

    mkdir -p "$ds_dir" "$dash_provider_dir" "$dash_json_dir"

    cat << EOF > "$ds_file"
apiVersion: 1

datasources:
  - name: Victoria Metrics
    type: prometheus
    access: proxy
    url: http://localhost:8428
    isDefault: true
    editable: true
EOF

    cat << EOF > "$provider_file"
apiVersion: 1

providers:
  - name: p4prometheus-dashboards
    orgId: 1
    folder: P4Prometheus
    type: file
    disableDeletion: false
    updateIntervalSeconds: 60
    allowUiUpdates: true
    options:
      path: ${dash_json_dir}
EOF

    local ids=(12278 15509 405 1860)
    local id=""
    for id in "${ids[@]}"; do
        local dash_url="https://grafana.com/api/dashboards/${id}/revisions/latest/download"
        local dash_file="${dash_json_dir}/${id}.json"
        if curl -fsSL "$dash_url" -o "$dash_file"; then
            msg "  Downloaded dashboard ${id}"
        else
            msg "  Warning: failed to download dashboard ${id} from ${dash_url}"
            rm -f "$dash_file"
        fi
    done

    chown -R grafana:grafana "$dash_json_dir" 2>/dev/null || true
    chmod 644 "$ds_file" "$provider_file"
    find "$dash_json_dir" -type f -name '*.json' -exec chmod 644 {} \;

    systemctl daemon-reload
    systemctl restart grafana-server
    systemctl status grafana-server --no-pager

    msg "Grafana provisioning complete: datasource 'Victoria Metrics' and dashboards imported."
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
    mkdir -p /etc/alertmanager/templates
    mkdir -p "${data_root}/alertmanager"
    chown "$userid:$userid" /etc/alertmanager
    chown "$userid:$userid" /etc/alertmanager/templates
    chown "$userid:$userid" "${data_root}/alertmanager"

    local perforce_alert_tmpl_file="/etc/alertmanager/templates/perforce.tmpl"
    local perforce_alert_tmpl_url="https://raw.githubusercontent.com/perforce/p4prometheus/master/examples/alertmanager/templates/perforce.tmpl"
    msg "Downloading default Alertmanager template to ${perforce_alert_tmpl_file}"
    if ! wget -q -O "$perforce_alert_tmpl_file" "$perforce_alert_tmpl_url"; then
        bail "Failed to download Alertmanager template from $perforce_alert_tmpl_url"
    fi
    chown "$userid:$userid" "$perforce_alert_tmpl_file"
    chmod 644 "$perforce_alert_tmpl_file"

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
        apply_bin_selinux_context "$bin_file"
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
  # Configure these according to alertmanager docs
  smtp_from: alertmanager@example.com
  smtp_smarthost: localhost:25
  smtp_require_tls: false
  # Hello is the local machine name
  smtp_hello: localhost
  #smtp_hello: localhost
  # slack_api_url: 'https://hooks.slack.com/services/XXX/XXX/XXX'

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

# - name: Slack
#   slack_configs:
#   - channel: '#p4d-alerts'
#     # Set to false for now - to avoid unnecessary posts
#     send_resolved: false

templates:
  - templates/*.tmpl
EOF

    echo -e "
# Makefile for alertmanager - default rule is validate
validate:
\\tamtool check-config alertmanager.yml

restart: validate
\\tsystemctl restart alertmanager
" >  /etc/alertmanager/Makefile

    chown $userid:$userid /etc/alertmanager/alertmanager.yml /etc/alertmanager/Makefile

    systemd_enable_and_restart /etc/systemd/system/alertmanager.service alertmanager

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

    apply_bin_selinux_context "${bin_dir}/node_exporter"

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

    systemd_enable_and_restart /etc/systemd/system/node_exporter.service node_exporter
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
            apply_bin_selinux_context "$bin_file"
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

    systemd_enable_and_restart /etc/systemd/system/victoria-metrics.service victoria-metrics
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
        apply_bin_selinux_context "$bin_file"
    done

    cp -r prometheus-files/consoles /etc/prometheus
    cp -r prometheus-files/console_libraries /etc/prometheus
    chown -R "$userid:$userid" /etc/prometheus/consoles
    chown -R "$userid:$userid" /etc/prometheus/console_libraries

    local perforce_rules_file="/etc/prometheus/perforce_rules.yml"
    local perforce_rules_url="https://raw.githubusercontent.com/perforce/p4prometheus/master/examples/prometheus/perforce_rules.yml"
    msg "Downloading default Perforce alert rules to ${perforce_rules_file}"
    if ! wget -q -O "$perforce_rules_file" "$perforce_rules_url"; then
        bail "Failed to download Perforce alert rules from $perforce_rules_url"
    fi
    chown "$userid:$userid" "$perforce_rules_file"
    chmod 644 "$perforce_rules_file"

    # Note that we don't retain much data in Prometheus itself - we use VictoriaMetrics for long-term storage.
    # So only 7 days
    prometheus_retention="7d"
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
 --storage.tsdb.retention.time=$prometheus_retention \
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

        local pushgateway_scrape_block=""
        if [[ $InstallPushgateway -eq 1 ]]; then
                pushgateway_scrape_block=$(cat <<'EOF'
    - job_name: 'pushgateway'
        honor_labels: true
        static_configs:
            - targets:
                    - localhost:9091
EOF
)
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

# Alert rules - default Perforce rules are downloaded by this installer
rule_files:
    - "/etc/prometheus/perforce_rules.yml"

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
    - targets: ['localhost:9090']

  - job_name: 'node_exporter'
    static_configs:
$(echo -e "$targets_block")

$(echo -e "$pushgateway_scrape_block")

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

    systemd_enable_and_restart /etc/systemd/system/prometheus.service prometheus
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

    apply_bin_selinux_context "${bin_dir}/pushgateway"

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

    systemd_enable_and_restart /etc/systemd/system/pushgateway.service pushgateway
}

install_pint () {
    msg "Installing Pint..."

    if check_service_exists pint && systemctl is-active --quiet pint; then
            msg "Stopping existing pint service..."
            systemctl stop pint
    fi

    local userid="prometheus"
    local pint_arch="$arch"
    local pint_file="pint-linux-${pint_arch}"
    local pint_url="https://github.com/rcowham/pint/releases/download/v${VER_PINT}/${pint_file}"
    local pint_bin="${bin_dir}/pint"
    local pint_cfg="/etc/prometheus/pint_vm.hcl"

    cd /tmp || bail "failed to cd"
    msg "Downloading ${pint_url}"
    if ! wget -q -O "$pint_file" "$pint_url"; then
            bail "Failed to download $pint_url"
    fi
    TEMP_FILES+=("$pint_file")

    mv "$pint_file" "$pint_bin"
    chown "$userid:$userid" "$pint_bin"
    chmod 755 "$pint_bin"

        apply_bin_selinux_context "$pint_bin"

    cat << EOF > "$pint_cfg"
# Point pint at VictoriaMetrics
prometheus "monitor" {
    uri     = "http://localhost:8428"
    timeout = "60s"
}

# Enable VictoriaMetrics mode to report syntax errors as warnings
# instead of fatal errors when MetricsQL extensions are used.
parser {
    victoria_metrics = true
}

# Disable smelly selectors warning in promql/regexp check.
check "promql/regexp" {
    smelly = false
}

# Disable checks that don't work with VictoriaMetrics.
checks {
    disabled = ["promql/rate", "promql/range_query"]
}
EOF
        chown "$userid:$userid" "$pint_cfg"
        chmod 644 "$pint_cfg"

        cat << EOF > /etc/systemd/system/pint.service
[Unit]
Description=Prometheus Pint validation tool
Wants=network-online.target
After=network-online.target

[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=${bin_dir}/pint watch glob /etc/prometheus/perforce_rules.yml --config ${pint_cfg}

[Install]
WantedBy=multi-user.target
EOF

        systemd_enable_and_restart /etc/systemd/system/pint.service pint
}

check_health () {
    msg ""
    msg "Running health checks..."
    local all_ok=1

    # Service status checks
    local services=(node_exporter alertmanager victoria-metrics prometheus grafana-server)
    [[ $InstallPushgateway -eq 1 ]] && services+=(pushgateway)
    [[ $InstallPint -eq 1 ]] && services+=(pint)

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
[[ $InstallPint -eq 1 ]] && install_pint
install_grafana
[[ $SetupGrafanaProvisioning -eq 1 ]] && setup_grafana_datasource_and_dashboards

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
    Pint config:       /etc/prometheus/pint_vm.hcl  (if -pint)

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

