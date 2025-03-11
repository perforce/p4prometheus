#!/bin/bash
# Installs the following: p4prometheus, node_exporter and monitor_metrics.sh
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
# This is for SDP installs only
metrics_link=/p4/metrics
# Just in case you want to customize this
local_bin_dir=/usr/local/bin

VER_NODE_EXPORTER="1.3.1"
VER_P4PROMETHEUS="0.9.0"

# Default to amd but allow arm architecture
arch="amd64"
[[ $(uname -p) == 'aarch64' ]] && arch="arm64"

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

function usage
{
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for install_p4prom.sh:

install_p4prom.sh [<instance> | -nosdp] [-m <metrics_root>] [-osuser <osuser>] 
        [-p <P4PORT>] [-u <p4user>] [-c <p4prom_config_dir>] [-push]

   or

install_p4prom.sh -h

    <metrics_root>  is the directory where metrics will be written - default: $metrics_root
    <osuser>        Operating system user, e.g. perforce, under which p4d process is running and to install crontab
    <P4PORT>        P4PORT to use within any installed scripts
    <P4USER>        P4USER to use within any installed scripts
    <p4prom_config_dir> Specify directory to install p4prometheus config file - useful for nonsdp installs
    -push           Means install pushgateway/report_data_instance cronjobs and config file.
                    Not relevant for most installations.

IMPORTANT: Specify either the SDP instance (e.g. 1), or -nosdp and other parameters

WARNING: If using -nosdp, then please ensure P4PORT and P4USER are provided or are appropriately set and that you can connect
    to your server (e.g. you have done a 'p4 trust' if required, and logged in already)

Examples:

./install_p4prom.sh 1
./install_p4prom.sh -nosdp -m /p4metrics -u perforce -p 1666 -u p4admin -c /p4/p4prometheus

"
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i UseSDP=1
declare -i SELinuxEnabled=0
declare -i InstallPushgateway=0
declare OsUser=""
declare p4port=""
declare p4user=""
declare P4LOG=""
declare P4SERVERID=""
declare p4prom_config_dir=""

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h  && exit 1;;
        # (-man) usage -man;;
        (-nosdp) UseSDP=0;;
        (-m) metrics_root=$2; shiftArgs=1;;
        (-osuser) OsUser="$2"; shiftArgs=1;;
        (-p) p4port="$2"; shiftArgs=1;;
        (-u) p4user="$2"; shiftArgs=1;;
        (-push) InstallPushgateway=1;;
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
    echo "Error: Directory $local_bin_dir does not exist. Please create it before running this script."
    exit 1
fi

command -v wget 2> /dev/null || bail "Failed to find wget in path"

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi

if [[ $UseSDP -eq 1 ]]; then
    SDP_INSTANCE=${SDP_INSTANCE:-Unset}
    SDP_INSTANCE=${1:-$SDP_INSTANCE}
    if [[ $SDP_INSTANCE == Unset ]]; then
        echo -e "\\nError: Instance parameter not supplied.\\n"
        echo "You must supply the Perforce SDP instance as a parameter to this script. E.g."
        echo "    install_p4prom.sh 1"
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
    p4port=${p4port:-$P4PORT}
    p4user=${p4user:-$P4USER}
    OSUSER="$OsUser"
    OSGROUP=$(id -gn "$OSUSER")
    p4="p4 -u $p4user -p $p4port"
    $p4 info -s || bail "Can't connect to P4PORT: $p4 info -s"
    $p4 login -s || bail "Error - can't run: $p4 login -s"
    P4PORT=$p4port
    P4USER=$p4user
    P4SERVERID=$($p4 info -s | grep "^ServerID" | awk '{print $2}')
    P4LOG=$($p4 configure show P4LOG | awk '{print $1}' | sed -e 's/P4LOG=//')
    [[ -n "$P4SERVERID" ]] || bail "Failed to find P4 serverid value"
    [[ -n "$P4LOG" ]] ||  bail "Failed to find P4LOG value"
    p4prom_config_dir=${p4prom_config_dir:-"/etc/p4prometheus"}
    p4prom_bin_dir="$p4prom_config_dir"
fi

p4prom_config_file="$p4prom_config_dir/p4prometheus.yaml"
p4metrics_config_file="$p4prom_config_dir/p4metrics.yaml"

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "downloading and extracting $url"
    wget -q "$url"
    tar zxvf "$fname"
}

install_node_exporter () {

    userid="node_exporter"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
        msg "Created user $userid"
    fi

    cd /tmp || bail "Failed to cd to /tmp"
    PVER="$VER_NODE_EXPORTER"
    fname="node_exporter-$PVER.linux-${arch}.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/node_exporter/releases/download/v$PVER/$fname"

    tar xvf node_exporter-$PVER.linux-${arch}.tar.gz 
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

    if [[ $UseSDP -eq 1 ]] && [[ ! -L "$metrics_link" ]]; then
        ln -sf "$metrics_root" "$metrics_link"
        chown -h "$OSUSER:$OSGROUP" "$metrics_link"
    fi

    service_name="node_exporter"
    service_file="/etc/systemd/system/${service_name}.service"
    msg "Creating service file for ${service_name}"
    cat << EOF > "${service_file}"
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

    chmod 644 "${service_file}"
    sudo systemctl daemon-reload
    sudo systemctl enable "${service_name}"
    sudo systemctl start "${service_name}"
    sudo systemctl status "${service_name}" --no-pager
}

