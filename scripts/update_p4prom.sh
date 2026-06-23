#!/bin/bash
# Updates the following: p4prometheus, node_exporter.
#
# First version assumes SDP environment.
#

# shellcheck disable=SC2128
if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# ============================================================
# Configuration section

# This might also be /hxlogs/metrics or passed as a parameter (with -m flag)
metrics_root=/hxlogs/metrics
metrics_link=/p4/metrics
local_bin_dir=/usr/local/bin
# To avoid issues with SELinux, install service config files into /var/vmagent
vmagent_config_dir="/var/vmagent"


VER_NODE_EXPORTER="1.3.1"
VER_P4PROMETHEUS="0.11.0"
VER_VICTORIA_METRICS="1.131.0"

# Default to amd but allow arm architecture
arch="amd64"
[[ $(uname -p) == 'aarch64' ]] && arch="arm64"

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

get_dir_owner() {
    local dir=$1
    # GNU stat (Linux)
    if stat -c '%U' "$dir" >/dev/null 2>&1; then
        stat -c '%U' "$dir"
        return 0
    fi
    # BSD stat (macOS)
    if stat -f '%Su' "$dir" >/dev/null 2>&1; then
        stat -f '%Su' "$dir"
        return 0
    fi
    return 1
}

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

   echo "USAGE for update_p4prom.sh:

update_p4prom.sh [<instance> | -nosdp] [-m <metrics_root>] [-l <metrics_link>] [-u <osuser>] [-c <p4prom_config_dir>]

   or

update_p4prom.sh -h

    <metrics_root> is the directory where metrics will be written - default: $metrics_root
    <metrics_link> is an alternative link to metrics_root where metrics will be written - default: $metrics_link
                Typically only used for SDP installations.
    <osuser>    Operating system user, e.g. perforce, under which p4d process is running
    <p4prom_config_dir> Specify directory to install p4prometheus/p4metrics config files - useful for nonsdp installs
    -vmagent    Means install vmagent as replacement for pushgateway/report_data_instance cronjobs and config file.
                Not relevant for most installations - intended for P4RA installations only.

Specify either the SDP instance (e.g. 1), or -nosdp

WARNING: If using -nosdp, then please ensure P4PORT and P4USER are appropriately set in the environment and that you can connect
    to your server (e.g. you have done a 'p4 trust' if required, and are logged in already)

Examples:

./update_p4prom.sh 1
./update_p4prom.sh -nosdp -m /p4metrics -u perforce -c /etc/p4prometheus

"
}

# Command Line Processing

declare -i shiftArgs=0
declare -i UseSDP=1
declare -i SELinuxEnabled=0
declare -i NewP4MetricsConfig=0
declare -i InstallVMAgent=0
declare OsUser=""
declare P4LOG=""
declare p4prom_config_dir=""

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h  && exit 1;;
        # (-man) usage -man;;
        (-m) metrics_root=$2; shiftArgs=1;;
        (-u) OsUser="$2"; shiftArgs=1;;
        (-nosdp) UseSDP=0;;
        (-vmagent) InstallVMAgent=1;;
        (-l) P4LOG="$2"; shiftArgs=1;;
        (-c) p4prom_config_dir="$2"; shiftArgs=1;;
        (-*) usage -h "Unknown command line option ($1)." && exit 1;;
        (*) export SDP_INSTANCE=$1;;
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

# Check if the local_bin_dir exists
if [[ ! -d "$local_bin_dir" ]]; then
    echo "Error: Directory $local_bin_dir does not exist. Please create it before running this script!"
    exit 1
fi

command -v wget 2> /dev/null || bail "Failed to find wget in path - please install it"

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi

[[ -d "$metrics_root" ]] || bail "Specified metrics directory '$metrics_root' does not exist - please create it!"

