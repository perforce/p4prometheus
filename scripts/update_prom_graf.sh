#!/bin/bash
# Updates the monitoring server stack: node_exporter, prometheus, alertmanager,
# victoria-metrics, grafana, and optionally pushgateway/vmagent.
#
# Reads install state from /etc/p4prometheus-monitoring/install.env (written by
# install_prom_graf.sh) so previously-chosen paths are preserved automatically.
#
# Also implements the split-file approach for perforce_rules.yml (issue #117):
#   perforce_rules.yml         - upstream file, always updated (do not edit)
#   perforce_rules_local.yml   - your customizations, never overwritten
# Both are referenced from prometheus.yml rule_files section.

set -e
set -o pipefail

# shellcheck disable=SC2128
if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4"; exit 1;
fi

# ============================================================
# Configuration section - update versions here when new releases are available

VER_NODE_EXPORTER="1.8.2"
VER_PROMETHEUS="2.54.1"
VER_ALERTMANAGER="0.27.0"
VER_PUSHGATEWAY="1.9.0"
VER_VICTORIA_METRICS="1.105.0"

# Configurable paths - overridden by state file or CLI flags
data_root="/var/lib"
bin_dir="/usr/local/bin"
retention_months=6
local_tarballs_dir=""

state_file="/etc/p4prometheus-monitoring/install.env"

# Default to amd but allow arm architecture
arch="amd64"
[[ $(uname -p) == 'aarch64' ]] && arch="arm64"

# Track whether CLI explicitly set a flag (to avoid state file clobbering it)
declare -i cli_data_root_set=0
declare -i cli_bin_dir_set=0
declare -i cli_retention_set=0

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON_LIB="${SCRIPT_DIR}/p4prom_common.sh"
if [[ ! -f "$COMMON_LIB" ]]; then
    COMMON_LIB_URL="https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/p4prom_common.sh"
    echo "Common library missing: $COMMON_LIB"
    echo "Attempting download from $COMMON_LIB_URL"
    if command -v wget >/dev/null 2>&1; then
        wget -q -O "$COMMON_LIB" "$COMMON_LIB_URL" || { echo "Error: Failed to download common library"; exit 1; }
    elif command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$COMMON_LIB" "$COMMON_LIB_URL" || { echo "Error: Failed to download common library"; exit 1; }
    else
        echo "Error: Missing common library and neither wget nor curl is available"; exit 1
    fi
fi
# shellcheck source=p4prom_common.sh
source "$COMMON_LIB" || { echo "Error: Failed to source common library $COMMON_LIB"; exit 1; }

# ============================================================

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

   echo "USAGE for update_prom_graf.sh:

    update_prom_graf.sh [-d <data_root>] [-b <bin_dir>] [-r <months>]
                        [--local-tarballs-dir <path>] [-push]

or

    update_prom_graf.sh -h

  -d <data_root>           Base directory for runtime data (prometheus, alertmanager, etc.)
                           Loaded automatically from state file if not specified.
  -b <bin_dir>             Binary installation directory.
                           Loaded automatically from state file if not specified.
  -r <months>              Metrics retention period in months for VictoriaMetrics/Prometheus.
                           Loaded automatically from state file if not specified.
  --local-tarballs-dir <p> Directory of pre-staged release tarballs (air-gap installs).
                           Skips all downloads; file names must match GitHub release assets.
  -push                    Update Pushgateway if installed (not updated by default).

Note: If /etc/p4prometheus-monitoring/install.env exists from a prior
  install_prom_graf.sh run, all paths and settings are loaded from it automatically.

"
}

# Command Line Processing

