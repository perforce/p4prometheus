#!/bin/bash

# Shared helper functions for p4prom_install/update scripts.

if [[ -n "${P4PROM_COMMON_SH_LOADED:-}" ]]; then
    return 0
fi
P4PROM_COMMON_SH_LOADED=1

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

systemd_enable_and_restart() {
    local service_file=$1
    local service_name=$2

    chmod 644 "$service_file"
    systemctl daemon-reload
    systemctl enable "$service_name"
    systemctl start "$service_name"
    systemctl status "$service_name" --no-pager
}

download_and_untar () {
    local fname=$1
    local url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "downloading and extracting $url"
    wget -q "$url"
    tar zxvf "$fname"
}

bootstrap_monitor_python_env () {
    local target_dir=$1
    local venv_dir="${target_dir}/.venv"
    local bootstrap_cmd=""

    if [[ -d "$venv_dir" ]]; then
        return
    fi

    msg "No .venv found in ${target_dir}; installing uv and Python dependencies as ${OSUSER}"
    bootstrap_cmd=$(cat <<'EOF'
set -e
cd "__TARGET_DIR__"
export PATH="$HOME/.local/bin:$PATH"
if ! command -v uv >/dev/null 2>&1; then
    curl -LsSf https://astral.sh/uv/install.sh | sh
    export PATH="$HOME/.local/bin:$PATH"
fi
uv python install
uv venv .venv
source .venv/bin/activate
uv pip install pyyaml
EOF
)
    bootstrap_cmd=${bootstrap_cmd/__TARGET_DIR__/$target_dir}

    if ! sudo -u "$OSUSER" /bin/bash -lc "$bootstrap_cmd"; then
        msg "Warning: Failed to bootstrap uv/.venv dependencies for ${OSUSER} in ${target_dir}"
    fi
}

write_node_exporter_service_file() {
    local service_file=$1
    local userid=${2:-node_exporter}

    cat << EOF > "$service_file"
[Unit]
Description=Node Exporter
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Group=$userid
Type=simple
ExecStart=${local_bin_dir}/node_exporter --collector.systemd \
  --collector.systemd.unit-include=(p4.*|node_exporter).service \
  --collector.textfile.directory=$metrics_root

[Install]
WantedBy=multi-user.target
EOF
}

write_p4prometheus_service_file() {
    local service_file=$1
    cat << EOF > "$service_file"
[Unit]
Description=P4prometheus
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Wants=network-online.target
After=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
ExecStart=${local_bin_dir}/p4prometheus --config=$p4prom_config_file
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
EOF
}

write_p4metrics_service_file() {
    local service_file=$1
    cat << EOF > "$service_file"
[Unit]
Description=P4metrics - part of P4prometheus
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Wants=network-online.target
After=network-online.target p4d_${SDP_INSTANCE}.service
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
ExecStart=${local_bin_dir}/p4metrics --config=${p4metrics_config_file}
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
EOF
}