if [[ $UseSDP -eq 1 ]]; then
    SDP_INSTANCE=${SDP_INSTANCE:-Unset}
    SDP_INSTANCE=${1:-$SDP_INSTANCE}
    if [[ $SDP_INSTANCE == Unset ]]; then
        echo -e "\\nError: Instance parameter not supplied.\\n"
        echo "You must supply the Perforce SDP instance as a parameter to this script. E.g."
        echo "    update_p4prom.sh 1"
        exit 1
    fi

    # Load SDP controlled shell environment.
    # shellcheck disable=SC1091
    source /p4/common/bin/p4_vars "$SDP_INSTANCE" ||\
    { echo -e "\\nError: Failed to load SDP environment.\\n"; exit 1; }

    OSGROUP=$(id -gn "$OSUSER")
    p4="$P4BIN -u $P4USER -p $P4PORT"
    $p4 info -s || bail "Can't connect to P4PORT: $P4PORT"
    p4prom_config_dir="/p4/common/config"
    p4prom_bin_dir="/p4/common/site/bin"
else
    SDP_INSTANCE=""
    p4port=${Port:-$P4PORT}
    p4user=${User:-$P4USER}
    OSUSER="$OsUser"
    OSGROUP=$(id -gn "$OSUSER")
    p4="p4 -u $p4user -p $p4port"
    $p4 info -s || bail "Can't connect to P4PORT: $p4port"
    p4prom_config_dir=${p4prom_config_dir:-"/etc/p4prometheus"}
    p4prom_bin_dir="$p4prom_config_dir"
fi

p4prom_config_file="$p4prom_config_dir/p4prometheus.yaml"
p4metrics_config_file="$p4prom_config_dir/p4metrics.yaml"
monitor_metrics_config_file="$p4prom_config_dir/monitor_metrics.yaml"

[[ -f "$p4prom_config_file" ]] || bail "Config file '$p4prom_config_file' does not exist - please run install_p4prom.sh instead of this script!"

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "downloading and extracting $url"
    wget -q "$url"
    tar zxvf "$fname"
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

update_node_exporter () {

    userid="node_exporter"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
        msg "Created user $userid"
    fi

    curr_ver=$(node_exporter --version 2>&1 | grep ' version ' | awk '{print $3}')
    if [[ "$curr_ver" == "$VER_NODE_EXPORTER" ]]; then
        msg "Current version $curr_ver of node_exporter is up-to-date"
        return
    fi

    sudo systemctl stop node_exporter

    cd /tmp || bail "Failed to cd to /tmp"
    PVER="$VER_NODE_EXPORTER"
    fname="node_exporter-$PVER.linux-${arch}.tar.gz"
    [[ -d node_exporter-$PVER.linux-${arch} ]] && rm -rf node_exporter-$PVER.linux-${arch}
    download_and_untar "$fname" "https://github.com/prometheus/node_exporter/releases/download/v$PVER/$fname"

    msg "Installing node_exporter"
    mv node_exporter-$PVER.linux-${arch}/node_exporter ${local_bin_dir}/

    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=${local_bin_dir}/node_exporter
        semanage fcontext -a -t bin_t $bin_file
        restorecon -vF $bin_file
    fi

    mkdir -p "$metrics_root"
    chown "$OSUSER:$OSGROUP" "$metrics_root"
    chmod 755 "$metrics_root"
    f=$(readlink -f "$metrics_root")
    while [[ $f != / ]]; do chmod 755 "$f"; f=$(dirname "$f"); done;

    if [[ $UseSDP -eq 1 ]]; then
        ln -s "$metrics_root" "$metrics_link"
        chown -h "$OSUSER:$OSGROUP" "$metrics_link"
    fi

    msg "Creating service file for node_exporter"
    cat << EOF > /etc/systemd/system/node_exporter.service
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

    sudo systemctl daemon-reload
    sudo systemctl enable node_exporter
    sudo systemctl start node_exporter
    sudo systemctl status node_exporter --no-pager
}