declare -i shiftArgs=0
declare -i UpdatePushgateway=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 1;;
        (-d) data_root=$2; cli_data_root_set=1; shiftArgs=1;;
        (-b) bin_dir=$2; cli_bin_dir_set=1; shiftArgs=1;;
        (-r) retention_months=$2; cli_retention_set=1; shiftArgs=1;;
        (--local-tarballs-dir) local_tarballs_dir=$2; shiftArgs=1;;
        (-push) UpdatePushgateway=1;;
        (-*) usage -h "Unknown command line option ($1)." && exit 1;;
    esac
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

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi
declare -i SELinuxEnabled=${SELinuxEnabled:-0}

# ============================================================
# Load state file - restore previously-chosen paths
# CLI flags already set above take priority over state file values

if [[ -f "$state_file" ]]; then
    msg "Loading install state from: $state_file"
    saved_data_root=$(grep '^DATA_ROOT=' "$state_file" 2>/dev/null | cut -d= -f2)
    saved_bin_dir=$(grep '^BIN_DIR=' "$state_file" 2>/dev/null | cut -d= -f2)
    saved_retention=$(grep '^RETENTION_MONTHS=' "$state_file" 2>/dev/null | cut -d= -f2)
    [[ $cli_data_root_set -eq 0 && -n "$saved_data_root" ]] && data_root="$saved_data_root"
    [[ $cli_bin_dir_set -eq 0 && -n "$saved_bin_dir" ]] && bin_dir="$saved_bin_dir"
    [[ $cli_retention_set -eq 0 && -n "$saved_retention" ]] && retention_months="$saved_retention"
    msg "  data_root=$data_root  bin_dir=$bin_dir  retention=${retention_months}m"
else
    msg "No state file found at $state_file - using defaults or CLI flags"
    msg "  (Run install_prom_graf.sh first, or specify -d / -b / -r flags)"
fi

# ============================================================
# OS detection (needed for Grafana package manager choice)

check_os () {
    if [[ -f /etc/os-release ]]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        OS_ID="$ID"
        msg "Detected OS: $NAME ${VERSION_ID:-}"
    else
        if grep -q ubuntu /proc/version 2>/dev/null; then OS_ID="ubuntu"
        elif grep -q centos /proc/version 2>/dev/null; then OS_ID="centos"
        elif grep -q redhat /proc/version 2>/dev/null; then OS_ID="rhel"
        else bail "Unable to detect operating system"
        fi
    fi
    case "$OS_ID" in
        ubuntu|debian) isubuntu=0 ;;
        centos|rhel|rocky|almalinux|fedora) isubuntu=1 ;;
        *) bail "Unsupported OS: $OS_ID" ;;
    esac
}

# ============================================================
# Helper: get current installed version of a binary

get_binary_version() {
    local binary=$1
    local pattern=$2   # awk pattern to extract version field
    "$binary" --version 2>&1 | awk "$pattern" | head -1
}

# ============================================================
# Update functions

update_node_exporter() {
    local progname="node_exporter"
    local userid="node_exporter"
    local service_name="node_exporter"
    local service_file="/etc/systemd/system/${service_name}.service"

    msg "Checking Node Exporter..."
    if ! grep -q "^$userid:" /etc/passwd; then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    local curr_ver
    curr_ver=$(${bin_dir}/${progname} --version 2>&1 | awk '/version/{print $3}' | head -1)
    if [[ "$curr_ver" == "$VER_NODE_EXPORTER" ]]; then
        msg "  node_exporter $curr_ver is up-to-date"
    else
        msg "  Updating node_exporter: $curr_ver → $VER_NODE_EXPORTER"
        if check_service_exists "$service_name" && systemctl is-active --quiet "$service_name"; then
            systemctl stop "$service_name"
        fi
        cd /tmp || bail "Failed to cd to /tmp"
        local fname="node_exporter-${VER_NODE_EXPORTER}.linux-${arch}.tar.gz"
        download_and_untar "$fname" \
            "https://github.com/prometheus/node_exporter/releases/download/v${VER_NODE_EXPORTER}/$fname"
        mv "node_exporter-${VER_NODE_EXPORTER}.linux-${arch}/node_exporter" "${bin_dir}/"
        chown "$userid:$userid" "${bin_dir}/node_exporter"
        chmod 755 "${bin_dir}/node_exporter"
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t "${bin_dir}/node_exporter" 2>/dev/null || true
            restorecon -vF "${bin_dir}/node_exporter"
        fi
    fi

    # Always refresh service file in case data paths changed
    cat << EOF > "$service_file"
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
    chmod 644 "$service_file"
    systemctl daemon-reload
    systemctl enable "$service_name"
    systemctl is-active --quiet "$service_name" || systemctl start "$service_name"
}