write_or_update_p4metrics_config_file() {
    msg "Writing/updating default p4metrics config: $p4metrics_config_file"

    # Write the file in chunks, so that we can add new parameters to existing config files without overwriting previous contents.
    if [[ ! -f "$p4metrics_config_file" ]]; then
        NewP4MetricsConfig=1
        cat << EOF > "$p4metrics_config_file"
# ----------------------
# metrics_root: REQUIRED! Directory into which to write metrics files for processing by node_exporter.
# Ensure that node_exporter user has read access to this folder (and any parent directories)!
metrics_root: $metrics_root

# ----------------------
# sdp_instance: SDP instance - typically integer, but can be alphanumeric
# See: https://swarm.workshop.perforce.com/projects/perforce-software-sdp for more
# If this value is blank then it is assumed to be a non-SDP instance, and you will want
# to set other values with a prefix of p4 below.
sdp_instance:   $SDP_INSTANCE

# ----------------------
# p4port: The value of P4PORT to use
# IGNORED if sdp_instance is non-blank!
p4port:         $p4port

# ----------------------
# p4user: The value of P4USER to use
# IGNORED if sdp_instance is non-blank!
p4user:         $p4user

# ----------------------
# p4config: The value of a P4CONFIG to use
# This is very useful and should be set to an absolute path if you need values like P4TRUST/P4TICKETS etc
# IGNORED if sdp_instance is non-blank!
p4config:

# ----------------------
# p4bin: The absolute path to the p4 binary to be used - important if not available in your PATH
# E.g. /some/path/to/p4
# IGNORED if sdp_instance is non-blank! (Will use /p4/<instance>/bin/p4_<instance>)
p4bin:      p4

# ----------------------
# p4dbin: The absolute path to the p4d binary to be used - important if not available in your PATH
# E.g. /some/path/to/p4d
# IGNORED if sdp_instance is non-blank! (Will use /p4/<instance>/bin/p4d_<instance>)
p4dbin:     p4d

# ----------------------
# update_interval: how frequently metrics should be written - defaults to 1m
# Values are as parsed by Go, e.g. 1m or 30s etc.
update_interval:    1m

# ----------------------
# cmds_by_user: true/false - Whether to output metrics p4_monitor_by_user
# Normally this should be set to true as the metrics are useful.
# If you have a p4d instance with hundreds/thousands of users you may find the number
# of metrics labels is too great (one per distinct user), so set this to false.
# Or set it to false if any personal information concerns
cmds_by_user:   true

# ----------------------
# monitor_swarm: true/false - Whether to monitor status and version of swarm
# Normally this should be set to true - won't run if there is no Swarm property
monitor_swarm:   true

# ----------------------
# swarm_url: URL of the Swarm instance to monitor
# Normally this is blank, and p4metrics reads the p4 property value
# Sometimes (e.g. due to VPN setup) that value is not correct - so set this instead
# swarm_url: https://swarm.example.com
swarm_url:

# ----------------------
# swarm_secure: true/false - Whether to validate SSL for swarm
# Defaults to true, but if you have a self-signed certificate or similar set to false
swarm_secure: true

EOF
    fi

    # Now check if max_journal_size or max_journal_percent are present - if not then add them with defaults
    # This allows us to add new parameters to existing config files
    if ! grep -qE '^[[:space:]]*#?[[:space:]]*max_journal_size:' "$p4metrics_config_file"; then
        cat << EOF >> "$p4metrics_config_file"
# ----------------------
# max_journal_size: Maximum size of journal file to monitor, e.g. 10G, 0 means no limit
# Units are K/M/G/T/P (powers of 1024), e.g. 10M, 1.5G etc
# If the journal file is larger than this value it will be rotated using: p4 admin journal
# This is useful to avoid sudden large journal growth causing disk space issues (often a sign of automation problems).
# Note that this is only actioned if the p4d server is a "standard" or "commit-server" (so no replicas or edge servers).
# The system will only rotate the journal if the user is a super user and the journalPrefix volume has sufficient free space.
# Leave blank or set to 0 to disable (see max_journal_percent below for alternative).
max_journal_size:

# ----------------------
# max_journal_percent: Maximum size of journal as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
# Values are integers 0-99
# Volume information is read using: p4 diskspace
# If the journal file is larger than this percentag value it will be rotated using: p4 admin journal
# This is useful to avoid sudden large journal growth causing disk space issues (often a sign of automation problems).
# Note that this is only actioned if the p4d server is a "standard" or "commit-server" (so no replicas or edge servers).
# The system will only rotate the journal if the journalPrefix volume has sufficient free space.
# Leave blank or set to 0 to disable (see max_journal_size above for alternative).
max_journal_percent:    30

# ----------------------
# max_log_size: Maximum size of P4LOG file to monitor - similar to max_journal_size above
# Units are K/M/G/T/P (powers of 1024), e.g. 10M, 1.5G etc
# If the log file is larger than this value it will be rotated and compressed (using rename + gzip)
max_log_size:

# ----------------------
# max_log_percent: Maximum size of log as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
# Values are integers 0-99
# Volume information is read using: p4 diskspace
# If the log file is larger than this percentage value it will be rotated and compressed (using rename + gzip)
max_log_percent:        30

EOF
    fi

    # Now add section for monitor_ignore and monitor_groups if not present - these are used to control which 
    # commands are monitored and how they are grouped in the p4_monitor_cmds metric
    if ! grep -qE '^[[:space:]]*#?[[:space:]]*monitor_ignore:' "$p4metrics_config_file"; then
        cat << EOF >> "$p4metrics_config_file"
# ----------------------
# monitor_ignore: Monitor commmands to ignore - e.g. long running background tasks
# Values are a Go regex pattern - e.g. "admin resource-monitor|ldapsync"
monitor_ignore: "admin resource-monitor|ldapsync"

# ----------------------
# monitor_groups: Optional (but recommended) grouping of commands for monitor entries (useful for spotting slow commands).
# Each entry has:
#   commands: a Go regex pattern matching command names
#   label: a name for this group of commands - used as a label value in the p4_monitor_commands metric, so should be a valid label value (see reLabelName in config.go for details)
# These values are ignored if monitor_ignore matches (first match wins), 
# and then the command is checked against the patterns in order, with the first match winning (so more specific patterns should come first).
# Note that only Running commands (state 'R') are counted for these groups, not Background ('B') or Idle ('I'), 
# as typically you want to monitor the runtime of active commands (and some IDLE commands can be long running and skew the metrics).
# Example:
# monitor_groups:
# - commands: "^rmt.*"
#   label:    rmt
# - commands: "sync|transmit"
#   label: sync_transmit
# - commands: ".*"
#   label:    other
monitor_groups:
  - commands: "^rmt.*"
    label:    rmt
  - commands: "sync|transmit"
    label:    sync_transmit
  - commands: ".*"
    label:    other

EOF
    fi

    # Now add section for memory_by_user and memlimits if not present - these are used to control how memory
    # is monitored and whether to terminate commands which exceed limits. See comments in config file for details.
    if ! grep -qE '^[[:space:]]*#?[[:space:]]*memory_by_user:' "$p4metrics_config_file"; then
        cat << EOF >> "$p4metrics_config_file"

# ----------------------
# memory_by_user: true/false - Whether to output metric p4_active_memory_by_user
# Normally this should be set to true as the metric is useful.
# If you have a p4d instance with hundreds/thousands of users you may find the number
# of metrics labels is too great (one per distinct user), so set this to false.
# Or set it to false if any personal information concerns
memory_by_user:   true

# ----------------------
# memlimits: Optional (but recommended) way to define which users and commands to monitor for memory limits 
#   (useful for inadvertently high memory usage). Some users run commands on inappropriate paths such as the entire repository,
#   or a huge depot. Commands which exceed these settings have 'p4 monitor terminate' run on them, which will ask the command to terminate.
#   This is related to the MaxMemory setting for p4 groups but has some more flexibility for cumulative limits across multiple commands for a user.
# candidate_cmds: A Go regex pattern matching command names to be considered for memory monitoring - e.g. "sync|transmit|print|fstat|files|changes"
#   We default to reporting commands only.
# enabled: true/false - whether to enable this memory monitoring functionality (if false will report the metrics but not take any action, 
#   so you can monitor the metrics and adjust settings before enabling the termination functionality).
# enforce_kills: true/false - whether to actually enforce kills when limits are exceeded (if false, will only report)
# Groups:
#   Each entry has:
#     description: Name for this group of settings - used for logging and debugging, so should be unique and descriptive
#     users:       Go regex pattern matching user names
#     cmd_max_percentage:             0-99, where 0 means no limit
#     cmd_max_value:                  Units are M/G (powers of 1024), e.g. 10M, 1.5G etc, if blank or 0 then no limit
#     user_cumulative_max_percentage: For all commands for a user, 0-99, where 0 means no limit
#     user_cumulative_max_value:      Units are M/G (powers of 1024), e.g. 10M, 1.5G etc, if blank or 0 then no limit
# THE ORDER OF THE GROUPS IS IMPORTANT - the first match wins, so more specific patterns should come first (e.g. admin users should be first, 
# with no limits, and then (optionally) a group for build users with higher limits, followed by a catch-all for other users with limits).
# Note that only Running commands (state 'R') and Idle ('I') are counted for these groups, not Background ('B'), 
# since Background commands are things like replication and resource monitoring
# Example:
memlimits:
  candidate_cmds:  "annotate|changes|changelists|describe|diff|diff2|filelog|files|fstat|grep|integrated|interchanges|istat|opened|print|sync|transmit|IDLE"
  enabled:         true
  enforce_kills:   false
  groups:
  - description: "No limits for super users (as they hopefully know what they are doing!)"
    users: "super|perforce|p4admin"
    cmd_max_percentage:             
    cmd_max_value:                  
    user_cumulative_max_percentage: 
    user_cumulative_max_value:      
  - description: "Default limits for all other users"
    users: ".*"
    cmd_max_percentage:             40%
    cmd_max_value:                  
    user_cumulative_max_percentage: 60%
    user_cumulative_max_value:      

EOF
    fi

    # Now add section for parse_journal if not present - whether to parse P4JOURNAL in the background 
    # and emit p4_journal_records_count metrics
    if ! grep -qE '^[[:space:]]*#?[[:space:]]*parse_journal:' "$p4metrics_config_file"; then
        cat << EOF >> "$p4metrics_config_file"

# ----------------------
# parse_journal: true/false - Whether to parse active P4JOURNAL in the background
# Normally this should be set to true to output p4_journal_records_count metrics.
# Set to false if you want to disable journal tailing/parsing completely.
parse_journal:   true

EOF
    fi

    chown "$OSUSER:$OSGROUP" "$p4metrics_config_file"
    chmod 640 "$p4metrics_config_file"
}