update_p4prometheus () {
    service_name="p4prometheus"
    progname="p4prometheus"
    service_file="/etc/systemd/system/${service_name}.service"
    curr_ver=$($progname --version 2>&1 | grep "$progname, version " | awk '{print $3}')
    if [[ "$curr_ver" == "v$VER_P4PROMETHEUS" ]]; then
        if [[ -f "$service_file" ]]; then
            msg "Updating existing service file for $service_name"
            write_p4prometheus_service_file "$service_file"
            systemctl daemon-reload
            systemctl enable ${service_name}
            systemctl restart ${service_name}
            systemctl status ${service_name} --no-pager
        fi
        msg "Current version $curr_ver of $progname is up-to-date"
        return
    fi

    systemctl stop $service_name

    PVER="$VER_P4PROMETHEUS"
    fname="${progname}.linux-${arch}.gz"
    url="https://github.com/perforce/p4prometheus/releases/download/v$PVER/$fname"
    msg "downloading and extracting $url"
    [[ -e ${progname}.linux-${arch} ]] && rm -f ${progname}.linux-${arch}
    wget -q "$url"

    gunzip "$fname"
    chmod +x ${progname}.linux-${arch}
    mv ${progname}.linux-${arch} ${local_bin_dir}/${progname}
    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=${local_bin_dir}/${progname}
        semanage fcontext -a -t bin_t $bin_file
        restorecon -vF $bin_file
    fi

    msg "Creating service file for $service_name"
    write_p4prometheus_service_file "$service_file"

    systemctl daemon-reload
    systemctl enable ${service_name}
    systemctl restart ${service_name}
    systemctl status ${service_name} --no-pager

}

update_p4metrics () {
    service_name="p4metrics"
    progname="p4metrics"
    service_file="/etc/systemd/system/${service_name}.service"
    up_to_date="false"
    curr_ver=$($progname --version 2>&1 | grep "$progname, version " | awk '{print $3}')
    if [[ "$curr_ver" == "v$VER_P4PROMETHEUS" ]]; then
        msg "Current version $curr_ver of $progname is up-to-date"
        up_to_date="true"
    fi

    if [[ "$up_to_date" != "true" ]]; then
        systemctl stop $service_name

        PVER="$VER_P4PROMETHEUS"
        fname="${progname}.linux-${arch}.gz"
        url="https://github.com/perforce/p4prometheus/releases/download/v$PVER/$fname"
        msg "downloading and extracting $url"
        wget -q "$url"

        gunzip "$fname"
        chmod +x "${progname}.linux-${arch}"
        mv "${progname}.linux-${arch}" "${local_bin_dir}/${progname}"

        if [[ $SELinuxEnabled -eq 1 ]]; then
            bin_file="${local_bin_dir}/${progname}"
            semanage fcontext -a -t bin_t "$bin_file"
            restorecon -vF "$bin_file"
        fi
    fi

    mkdir -p "$p4prom_config_dir" "$p4prom_bin_dir"
    chown "$OSUSER:$OSGROUP" "$p4prom_config_dir" "$p4prom_bin_dir"

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

    chown "$OSUSER:$OSGROUP" "$p4metrics_config_file"

    service_name="${progname}"
    service_file="/etc/systemd/system/${service_name}.service"
    msg "Creating service file for ${service_name}"
    write_p4metrics_service_file "$service_file"

    chmod 644 "${service_file}"
    systemctl daemon-reload
    systemctl enable "${service_name}"
    systemctl restart "${service_name}"
    systemctl status "${service_name}" --no-pager

    # Update the crontab of the specified user - to comment out entries relating to previous installs of monitoring
    # These are replaced by the systemd timers or p4metrics service.
    TEMP_FILE=$(mktemp)
    crontab -u "$OSUSER" -l > "$TEMP_FILE" 2>/dev/null || echo "" > "$TEMP_FILE"
    COMMENT="# This script has been replaced by systemd services/timers (p4metrics)"
    CHANGES_MADE=false
    for f in monitor_metrics.sh monitor_wrapper.sh; do
        if grep -v "^#" "$TEMP_FILE" | grep -q "${f}"; then
            cp "$TEMP_FILE" "${TEMP_FILE}.bak"
            sed -i "/^[^#].*\/${f}/ s|^|# ${COMMENT}\n# |" "$TEMP_FILE"
            CHANGES_MADE=true
        fi
    done
    if [ "$CHANGES_MADE" = true ]; then # Load up new crontab
        crontab -u "$OSUSER" "$TEMP_FILE"
    fi
}