update_prometheus() {
    local progname="prometheus"
    local userid="prometheus"
    local service_name="prometheus"
    local service_file="/etc/systemd/system/${service_name}.service"

    msg "Checking Prometheus..."
    if ! grep -q "^$userid:" /etc/passwd; then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    local curr_ver
    curr_ver=$(${bin_dir}/prometheus --version 2>&1 | awk '/prometheus, version/{print $3}' | head -1)
    if [[ "$curr_ver" != "$VER_PROMETHEUS" ]]; then
        msg "  Updating prometheus: $curr_ver → $VER_PROMETHEUS"
        if check_service_exists "$service_name" && systemctl is-active --quiet "$service_name"; then
            systemctl stop "$service_name"
        fi
        cd /tmp || bail "Failed to cd to /tmp"
        local fname="prometheus-${VER_PROMETHEUS}.linux-${arch}.tar.gz"
        download_and_untar "$fname" \
            "https://github.com/prometheus/prometheus/releases/download/v${VER_PROMETHEUS}/$fname"
        local extract_dir="prometheus-${VER_PROMETHEUS}.linux-${arch}"
        for f in prometheus promtool; do
            cp "${extract_dir}/$f" "${bin_dir}/"
            chown "$userid:$userid" "${bin_dir}/$f"
            chmod 755 "${bin_dir}/$f"
            if [[ $SELinuxEnabled -eq 1 ]]; then
                semanage fcontext -a -t bin_t "${bin_dir}/$f" 2>/dev/null || true
                restorecon -vF "${bin_dir}/$f"
            fi
        done
        # Update console templates from new tarball
        cp -r "${extract_dir}/consoles" /etc/prometheus/
        cp -r "${extract_dir}/console_libraries" /etc/prometheus/
        chown -R "$userid:$userid" /etc/prometheus/consoles /etc/prometheus/console_libraries
    else
        msg "  prometheus $curr_ver is up-to-date"
    fi

    # Always refresh service file in case data paths or retention changed
    mkdir -p "${data_root}/prometheus"
    chown "$userid:$userid" "${data_root}/prometheus"
    cat << EOF > "$service_file"
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
    chmod 644 "$service_file"
    systemctl daemon-reload
    systemctl enable "$service_name"
    systemctl is-active --quiet "$service_name" && systemctl restart "$service_name" \
        || systemctl start "$service_name"
}