write_default_monitor_metrics_config() {
    local config_file=$1

    cat << 'EOF' > "$config_file"
# monitor_metrics.yaml - configuration for monitor_metrics.py
#
# Pass this file via monitor_wrapper.sh -c <config_file>
# Requires pyyaml: pip install pyyaml

notifications:
    # Minimum number of blocked commands before any notification is sent.
    min_blocked_commands: 30

    # Seconds to wait between repeat notifications (avoids flooding).
    cooldown_seconds: 1500

    # File used to track the timestamp of the last notification.
    # Must be writable by the user running monitor_metrics.py.
    state_file: "/tmp/monitor_metrics.notify.state"

    # Optional text shown as the first line after the title in chat notifications.
    notification_text: ""

    # Optional URL used to render an "Open Runbook" button.
    runbook_url: ""

    # Maximum lines for Slack/Teams chat payloads (summary + blocking tree).
    # Set to 0 for no line limit.
    max_lines: 40

    slack:
        enabled: false
        webhook_url: "https://<some>/<webhook>/<url>"
        # Optional overrides:
        # max_lines: 40
        # runbook_url: ""

    email:
        enabled: false
        smtp_host: "localhost"
        smtp_port: 25
        use_tls: false
        username: ""
        password: ""
        from_addr: "p4monitor@example.com"
        to_addrs:
            - "admin@example.com"
        subject: "P4 Lock Alert"

    teams:
        enabled: false
        webhook_url: "https://<some>/<webhook>/<url>"
        # Optional overrides:
        # max_lines: 40
        # runbook_url: ""

    # Generic script called with JSON payload on stdin.
    script:
        enabled: false
        command: "/usr/local/bin/p4_lock_notify.sh"
EOF
}