install_p4prometheus () {

    PVER="$VER_P4PROMETHEUS"
    progname="p4prometheus"
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

cat << EOF > "$p4prom_config_file"
# ----------------------
# sdp_instance: SDP instance - typically integer, but can be
# See: https://swarm.workshop.perforce.com/projects/perforce-software-sdp for more
# If this value is blank then it is assumed to be a non-SDP instance.
sdp_instance:   $SDP_INSTANCE

# ----------------------
# log_path: Path to p4d server log - REQUIRED!
#   Recommended to set an absolute path, e.g. /p4/1/logs/log
log_path:       $P4LOG

# ----------------------
# metrics_output: Name of output file to write for processing by node_exporter.
# Ensure that node_exporter user has read access to this folder.
metrics_output: $metrics_root/p4_cmds.prom

# ----------------------
# server_id: Optional - serverid for metrics.
# If SDP install then it will read /p4/<sdp_instance>/root/server.id for 
# If non-SDP install, set this field or set server_id_path (this field has preference!)
server_id:      $P4SERVERID

# ----------------------
# server_id_path: Optional - path to server.id file for metrics - only used if non-SDP install.
# If non-SDP install, set either this field, or server_id instead.
# e.g. server_id_path: /opt/perforce/server/root/server.id
server_id_path:      

# ----------------------
# output_cmds_by_user: true/false - Whether to output metrics p4_cmd_user_counter/p4_cmd_user_cumulative_seconds
# Normally this should be set to true as the metrics are useful.
# If you have a p4d instance with thousands of users you may find the number
# of metrics labels is too great (one per distinct user), so set this to false.
output_cmds_by_user: true

# ----------------------
# case_sensitive_server: true/false - if output_cmds_by_user=true then if this value is set to false
# all userids will be written in lowercase - otherwise as they occur in the log file
# If not present, this value will default to true on Windows and false otherwise.
case_sensitive_server: true

# ----------------------
# output_cmds_by_ip: true/false - Whether to output metrics p4_cmd_ip_counter/p4_cmd_ip_cumulative_seconds
# Like output_cmds_by_user this can be an issue for larger sites so defaults to false.
output_cmds_by_ip: false

# ----------------------
# output_cmds_by_user_regex: Specifies a Go regex for users for whom to output
# metrics p4_cmd_user_detail_counter (tracks cmd counts per user/per cmd) and
# p4_cmd_user_detail_cumulative_seconds
# 
# This can be set to values such as: "" (no users), ".*" (all users), or "swarm|jenkins"
# for just those 2 users. The latter is likely to be appropriate in many sites (keep an eye
# on automation users only, without generating thousands of labels for all users)
output_cmds_by_user_regex: ""

# ----------------------
# fail_on_missing_logfile: Due to timing log file might not be there - just wait.
fail_on_missing_logfile: false

EOF

    chown "$OSUSER:$OSGROUP" "$p4prom_config_file"

    service_name="p4prometheus"
    service_file="/etc/systemd/system/${service_name}.service"
    msg "Creating service file for ${service_name}"
    cat << EOF > "${service_file}"
[Unit]
Description=P4prometheus
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Wants=network-online.target
After=network-online.target

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
ExecStart=${local_bin_dir}/p4prometheus --config=$p4prom_config_file

[Install]
WantedBy=multi-user.target
EOF

    chmod 644 "${service_file}"
    systemctl daemon-reload
    systemctl enable "${service_name}"
    systemctl start "${service_name}"
    systemctl status "${service_name}" --no-pager

}


install_p4metrics () {

    PVER="$VER_P4PROMETHEUS"
    progname="p4metrics"
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

cat << EOF > "$p4metrics_config_file"
# ----------------------
# metrics_root: REQUIRED! Directory into which to write metrics files for processing by node_exporter.
# Ensure that node_exporter user has read access to this folder.
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
# p4bin: The value of the p4 binary to be used - important if not available in your PATH
# IGNORED if sdp_instance is non-blank!
p4bin:      p4

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

EOF

    chown "$OSUSER:$OSGROUP" "$p4metrics_config_file"

    service_name="${progname}"
    service_file="/etc/systemd/system/${service_name}.service"
    msg "Creating service file for ${service_name}"
    cat << EOF > "${service_file}"
[Unit]
Description=P4prometheus
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Wants=network-online.target
After=network-online.target

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
    systemctl start "${service_name}"
    systemctl status "${service_name}" --no-pager

}