update_alertmanager() {
    local progname="alertmanager"
    local userid="alertmanager"
    local service_name="alertmanager"
    local service_file="/etc/systemd/system/${service_name}.service"

    msg "Checking Alertmanager..."
    if ! grep -q "^$userid:" /etc/passwd; then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    local curr_ver
    curr_ver=$(${bin_dir}/alertmanager --version 2>&1 | awk '/alertmanager, version/{print $3}' | head -1)
    if [[ "$curr_ver" != "$VER_ALERTMANAGER" ]]; then
        msg "  Updating alertmanager: $curr_ver → $VER_ALERTMANAGER"
        if check_service_exists "$service_name" && systemctl is-active --quiet "$service_name"; then
            systemctl stop "$service_name"
        fi
        cd /tmp || bail "Failed to cd to /tmp"
        local fname="alertmanager-${VER_ALERTMANAGER}.linux-${arch}.tar.gz"
        download_and_untar "$fname" \
            "https://github.com/prometheus/alertmanager/releases/download/v${VER_ALERTMANAGER}/$fname"
        local extract_dir="alertmanager-${VER_ALERTMANAGER}.linux-${arch}"
        for f in alertmanager amtool; do
            cp "${extract_dir}/$f" "${bin_dir}/"
            chown "$userid:$userid" "${bin_dir}/$f"
            chmod 755 "${bin_dir}/$f"
            if [[ $SELinuxEnabled -eq 1 ]]; then
                semanage fcontext -a -t bin_t "${bin_dir}/$f" 2>/dev/null || true
                restorecon -vF "${bin_dir}/$f"
            fi
        done
    else
        msg "  alertmanager $curr_ver is up-to-date"
    fi

    # Always refresh service file in case data paths changed
    mkdir -p "${data_root}/alertmanager"
    chown "$userid:$userid" "${data_root}/alertmanager"
    cat << EOF > "$service_file"
[Unit]
Description=Alertmanager
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=${bin_dir}/alertmanager --config.file=/etc/alertmanager/alertmanager.yml \
    --storage.path=${data_root}/alertmanager --log.level=info

[Install]
WantedBy=multi-user.target
EOF
    chmod 644 "$service_file"
    systemctl daemon-reload
    systemctl enable "$service_name"
    systemctl is-active --quiet "$service_name" && systemctl restart "$service_name" \
        || systemctl start "$service_name"
}

update_victoria_metrics() {
    local userid="prometheus"
    local service_name="victoria-metrics"
    local service_file="/etc/systemd/system/${service_name}.service"

    msg "Checking VictoriaMetrics..."
    if ! grep -q "^$userid:" /etc/passwd; then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    # VictoriaMetrics version output format varies; compare against state file version
    local saved_vm_ver
    saved_vm_ver=$(grep '^VER_VICTORIA_METRICS=' "$state_file" 2>/dev/null | cut -d= -f2)
    if [[ "$saved_vm_ver" == "$VER_VICTORIA_METRICS" ]]; then
        msg "  victoria-metrics $VER_VICTORIA_METRICS is up-to-date"
    else
        msg "  Updating victoria-metrics: ${saved_vm_ver:-unknown} → $VER_VICTORIA_METRICS"
        if check_service_exists "$service_name" && systemctl is-active --quiet "$service_name"; then
            systemctl stop "$service_name"
        fi
        cd /tmp || bail "Failed to cd to /tmp"
        local PVER="$VER_VICTORIA_METRICS"
        for fname in victoria-metrics-linux-${arch}-v$PVER.tar.gz vmutils-linux-${arch}-v$PVER.tar.gz; do
            download_and_untar "$fname" \
                "https://github.com/victoriametrics/victoriametrics/releases/download/v$PVER/$fname"
        done
        for base_file in victoria-metrics-prod vmagent-prod vmalert-prod vmauth-prod vmbackup-prod vmrestore-prod vmctl-prod; do
            if [[ -f "$base_file" ]]; then
                mv "$base_file" "${bin_dir}/"
                chown "$userid:$userid" "${bin_dir}/$base_file"
                chmod 755 "${bin_dir}/$base_file"
                if [[ $SELinuxEnabled -eq 1 ]]; then
                    semanage fcontext -a -t bin_t "${bin_dir}/$base_file" 2>/dev/null || true
                    restorecon -vF "${bin_dir}/$base_file"
                fi
            fi
        done
    fi

    # Always refresh service file in case data paths or retention changed
    mkdir -p "${data_root}/victoria-metrics"
    chown -R "$userid:$userid" "${data_root}/victoria-metrics"
    cat << EOF > "$service_file"
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
    chmod 644 "$service_file"
    systemctl daemon-reload
    systemctl enable "$service_name"
    systemctl is-active --quiet "$service_name" && systemctl restart "$service_name" \
        || systemctl start "$service_name"
}