ensure_monitor_metrics_config_file_exists() {
    local config_file=${1:-${monitor_metrics_config_file:-}}

    if [[ -z "$config_file" ]]; then
        bail "monitor_metrics_config_file is not set and no config path was supplied"
    fi

    if [[ -f "$config_file" ]]; then
        msg "monitor_metrics config already exists: $config_file"
        return 0
    fi

    mkdir -p "$(dirname "$config_file")"
    write_default_monitor_metrics_config "$config_file"

    if [[ -n "${OSUSER:-}" ]] && [[ -n "${OSGROUP:-}" ]]; then
        chown "$OSUSER:$OSGROUP" "$config_file"
    fi
    chmod 640 "$config_file"
    msg "Created default monitor_metrics config: $config_file"
}

write_vmagent_service_file() {
    local service_file=$1
    local vm_cfg_dir=${vmagent_config_dir:-/var/vmagent}
    cat << EOF > "${service_file}"
[Unit]
Description=Victoria Metrics Agent
Wants=network-online.target
After=network-online.target

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
WorkingDirectory=${vm_cfg_dir}
EnvironmentFile=${vm_cfg_dir}/vmagent.env
ExecStart=/usr/local/bin/vmagent-prod \
  -memory.allowedPercent=20 \
  -promscrape.config=vmagent.yml \
  -remoteWrite.basicAuth.username=\${VM_CUSTOMER} \
  -remoteWrite.basicAuth.passwordFile=.vmpassword \
  -remoteWrite.urlRelabelConfig=relabelConfig.yml \
  -remoteWrite.url=\${VM_METRICS_HOST}/api/v1/write

[Install]
WantedBy=multi-user.target
EOF
}