install_monitor_locks () {

    if [[ $UseSDP -eq 1 ]]; then
        service_args="$SDP_INSTANCE"
    else
        service_args="-p $P4PORT -u $P4USER -nosdp -m $metrics_root"
    fi

    # We install in /p4/common/site/bin but need to reference the ultimate path without links for SELinux/systemd
    bin_dir="/p4/common/site/bin"
    abs_bin_dir=$(readlink -f "$bin_dir")
    cd "$bin_dir" || bail "Failed to cd to $bin_dir"
    for scriptname in monitor_metrics.py monitor_wrapper.sh; do
        [[ -f "$scriptname" ]] && rm "$scriptname"
        echo "downloading $scriptname"
        wget "https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/$scriptname"
        chmod 755 "$scriptname"
        chown "$OSUSER:$OSGROUP" "$scriptname"
        if [[ $SELinuxEnabled -eq 1 ]]; then
            semanage fcontext -a -t bin_t "$abs_bin_dir/$scriptname"
            restorecon -vF "$abs_bin_dir/$scriptname"
        fi
    done

    service_name="monitor_locks"
    service_file="/etc/systemd/system/${service_name}.service"
    msg "Creating service file for ${service_name}"
    cat << EOF > "${service_file}"
# monitor_locks.service
# Service file to run p4prometheus monitor_wrapper.sh - ensuring single threading

[Unit]
Description=p4prometheus monitor_wrapper.sh for p4d lock monitoring
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Wants=monitor_locks.timer network-online.target
After=network-online.target

[Service]
User=$OSUSER
Type=oneshot
ExecStart=${abs_bin_dir}/monitor_wrapper.sh ${service_args}

[Install]
WantedBy=multi-user.target
EOF

    chmod 644 "${service_file}"

    msg "Creating timer file for ${service_name}"
    service_file="/etc/systemd/system/${service_name}.timer"
    cat << EOF > "${service_file}"
# monitor_locks.timer
# Timer for service to run p4prometheus monitor_locks.sh - ensuring single threading

[Unit]
Description=p4prometheus monitor_locks.sh for p4d metrics gathering
Documentation=https://github.com/perforce/p4prometheus/blob/master/README.md
Requires=monitor_locks.service

[Timer]
Unit=monitor_locks.service
# Runs once a minute
OnCalendar=*-*-* *:*:00
AccuracySec=5s

[Install]
WantedBy=timers.target
EOF

    chmod 644 "${service_file}"

    systemctl daemon-reload
    for svc in monitor_locks; do
        systemctl enable $svc.timer
        systemctl start $svc.timer
        systemctl status $svc.timer --no-pager
    done

    mon_installer="/tmp/_install_mon.sh"
    cat << EOF > $mon_installer
# Download latest versions
mkdir -p $p4prom_bin_dir
cd $p4prom_bin_dir
for scriptname in report_instance_data.sh check_for_updates.sh; do
    [[ -f "\$scriptname" ]] && rm "\$scriptname"
    echo "downloading \$scriptname"
    wget "https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/\$scriptname"
    chmod +x "\$scriptname"
    chown "$OSUSER:$OSGROUP" "\$scriptname"
done
EOF

    chmod 755 "$mon_installer"
    sudo -u "$OSUSER" bash "$mon_installer"

    if [[ $InstallPushgateway -eq 0 ]]; then
        return
    fi

    config_file="$p4prom_config_dir/.push_metrics.cfg"
    cat << EOF > "$config_file"
# Set these values as appropriate according to HRA Procedures document
metrics_host=https://monitor.hra.p4demo.com:9091
metrics_user=customerid_CHANGEME
metrics_passwd=MySecurePassword_CHANGEME
metrics_job=pushgateway
metrics_instance=customerid-prod-hra_CHANGEME
metrics_customer=customerid_CHANGEME
# Modify the value below when everything above is ready - avoids getting bad metrics
enabled=0
EOF

    chown "$OSUSER:$OSGROUP" "$config_file"
    push_installer="/tmp/_install_push.sh"
    cat << EOF > $push_installer
scriptname="push_metrics.sh"
if ! crontab -l | grep -q "\$scriptname" ;then
    entry1="*/1 * * * * $p4prom_bin_dir/\$scriptname -c $config_file > /dev/null 2>&1 ||:"
    (crontab -l && echo "\$entry1") | crontab -
fi

scriptname="report_instance_data.sh"
if ! crontab -l | grep -q "\$scriptname" ;then
    entry1="0 23 * * * $p4prom_bin_dir/\$scriptname -c $config_file > /dev/null 2>&1 ||:"
    (crontab -l && echo "\$entry1") | crontab -
fi

# List things out for review
echo "Crontab after updating - showing push_metrics entries:"
crontab -l | grep -E "/push_metrics|/report_instance"

echo ""
echo "===================================="
echo "Please update values in $config_file"
echo "===================================="

EOF

    chmod 755 "$push_installer"
    sudo -u "$OSUSER" bash "$push_installer"

}

install_node_exporter
install_p4prometheus
install_p4metrics
install_monitor_locks
systemctl list-timers

echo "

Should have installed node_exporter, p4prometheus and friends.

To review further, please:

    ls -al $metrics_link/

    curl localhost:9100/metrics | grep ^p4_
"