update_grafana() {
    msg "Updating Grafana..."
    if ! check_service_exists grafana-server; then
        msg "  Grafana service not found - skipping (run install_prom_graf.sh first)"
        return
    fi

    if [[ $isubuntu -eq 0 ]]; then
        apt-get update -qq
        apt-get install -y --only-upgrade grafana
    else
        yum update -y grafana
    fi

    # Re-apply data dir setting if custom data_root is in use
    if [[ "$data_root" != "/var/lib" ]]; then
        local grafana_data_dir="${data_root}/grafana"
        local grafana_ini="/etc/grafana/grafana.ini"
        if [[ -f "$grafana_ini" ]]; then
            if grep -q '^\s*;*\s*data\s*=' "$grafana_ini"; then
                sed -i "s|^\s*;*\s*data\s*=.*|data = ${grafana_data_dir}|" "$grafana_ini"
            fi
        fi
    fi

    systemctl restart grafana-server
    systemctl status grafana-server --no-pager
}

update_pushgateway() {
    local progname="pushgateway"
    local userid="pushgateway"
    local service_name="pushgateway"
    local service_file="/etc/systemd/system/${service_name}.service"

    if ! check_service_exists "$service_name"; then
        msg "  Pushgateway not installed - skipping (use install_prom_graf.sh -push to install)"
        return
    fi

    msg "Checking Pushgateway..."
    if ! grep -q "^$userid:" /etc/passwd; then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    local curr_ver
    curr_ver=$(${bin_dir}/pushgateway --version 2>&1 | awk '/pushgateway, version/{print $3}' | head -1)
    if [[ "$curr_ver" != "$VER_PUSHGATEWAY" ]]; then
        msg "  Updating pushgateway: $curr_ver → $VER_PUSHGATEWAY"
        if systemctl is-active --quiet "$service_name"; then
            systemctl stop "$service_name"
        fi
        cd /tmp || bail "Failed to cd to /tmp"
        local fname="pushgateway-${VER_PUSHGATEWAY}.linux-${arch}.tar.gz"
        download_and_untar "$fname" \
            "https://github.com/prometheus/pushgateway/releases/download/v${VER_PUSHGATEWAY}/$fname"
        mv "pushgateway-${VER_PUSHGATEWAY}.linux-${arch}/pushgateway" "${bin_dir}/"
        chown "$userid:$userid" "${bin_dir}/pushgateway"
        chmod 755 "${bin_dir}/pushgateway"
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t "${bin_dir}/pushgateway" 2>/dev/null || true
            restorecon -vF "${bin_dir}/pushgateway"
        fi
    else
        msg "  pushgateway $curr_ver is up-to-date"
    fi

    # Refresh service file
    mkdir -p "${data_root}/pushgateway"
    chown "$userid:$userid" "${data_root}/pushgateway"
    cat << EOF > "$service_file"
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
    chmod 644 "$service_file"
    systemctl daemon-reload
    systemctl enable "$service_name"
    systemctl is-active --quiet "$service_name" && systemctl restart "$service_name" \
        || systemctl start "$service_name"
}

# ============================================================
# Issue #117: perforce_rules.yml split-file management
# perforce_rules.yml         = upstream (always overwritten on update)
# perforce_rules_local.yml   = customer customizations (never touched)
# Both referenced in prometheus.yml rule_files section.

