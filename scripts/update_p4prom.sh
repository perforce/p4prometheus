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

VER_NODE_EXPORTER="1.3.1"
VER_P4PROMETHEUS="0.10.4"
VER_VICTORIA_METRICS="1.131.0"

# Default to amd but allow arm architecture
arch="amd64"
[[ $(uname -p) == 'aarch64' ]] && arch="arm64"

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

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

    # Find OSGROUP for ownership permissions - group of /p4 dir itself
    # shellcheck disable=SC2010
    OSGROUP=$(ls -al /p4/ | grep -E '\.$' | head -1 | awk '{print $4}')

    # Load SDP controlled shell environment.
    # shellcheck disable=SC1091
    source /p4/common/bin/p4_vars "$SDP_INSTANCE" ||\
    { echo -e "\\nError: Failed to load SDP environment.\\n"; exit 1; }

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

[[ -f "$p4prom_config_file" ]] || bail "Config file '$p4prom_config_file' does not exist - please run install_p4prom.sh instead of this script!"

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "downloading and extracting $url"
    wget -q "$url"
    tar zxvf "$fname"
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
    cat << EOF > $service_file
[Unit]
Description=P4prometheus
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Wants=network-online.target
After=network-online.target

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
ExecStart=${local_bin_dir}/${progname} --config=$p4prom_config_file

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable ${service_name}
    systemctl restart ${service_name}
    systemctl status ${service_name} --no-pager

}

update_p4metrics () {
    service_name="p4metrics"
    progname="p4metrics"
    service_file="/etc/systemd/system/${service_name}.service"
    curr_ver=$($progname --version 2>&1 | grep "$progname, version " | awk '{print $3}')
    if [[ "$curr_ver" == "v$VER_P4PROMETHEUS" ]]; then
        msg "Current version $curr_ver of $progname is up-to-date"
        return
    fi

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

    chown "$OSUSER:$OSGROUP" "$p4metrics_config_file"

    service_name="${progname}"
    service_file="/etc/systemd/system/${service_name}.service"
    msg "Creating service file for ${service_name}"
    cat << EOF > "${service_file}"
[Unit]
Description=P4metrics - part of P4prometheus
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Wants=network-online.target
After=network-online.target p4d_${SDP_INSTANCE}.service

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
ExecStart=${local_bin_dir}/${progname} --config=${p4metrics_config_file}

[Install]
WantedBy=multi-user.target
EOF

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

    cat << EOF > /etc/systemd/system/vmagent.service
[Unit]
Description=Victoria Metrics Agent
Wants=network-online.target
After=network-online.target

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
WorkingDirectory=/p4/common/site/config
EnvironmentFile=/p4/common/site/config/vmagent.env
ExecStart=/usr/local/bin/vmagent-prod \
  -promscrape.config=vmagent.yml \
  -remoteWrite.basicAuth.username=\${VM_CUSTOMER} \
  -remoteWrite.basicAuth.passwordFile=.vmpassword \
  -remoteWrite.urlRelabelConfig=relabelConfig.yml \
  -remoteWrite.url=\${VM_METRICS_HOST}/api/v1/write

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable vmagent
    # Don't start service yet - prompt user to do so at the end after verifying config files created!

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

    # Convert pushgateway port (9091) to vmagent port (9092)
    local vm_host="${host/:9091/:9092}"

    # Determine config directory based on SDP or non-SDP
    local vmagent_config_dir
    if [[ $UseSDP -eq 1 ]]; then
        vmagent_config_dir="/p4/common/site/config"
    else
        vmagent_config_dir="$p4prom_config_dir"
    fi

    mkdir -p "$vmagent_config_dir"
    chown "$OSUSER:$OSGROUP" "$vmagent_config_dir"

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
