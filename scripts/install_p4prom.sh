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
metrics_link=/p4/metrics

VER_NODE_EXPORTER="1.3.1"
VER_P4PROMETHEUS="0.7.5"

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
 
   echo "USAGE for install_p4prom.sh:
 
install_p4prom.sh [<instance> | -nosdp] [-m <metrics_root>] [-l <metrics_link>] [-u <osuser>] [-push]
 
   or

install_p4prom.sh -h

    -push     Means install pushgateway cronjob and config file.
    <metrics_root> is the directory where metrics will be written - default: $metrics_root
    <metrics_link> is an alternative link to metrics_root where metrics will be written - default: $metrics_link
                Typically only used for SDP installations.
    <osuser>    Operating system user, e.g. perforce, under which p4d process is running

Specify either the SDP instance (e.g. 1), or -nosdp

WARNING: If using -nosdp, then please ensure P4PORT and P4USER are appropriately set and that you can connect
    to your server (e.g. you have done a 'p4 trust' if required, and logged in already)

Examples:

./install_p4prom.sh 1
./install_p4prom.sh -nosdp -m /p4metrics -u perforce

"
   if [[ "$style" == -man ]]; then
       # Add full manual page documentation here.
      true
   fi

   exit 2
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i UseSDP=1
declare -i SELinuxEnabled=0
declare -i InstallPushgateway=0
declare OsUser=""
declare P4LOG=""

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h;;
        (-man) usage -man;;
        (-m) metrics_root=$2; shiftArgs=1;;
        (-u) OsUser="$2"; shiftArgs=1;;
        (-push) InstallPushgateway=1;;
        (-nosdp) UseSDP=0;;
        (-l) P4LOG="$2"; shiftArgs=1;;
        (-*) usage -h "Unknown command line option ($1).";;
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

[[ -n "$(command -v wget)" ]] || bail "Failed to find wget in PATH."

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi

# [[ -d "$metrics_root" ]] || bail "Specified metrics directory '$metrics_root' does not exist!"

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

    # shellcheck disable=SC2153
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
    p4prom_config_dir="/etc/p4prometheus"
    p4prom_bin_dir="$p4prom_config_dir"
fi

p4prom_config_file="$p4prom_config_dir/p4prometheus.yaml"

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
    fname="node_exporter-$PVER.linux-amd64.tar.gz"
    download_and_untar "$fname" "https://github.com/prometheus/node_exporter/releases/download/v$PVER/$fname"

    tar xvf node_exporter-$PVER.linux-amd64.tar.gz 
    msg "Installing node_exporter"
    mv node_exporter-$PVER.linux-amd64/node_exporter /usr/local/bin/

    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=/usr/local/bin/node_exporter
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
ExecStart=/usr/local/bin/node_exporter --collector.systemd \
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

install_p4prometheus () {

    PVER="$VER_P4PROMETHEUS"
    fname="p4prometheus.linux-amd64.gz"
    url="https://github.com/perforce/p4prometheus/releases/download/v$PVER/$fname"
    msg "downloading and extracting $url"
    wget -q "$url"

    gunzip "$fname"
    
    chmod +x p4prometheus.linux-amd64

    mv p4prometheus.linux-amd64 /usr/local/bin/p4prometheus

    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=/usr/local/bin/p4prometheus
        semanage fcontext -a -t bin_t $bin_file
        restorecon -vF $bin_file
    fi

cat << EOF > $p4prom_config_file
# ----------------------
# sdp_instance: SDP instance - typically integer, but can be
# See: https://swarm.workshop.perforce.com/projects/perforce-software-sdp for more
# If this value is blank then it is assumed to be a non-SDP instance.
sdp_instance:   $SDP_INSTANCE

# ----------------------
# log_path: Path to p4d server log - REQUIRED!
log_path:       $P4LOG

# ----------------------
# metrics_output: Name of output file to write for processing by node_exporter.
# Ensure that node_exporter user has read access to this folder.
metrics_output: $metrics_root/p4_cmds.prom

# ----------------------
# server_id: Optional - serverid for metrics - typically read from /p4/<sdp_instance>/root/server.id for 
# SDP installations - please specify a value if non-SDP install
server_id:      

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

    msg "Creating service file for p4prometheus"
    cat << EOF > /etc/systemd/system/p4prometheus.service
[Unit]
Description=P4prometheus
Wants=network-online.target
After=network-online.target

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
ExecStart=/usr/local/bin/p4prometheus --config=$p4prom_config_file

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable p4prometheus
    systemctl start p4prometheus
    systemctl status p4prometheus --no-pager

}

install_monitor_metrics () {

    mon_installer="/tmp/_install_mon.sh"
    cat << EOF > $mon_installer
# Download latest versions
mkdir -p $p4prom_bin_dir
cd $p4prom_bin_dir
for scriptname in monitor_metrics.sh monitor_metrics.py monitor_wrapper.sh push_metrics.sh check_for_updates.sh; do
    [[ -f "\$scriptname" ]] && rm "\$scriptname"
    echo "downloading \$scriptname"
    wget "https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/\$scriptname"
    chmod +x "\$scriptname"
    chown "$OSUSER:$OSGROUP" "\$scriptname"
done

# Install in crontab if required
scriptname="monitor_metrics.sh"
if ! crontab -l | grep -q "\$scriptname" ;then
    entry1="*/1 * * * * $p4prom_bin_dir/\$scriptname $SDP_INSTANCE > /dev/null 2>&1 ||:"
    (crontab -l && echo "\$entry1") | crontab -
fi
scriptname="monitor_wrapper.sh"
if ! crontab -l | grep -q "\$scriptname" ;then
    entry1="*/1 * * * * $p4prom_bin_dir/\$scriptname $SDP_INSTANCE > /dev/null 2>&1 ||:"
    (crontab -l && echo "\$entry1") | crontab -
fi

EOF

    if [[ $InstallPushgateway -eq 1 ]]; then
        cat << EOF >> "$mon_installer"
scriptname="push_metrics.sh"
if ! crontab -l | grep -q "\$scriptname" ;then
    entry1="*/1 * * * * $p4prom_bin_dir/\$scriptname -c $p4prom_config_dir/.push_metrics.cfg > /dev/null 2>&1 ||:"
    (crontab -l && echo "\$entry1") | crontab -
fi
EOF
    fi

    cat << EOF >> "$mon_installer"
# List things out for review
echo "Crontab after updating - showing monitor entries:"
crontab -l | grep /monitor_

EOF

    chmod 755 "$mon_installer"
    sudo -u "$OSUSER" bash "$mon_installer"

    if [[ $InstallPushgateway -eq 0 ]]; then
        return
    fi

    config_file="$p4prom_config_dir/.push_metrics.cfg"
    cat << EOF > $config_file
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
# List things out for review
echo "Crontab after updating - showing push_metrics entries:"
crontab -l | grep /push_metrics

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
install_monitor_metrics

echo "

Should have installed node_exporter, p4prometheus and friends.
Check crontab -l output above (as user $OSUSER)

To review further, please:

    ls -al $metrics_link/

    curl localhost:9100/metrics | grep -E ^p4_
"