update_perforce_rules() {
    local rules_dir="/etc/prometheus"
    local upstream_file="${rules_dir}/perforce_rules.yml"
    local local_file="${rules_dir}/perforce_rules_local.yml"
    local tmp_upstream="/tmp/perforce_rules_upstream.yml"
    local prometheus_userid="prometheus"

    msg "Checking perforce_rules.yml (issue #117 split-file management)..."

    # Download latest upstream rules
    local rules_url="https://raw.githubusercontent.com/perforce/p4prometheus/master/examples/prometheus/perforce_rules.yml"
    if [[ -n "$local_tarballs_dir" ]]; then
        local local_rules="${local_tarballs_dir}/perforce_rules.yml"
        if [[ ! -f "$local_rules" ]]; then
            msg "  Air-gap mode: perforce_rules.yml not found at $local_rules - skipping"
            return
        fi
        cp "$local_rules" "$tmp_upstream"
    else
        if ! wget -q -O "$tmp_upstream" "$rules_url"; then
            msg "  Warning: Could not download perforce_rules.yml - skipping rule update"
            return
        fi
    fi

    # Load last known upstream checksum from state file
    local last_upstream_checksum
    last_upstream_checksum=$(grep '^PERFORCE_RULES_UPSTREAM_CHECKSUM=' "$state_file" 2>/dev/null | cut -d= -f2)
    local new_upstream_checksum
    new_upstream_checksum=$(sha256sum "$tmp_upstream" | awk '{print $1}')

    if [[ "$new_upstream_checksum" == "$last_upstream_checksum" && -f "$upstream_file" ]]; then
        msg "  perforce_rules.yml is up-to-date (checksum unchanged)"
        rm -f "$tmp_upstream"
    else
        # Check if local copy has been customized since last update
        local local_checksum=""
        [[ -f "$upstream_file" ]] && local_checksum=$(sha256sum "$upstream_file" | awk '{print $1}')

        if [[ -f "$upstream_file" && -n "$last_upstream_checksum" && \
              "$local_checksum" != "$last_upstream_checksum" ]]; then
            # Local file differs from last upstream - customer has made changes
            local backup="${upstream_file}.$(date +%Y%m%d)"
            msg "  perforce_rules.yml has local modifications."
            msg "  Preserving local version as: $backup"
            cp "$upstream_file" "$backup"
            chown "$prometheus_userid:$prometheus_userid" "$backup" 2>/dev/null || true
            msg "  Installing new upstream perforce_rules.yml"
            msg "  Review $backup and merge any needed changes into $local_file"
        else
            msg "  Updating perforce_rules.yml (upstream changed, no local modifications)"
        fi

        cp "$tmp_upstream" "$upstream_file"
        chown "$prometheus_userid:$prometheus_userid" "$upstream_file"
        chmod 644 "$upstream_file"
        rm -f "$tmp_upstream"

        # Record new upstream checksum for next update
        # (Updated in write_state_file below via PERFORCE_RULES_UPSTREAM_CHECKSUM)
        PERFORCE_RULES_UPSTREAM_CHECKSUM="$new_upstream_checksum"
    fi

    # Create perforce_rules_local.yml if it doesn't exist (customer customization file)
    if [[ ! -f "$local_file" ]]; then
        cat << 'EOF' > "$local_file"
# perforce_rules_local.yml - local alert rule customizations
#
# This file is NEVER overwritten by update_prom_graf.sh.
# Add your custom alert rules here.
# Reference: https://prometheus.io/docs/prometheus/latest/configuration/alerting_rules/
#
# To activate: uncomment the rule_files section in /etc/prometheus/prometheus.yml
# and ensure both perforce_rules.yml and perforce_rules_local.yml are listed.
#
# Example:
# groups:
# - name: local.rules
#   rules:
#   - alert: MyCustomAlert
#     expr: some_metric > threshold
#     labels:
#       severity: warning
#     annotations:
#       summary: "Custom alert description"
EOF
        chown "$prometheus_userid:$prometheus_userid" "$local_file"
        chmod 644 "$local_file"
        msg "  Created $local_file (your customizations go here - never overwritten)"
    fi

    # Remind operator to enable rule_files in prometheus.yml if not already done
    if ! grep -qE '^\s*-\s+"?perforce_rules\.yml"?' /etc/prometheus/prometheus.yml 2>/dev/null; then
        msg ""
        msg "  *** ACTION REQUIRED: Enable alert rules in /etc/prometheus/prometheus.yml ***"
        msg "  Uncomment or add to the rule_files section:"
        msg "    rule_files:"
        msg "      - \"perforce_rules.yml\""
        msg "      - \"perforce_rules_local.yml\""
        msg "  Then run: cd /etc/prometheus && make restart"
    fi
}