comment_out_push_metrics_cron() {
    local osuser=$1
    local temp_file
    temp_file=$(mktemp)
    crontab -u "$osuser" -l > "$temp_file" 2>/dev/null || echo "" > "$temp_file"
    local comment="# This script has been replaced by systemd service (vmagent)"
    local changes_made=false
    if grep -v "^#" "$temp_file" | grep -q "push_metrics.sh"; then
        cp "$temp_file" "${temp_file}.bak"
        sed -i "/^[^#].*\/push_metrics.sh/ s|^|# ${comment}\\n# |" "$temp_file"
        changes_made=true
    fi
    if [[ "$changes_made" == "true" ]]; then
        crontab -u "$osuser" "$temp_file"
    fi
}

create_vmagent_configs_from_push_config() {
    local config_file=${1:-${p4prom_config_dir:-}/.push_metrics.cfg}
    local vm_cfg_dir=${vmagent_config_dir:-/var/vmagent}

    if [[ ! -f "$config_file" ]]; then
        msg "Warning: Config file $config_file not found, skipping vmagent config creation"
        return
    fi

    # shellcheck disable=SC1090
    source "$config_file"

    local customer="${metrics_customer:-}"
    local instance="${metrics_instance:-}"
    local host="${metrics_host:-}"
    local password="${metrics_passwd:-}"

    if [[ -z "$customer" || -z "$instance" || -z "$host" ]]; then
        msg "Warning: Required metrics values not found in $config_file"
        return
    fi

    local vm_host="${host/:9091/:9093}"

    mkdir -p "$vm_cfg_dir"
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir"
    chmod 755 "$vm_cfg_dir"

    if [[ ${SELinuxEnabled:-0} -eq 1 ]]; then
        semanage fcontext -a -t etc_t "$vm_cfg_dir(/.*)?" 2>/dev/null || true
        restorecon -Rv "$vm_cfg_dir"
    fi

    cat << EOF > "$vm_cfg_dir/vmagent.env"
# For use with vmagent to send to P4RA monitoring server
VM_METRICS_HOST=$vm_host
VM_CUSTOMER=$customer
EOF
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/vmagent.env"
    chmod 644 "$vm_cfg_dir/vmagent.env"

    cat << EOF > "$vm_cfg_dir/relabelConfig.yml"
# Relabelling config for vmagent
# These values are specific to each P4RA customer and need to conform to the values on P4RA Monitor server

# P4RA customer tag
- target_label: customer
  replacement: $customer

# Unique P4RA instance ID for this server
- target_label: instance
  replacement: $instance
EOF
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/relabelConfig.yml"
    chmod 644 "$vm_cfg_dir/relabelConfig.yml"

    cat << EOF > "$vm_cfg_dir/vmagent.yml"
# Configuration file for vmagent to scrape local node_exporter on (default) localhost:9100
global:
  scrape_interval:     30s # Set the scrape interval

scrape_configs:
  - job_name: 'remote_vmagent'
    static_configs:
    - targets:
        - localhost:9100
EOF
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/vmagent.yml"
    chmod 644 "$vm_cfg_dir/vmagent.yml"

    if [[ -n "$password" ]]; then
        echo "$password" > "$vm_cfg_dir/.vmpassword"
        chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/.vmpassword"
        chmod 600 "$vm_cfg_dir/.vmpassword"
    else
        msg "Warning: metrics_passwd not found in $config_file"
    fi

    comment_out_push_metrics_cron "$OSUSER"
}