update_monitor_locks_service() {
    local service_name="monitor_locks"
    local service_file="/etc/systemd/system/${service_name}.service"
    local updates_dir="/p4/common/site/bin"
    local venv_dir="${updates_dir}/.venv"
    local updates_script="${updates_dir}/check_for_updates.sh"
    local bootstrap_cmd=""
    if [[ ! -f "$service_file" ]]; then
        return
    fi

    if ! grep -qE '^[[:space:]]*ExecStart=.*monitor_wrapper\.sh' "$service_file"; then
        return
    fi

    if grep -qE '^[[:space:]]*ExecStart=.*monitor_wrapper\.sh.*[[:space:]]-c[[:space:]]' "$service_file"; then
        msg "monitor_locks service already has a monitor_metrics config argument"
    else
        msg "Updating monitor_locks service to include monitor_metrics config argument"
        sed -i "/^[[:space:]]*ExecStart=.*monitor_wrapper\\.sh/ s|$| -c ${monitor_metrics_config_file}|" "$service_file"
        systemctl daemon-reload
        systemctl restart ${service_name}.timer 2>/dev/null || true
        systemctl restart ${service_name}.service 2>/dev/null || true
        systemctl status ${service_name}.timer --no-pager 2>/dev/null || true
    fi

    if [[ -z "${OSUSER:-}" ]]; then
        msg "Warning: OSUSER is not set, skipping ${updates_script}"
        return
    fi

    if [[ ! -d "$venv_dir" ]]; then
        msg "No .venv found in ${updates_dir}; installing uv and Python dependencies as ${OSUSER}"
        bootstrap_cmd=$(cat <<'EOF'
set -e
cd /p4/common/site/bin
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
        if ! sudo -u "$OSUSER" /bin/bash -lc "$bootstrap_cmd"; then
            msg "Warning: Failed to bootstrap uv/.venv dependencies for ${OSUSER}"
        fi
    fi

    if [[ ! -x "$updates_script" ]]; then
        msg "Warning: ${updates_script} not found or not executable, skipping update check"
        return
    fi

    msg "Running check_for_updates.sh as ${OSUSER}"
    if ! sudo -u "$OSUSER" /bin/bash -lc 'cd /p4/common/site/bin && ./check_for_updates.sh'; then
        msg "Warning: check_for_updates.sh failed for user ${OSUSER}"
    fi
}

ensure_monitor_metrics_config_exists() {
    if [[ -f "$monitor_metrics_config_file" ]]; then
        return
    fi

    msg "Creating default monitor_metrics config: $monitor_metrics_config_file"
    cat << 'EOF' > "$monitor_metrics_config_file"
# monitor_metrics.yaml - configuration for monitor_metrics.py
#
# Pass this file via monitor_wrapper.sh -c <config_file>
# Requires pyyaml: pip install pyyaml

notifications:
    # Minimum number of blocked commands before any notification is sent.
    min_blocked_commands: 5

    # Seconds to wait between repeat notifications (avoids flooding).
    cooldown_seconds: 300

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

    chown "$OSUSER:$OSGROUP" "$monitor_metrics_config_file"
    chmod 640 "$monitor_metrics_config_file"
}

install_vmagent () {
    msg "Installing Victoria Metrics Agent..."

    # Check if already installed and stop if running
    if check_service_exists vmagent && systemctl is-active --quiet vmagent; then
        msg "Stopping existing vmagent service..."
        systemctl stop vmagent
    fi

    userid="$OSUSER"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp || bail "failed to cd"
    PVER="$VER_VICTORIA_METRICS"
    for fname in vmutils-linux-${arch}-v$PVER.tar.gz; do
        download_and_untar "$fname" "https://github.com/victoriametrics/victoriametrics/releases/download/v$PVER/$fname"
        TEMP_FILES+=("$fname")
    done

    for base_file in vmagent-prod; do
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

    # Create vmagent configuration files by parsing .push_metrics.cfg
    create_vmagent_configs

    write_vmagent_service_file /etc/systemd/system/vmagent.service

    systemctl daemon-reload
    systemctl enable vmagent
    # Don't start service yet - prompt user to do so at the end after verifying config files created!

}

write_vmagent_service_file() {
    local service_file=$1
    cat << EOF > "$service_file"
[Unit]
Description=Victoria Metrics Agent
Wants=network-online.target
After=network-online.target

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
WorkingDirectory=${vmagent_config_dir}
EnvironmentFile=${vmagent_config_dir}/vmagent.env
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

update_vmagent_service_if_present() {
    local service_name="vmagent"
    local service_file="/etc/systemd/system/${service_name}.service"
    if [[ ! -f "$service_file" ]]; then
        return
    fi

    msg "Updating existing vmagent service file"
    write_vmagent_service_file "$service_file"
    systemctl daemon-reload
    systemctl enable "$service_name"
    if systemctl is-active --quiet "$service_name"; then
        systemctl restart "$service_name"
        systemctl status "$service_name" --no-pager
    fi
}

create_vmagent_configs() {
    local config_file="$p4prom_config_dir/.push_metrics.cfg"

    if [[ ! -f "$config_file" ]]; then
        msg "Warning: Config file $config_file not found, skipping vmagent config creation"
        return
    fi

    # Parse the push_metrics.cfg file
    # shellcheck disable=SC1090
    source "$config_file"

    # Extract customer and instance from metrics values
    local customer="${metrics_customer:-}"
    local instance="${metrics_instance:-}"
    local host="${metrics_host:-}"

    if [[ -z "$customer" || -z "$instance" || -z "$host" ]]; then
        msg "Warning: Required metrics values not found in $config_file"
        return
    fi

    # Convert pushgateway port (9091) to vmagent port (9093 on load balancer)
    local vm_host="${host/:9091/:9093}"

    mkdir -p "$vmagent_config_dir"
    chown "$OSUSER:$OSGROUP" "$vmagent_config_dir"
    chmod 755 "$vmagent_config_dir"

    # Set SELinux context for config directory if SELinux is enabled
    if [[ $SELinuxEnabled -eq 1 ]]; then
        semanage fcontext -a -t etc_t "$vmagent_config_dir(/.*)?" 2>/dev/null || true
        restorecon -Rv "$vmagent_config_dir"
    fi

    # Create vmagent.env file
    local vmagent_env_file="$vmagent_config_dir/vmagent.env"
    cat << EOF > "$vmagent_env_file"
# For use with vmagent to send to P4RA monitoring server
VM_METRICS_HOST=$vm_host
VM_CUSTOMER=$customer
EOF

    chown "$OSUSER:$OSGROUP" "$vmagent_env_file"
    chmod 644 "$vmagent_env_file"
    msg "Created vmagent environment file: $vmagent_env_file"

    # Create relabelConfig.yml file
    local relabel_config_file="$vmagent_config_dir/relabelConfig.yml"
    cat << EOF > "$relabel_config_file"
# Relabelling config for vmagent
# These values are specific to each P4RA customer and need to conform to the values on P4RA Monitor server

# P4RA customer tag
- target_label: customer
  replacement: $customer

# Unique P4RA instance ID for this server
- target_label: instance
  replacement: $instance
EOF

    chown "$OSUSER:$OSGROUP" "$relabel_config_file"
    chmod 644 "$relabel_config_file"
    msg "Created vmagent config: $relabel_config_file"

    # Create vmagent.yml file
    local vmagent_config_file="$vmagent_config_dir/vmagent.yml"
    cat << EOF > "$vmagent_config_file"
# Configuration file for vmagent to scrape local node_exporter on (default) localhost:9100
global:
  scrape_interval:     30s # Set the scrape interval

scrape_configs:
  - job_name: 'remote_vmagent'
    static_configs:
    - targets:
        - localhost:9100
EOF

    chown "$OSUSER:$OSGROUP" "$vmagent_config_file"
    chmod 644 "$vmagent_config_file"
    msg "Created vmagent config: $vmagent_config_file"

    # Create .vmpassword file from metrics_passwd
    local password="${metrics_passwd:-}"
    if [[ -n "$password" ]]; then
        local vmpassword_file="$vmagent_config_dir/.vmpassword"
        echo "$password" > "$vmpassword_file"
        chown "$OSUSER:$OSGROUP" "$vmpassword_file"
        chmod 600 "$vmpassword_file"
        msg "Created vmagent password file: $vmpassword_file"
    else
        msg "Warning: metrics_passwd not found in $config_file"
    fi

    # Update the crontab of the specified user - to comment out push_gateway entry (replaced by vmagent)
    TEMP_FILE=$(mktemp)
    crontab -u "$OSUSER" -l > "$TEMP_FILE" 2>/dev/null || echo "" > "$TEMP_FILE"
    COMMENT="# This script has been replaced by systemd service (vmagent)"
    CHANGES_MADE=false
    for f in push_metrics.sh; do
        if grep -v "^#" "$TEMP_FILE" | grep -q "${f}"; then
            cp "$TEMP_FILE" "${TEMP_FILE}.bak"
            sed -i "/^[^#].*\/${f}/ s|^|# ${COMMENT}\n# |" "$TEMP_FILE"
            CHANGES_MADE=true
        fi
    done
    if [ "$CHANGES_MADE" = true ]; then # Load up new crontab
        crontab -u "$OSUSER" "$TEMP_FILE"
    fi

    msg ""
    msg "===================================="
    msg "vmagent configuration files created:"
    msg "  - $vmagent_env_file"
    msg "  - $relabel_config_file"
    msg "  - $vmagent_config_file"
    msg "  - $vmagent_config_dir/.vmpassword"
    msg "===================================="
    msg ""
}

update_node_exporter
update_p4prometheus
update_p4metrics
ensure_monitor_metrics_config_exists
update_monitor_locks_service
update_vmagent_service_if_present
if [[ $InstallVMAgent -eq 1 ]]; then
    install_vmagent
fi
# update_monitor_locks
systemctl list-timers | grep -E "^NEXT|monitor"

echo "

Have updated node_exporter and p4prometheus.
"

if [[ $InstallVMAgent -eq 1 ]]; then
    echo "Please start the  vmagent service when you have checked its configuration...

    systemctl start vmagent
    systemctl status vmagent --no-pager
    "
fi

if [[ $NewP4MetricsConfig -eq 1 ]]; then
    echo "A new p4metrics config file has been created at: $p4metrics_config_file

Please edit this file to set any required parameters and consider re-starting the p4metrics service.

    sudo systemctl restart p4metrics
"
fi

if [[ -f "$p4metrics_config_file" ]] || [[ -f "$monitor_metrics_config_file" ]]; then
        echo "
Manual review required:

If p4metrics/monitor_metrics were installed or updated, please manually review and update these files as needed:
    - $p4metrics_config_file
    - $monitor_metrics_config_file
"
fi