# ============================================================
# Write updated state file

write_monitoring_state_file() {
    mkdir -p "$(dirname "$state_file")"
    cat << EOF > "$state_file"
# p4prometheus monitoring stack - install state
# Written by update_prom_graf.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ)
# This file is read by update_prom_graf.sh to preserve settings across upgrades.
# CLI flags always override these values.
DATA_ROOT=${data_root}
BIN_DIR=${bin_dir}
RETENTION_MONTHS=${retention_months}
VER_NODE_EXPORTER=${VER_NODE_EXPORTER}
VER_PROMETHEUS=${VER_PROMETHEUS}
VER_ALERTMANAGER=${VER_ALERTMANAGER}
VER_VICTORIA_METRICS=${VER_VICTORIA_METRICS}
VER_PUSHGATEWAY=${VER_PUSHGATEWAY}
PERFORCE_RULES_UPSTREAM_CHECKSUM=${PERFORCE_RULES_UPSTREAM_CHECKSUM:-}
EOF
    chmod 644 "$state_file"
    msg "Install state updated: $state_file"
}

# ============================================================
# Health checks

check_health() {
    msg ""
    msg "Running health checks..."
    local all_ok=1
    local services=(node_exporter alertmanager victoria-metrics prometheus grafana-server)
    check_service_exists pushgateway && services+=(pushgateway)

    for service in "${services[@]}"; do
        if systemctl is-active --quiet "$service" 2>/dev/null; then
            msg "  ✓ $service is running"
        else
            msg "  ✗ $service is NOT running - check: journalctl -u $service"
            all_ok=0
        fi
    done

    msg ""
    msg "Checking HTTP endpoints..."
    sleep 2
    local endpoints=(
        "Prometheus:localhost:9090/-/healthy"
        "VictoriaMetrics:localhost:8428/health"
        "Alertmanager:localhost:9093/-/healthy"
        "Grafana:localhost:3000/api/health"
        "NodeExporter:localhost:9100/metrics"
    )
    check_service_exists pushgateway && endpoints+=("Pushgateway:localhost:9091/-/healthy")

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

# ============================================================
# Main

msg "Starting update process..."
msg "Architecture: $arch"
msg "Data root:    $data_root"
msg "Bin dir:      $bin_dir"
msg "Retention:    ${retention_months} months"
[[ -n "$local_tarballs_dir" ]] && msg "Air-gap mode: using tarballs from $local_tarballs_dir"

check_os

update_node_exporter
update_prometheus
update_alertmanager
update_victoria_metrics
update_grafana
[[ $UpdatePushgateway -eq 1 ]] && update_pushgateway
update_perforce_rules
update_vmagent_service_if_present

write_monitoring_state_file

check_health

echo "
======================================================================
Update complete.

Data directories:
  Prometheus:       ${data_root}/prometheus/
  VictoriaMetrics:  ${data_root}/victoria-metrics/
  Alertmanager:     ${data_root}/alertmanager/

Alert rules:
  /etc/prometheus/perforce_rules.yml        (upstream - do not edit)
  /etc/prometheus/perforce_rules_local.yml  (your customizations)

Install state saved to: $state_file
  (Future updates will use these paths and versions automatically)

Ports that may need to be open in your firewall:
  9090  Prometheus UI
  9093  Alertmanager UI
  8428  VictoriaMetrics
  9100  Node Exporter (metrics)
  3000  Grafana UI
$(check_service_exists pushgateway 2>/dev/null && echo "  9091  Pushgateway")
======================================================================"