create_vmagent_temp_configs() {
    local vm_cfg_dir=${vmagent_config_dir:-/var/vmagent}

    mkdir -p "$vm_cfg_dir"
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir"
    chmod 755 "$vm_cfg_dir"

    if [[ ${SELinuxEnabled:-0} -eq 1 ]]; then
        semanage fcontext -a -t etc_t "$vm_cfg_dir(/.*)?" 2>/dev/null || true
        restorecon -Rv "$vm_cfg_dir"
    fi

    cat << EOF > "$vm_cfg_dir/vmagent.env"
# For use with vmagent to send to P4RA monitoring server
VM_METRICS_HOST=https://monitor.hra.p4demo.com:9093
VM_CUSTOMER=customerid_CHANGEME
EOF
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/vmagent.env"
    chmod 644 "$vm_cfg_dir/vmagent.env"

    cat << EOF > "$vm_cfg_dir/relabelConfig.yml"
# Relabelling config for vmagent
# These values are specific to each P4RA customer and need to conform to the values on P4RA Monitor server

# P4RA customer tag
- target_label: customer
  replacement: customerid_CHANGEME

# Unique P4RA instance ID for this server
- target_label: instance
  replacement: customerid-prod-hra_CHANGEME
EOF
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/relabelConfig.yml"
    chmod 644 "$vm_cfg_dir/relabelConfig.yml"

    cat << EOF > "$vm_cfg_dir/vmagent.yml"
# Configuration file for vmagent to scrape local node_exporter on (default) localhost:9100
global:
  scrape_interval:     30s # Set the scrape interval

scrape_configs:
  - job_name: 'remote_vmagent'
    static_configs:
    - targets:
        - localhost:9100
EOF
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/vmagent.yml"
    chmod 644 "$vm_cfg_dir/vmagent.yml"

    echo "MySecurePassword_CHANGEME" > "$vm_cfg_dir/.vmpassword"
    chown "$OSUSER:$OSGROUP" "$vm_cfg_dir/.vmpassword"
    chmod 600 "$vm_cfg_dir/.vmpassword"
}

install_vmagent() {
    local mode=${1:-${VMAGENT_CONFIG_MODE:-push_config}}
    local vm_cfg_dir=${vmagent_config_dir:-/var/vmagent}

    msg "Installing Victoria Metrics Agent..."

    if systemctl list-unit-files | grep -q "^vmagent.service" && systemctl is-active --quiet vmagent; then
        msg "Stopping existing vmagent service..."
        systemctl stop vmagent
    fi

    local userid="$OSUSER"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    local pver="${VER_VICTORIA_METRICS:-}"
    [[ -n "$pver" ]] || bail "VER_VICTORIA_METRICS is not set"
    local cpu_arch="${arch:-amd64}"
    local fname="vmutils-linux-${cpu_arch}-v${pver}.tar.gz"
    download_and_untar "$fname" "https://github.com/victoriametrics/victoriametrics/releases/download/v${pver}/$fname"

    if [[ -f "vmagent-prod" ]]; then
        local bin_file=/usr/local/bin/vmagent-prod
        mv "vmagent-prod" /usr/local/bin/
        chown "$userid:$userid" "$bin_file"
        chmod 755 "$bin_file"
        if [[ ${SELinuxEnabled:-0} -eq 1 ]]; then
            semanage fcontext -a -t bin_t "$bin_file" 2>/dev/null || true
            restorecon -vF "$bin_file"
        fi
    else
        bail "Failed to find vmagent-prod after download"
    fi

    if [[ "$mode" == "temp" ]]; then
        create_vmagent_temp_configs
    else
        create_vmagent_configs_from_push_config
    fi

    write_vmagent_service_file /etc/systemd/system/vmagent.service
    systemctl daemon-reload
    systemctl enable vmagent

    msg ""
    msg "===================================="
    msg "vmagent configuration files created:"
    msg "  - $vm_cfg_dir/vmagent.env"
    msg "  - $vm_cfg_dir/relabelConfig.yml"
    msg "  - $vm_cfg_dir/vmagent.yml"
    msg "  - $vm_cfg_dir/.vmpassword"
    msg "===================================="
    msg ""
}

update_vmagent_service_if_present() {
    local service_name="vmagent"
    local service_file="/etc/systemd/system/${service_name}.service"
    if [[ ! -f "${service_file}" ]]; then
        return
    fi

    msg "Updating existing vmagent service file"
    write_vmagent_service_file "${service_file}"
    systemctl daemon-reload
    systemctl enable "$service_name"
    if systemctl is-active --quiet "$service_name"; then
        systemctl restart "$service_name"
        systemctl status "$service_name" --no-pager
    fi
}

# ── Check AWS CLI version ────────────────────────────────────────────────────
check_aws_cli_version() {
    local version_output=""
    local major_version=""
    local arch=""
    local pkg_arch=""
    local zip_url=""
    local install_dir="/tmp/awscliv2-install"

    version_output=$(aws --version 2>&1 || true)
    major_version=$(echo "$version_output" | sed -n 's/^aws-cli\/\([0-9]\+\)\..*/\1/p')

    if [[ -n "$major_version" ]] && [[ "$major_version" -ge 2 ]]; then
        return 0
    fi

    msg "AWS CLI v2 required for EBS Throughput data; installing/upgrading now"

    if [[ $(id -u) -ne 0 ]]; then
        bail "AWS CLI v2 installation requires root privileges (run as root/sudo)"
    fi

    if command -v yum >/dev/null 2>&1; then
        yum remove awscli -y >/dev/null 2>&1 || true
        yum install -y unzip curl >/dev/null
    elif command -v dnf >/dev/null 2>&1; then
        dnf remove awscli -y >/dev/null 2>&1 || true
        dnf install -y unzip curl >/dev/null
    elif command -v apt-get >/dev/null 2>&1; then
        apt-get update -y >/dev/null
        apt-get remove -y awscli >/dev/null 2>&1 || true
        apt-get install -y unzip curl >/dev/null
    else
        bail "Unsupported package manager for automatic AWS CLI installation"
    fi

    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64) pkg_arch="x86_64" ;;
        aarch64|arm64) pkg_arch="aarch64" ;;
        *) bail "Unsupported architecture for AWS CLI v2 install: $arch" ;;
    esac

    zip_url="https://awscli.amazonaws.com/awscli-exe-linux-${pkg_arch}.zip"

    rm -rf "$install_dir"
    mkdir -p "$install_dir"
    cd "$install_dir" || bail "Failed to cd to install dir: $install_dir"

    curl -fsSL "$zip_url" -o awscliv2.zip || bail "Failed to download AWS CLI v2 from $zip_url"
    unzip -q -o awscliv2.zip || bail "Failed to unzip awscliv2.zip"

    if [[ -x /usr/local/bin/aws ]]; then
        ./aws/install --update || bail "Failed to update AWS CLI v2"
    else
        ./aws/install || bail "Failed to install AWS CLI v2"
    fi

    version_output=$(aws --version 2>&1 || true)
    major_version=$(echo "$version_output" | sed -n 's/^aws-cli\/\([0-9]\+\)\..*/\1/p')

    if [[ -z "$major_version" ]] || [[ "$major_version" -lt 2 ]]; then
        bail "AWS CLI install attempted but v2 is not active. Current version output: ${version_output:-unknown}"
    fi

    msg "AWS CLI successfully installed/upgraded: $version_output"
}
