#!/bin/bash
# Installs the following: p4prometheus, node_exporter and monitor_metrics.sh
#
# First version assumes SDP environment.
#

if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# ============================================================
# Configuration section

# This might also be /hxlogs/metrics or passed as a parameter (with -m flag)
metrics_root=/hxlogs/metrics
metrics_link=/p4/metrics

VER_NODE_EXPORTER="1.1.2"
VER_P4PROMETHEUS="0.7.3"

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage
{
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for install_p4mon.sh:
 
install_p4mon.sh <instance> [-m <metrics_dir>] [-d <data_file>]
 
   or
 
monitor_metrics.sh -h
"
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i UseSDP=1

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h;;
        # (-man) usage -man;;
        (-p) Port=$2; shiftArgs=1;;
        (-u) User=$2; shiftArgs=1;;
        (-m) metrics_root=$2; shiftArgs=1;;
        (-d) data_file=$2; shiftArgs=1;;
        (-nosdp) UseSDP=0;;
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

# Find OSGROUP for ownership permissions - group of /p4 dir itself
OSGROUP=$(ls -al /p4/ | grep -E '\.$' | head -1 | awk '{print $4}')

# [[ -d "$metrics_root" ]] || bail "Specified metrics directory '$metrics_root' does not exist!"

if [[ $UseSDP -eq 1 ]]; then
    SDP_INSTANCE=${SDP_INSTANCE:-Unset}
    SDP_INSTANCE=${1:-$SDP_INSTANCE}
    if [[ $SDP_INSTANCE == Unset ]]; then
        echo -e "\\nError: Instance parameter not supplied.\\n"
        echo "You must supply the Perforce SDP instance as a parameter to this script."
        exit 1
    fi

    # Load SDP controlled shell environment.
    # shellcheck disable=SC1091
    source /p4/common/bin/p4_vars "$SDP_INSTANCE" ||\
    { echo -e "\\nError: Failed to load SDP environment.\\n"; exit 1; }

    p4="$P4BIN -u $P4USER -p $P4PORT"
    $p4 info -s || bail "Can't connect to P4PORT: $P4PORT"
else
    echo -e "\\nError: only SDP installs supported.\\n"
    exit 1
fi


install_node_exporter () {

    userid="node_exporter"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
    fi

    cd /tmp
    PVER="$VER_NODE_EXPORTER"
    curl -k -s -O https://github.com/prometheus/node_exporter/releases/download/v$PVER/node_exporter-$PVER.linux-amd64.tar.gz

    tar xvf node_exporter-$PVER.linux-amd64.tar.gz 
    mv node_exporter-$PVER.linux-amd64/node_exporter /usr/local/bin/

    mkdir "$metrics_root"
    chown "$OSUSER:$OSGROUP" "$metrics_root"
    chmod 755 "$metrics_root"
    # Assume only 2 levels deep - TODO make generic
    chmod 755 $(dirname "$metrics_root")

    ln -s "$metrics_root" "$metrics_link"
    chown -h "$OSUSER:$OSGROUP" "$metrics_link"

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
        --collector.systemd.unit-include="(p4.*|node_exporter)\.service" \
        --collector.textfile.directory=$metrics_root

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable node_exporter
    sudo systemctl start node_exporter
    sudo systemctl status node_exporter
}

install_p4prometheus () {

    PVER="$VER_P4PROMETHEUS"
    curl -k -s -O https://github.com/perforce/p4prometheus/releases/download/v$PVER/p4prometheus.linux-amd64.gz

    gunzip p4prometheus.linux-amd64.gz
    
    chmod +x p4prometheus.linux-amd64

    mv p4prometheus.linux-amd64 /usr/local/bin/p4prometheus

cat << EOF > /p4/common/config/p4prometheus.yaml
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

    chown "$OSUSER:$OSGROUP" /p4/common/config/p4prometheus.yaml

    cat << EOF > /etc/systemd/system/p4prometheus.service
[Unit]
Description=P4prometheus
Wants=network-online.target
After=network-online.target

[Service]
User=$OSUSER
Group=$OSGROUP
Type=simple
ExecStart=/usr/local/bin/p4prometheus --config=/p4/common/config/p4prometheus.yaml

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable p4prometheus
    systemctl start p4prometheus
    systemctl status p4prometheus

}

install_monitor_metrics () {

    su $OSUSER <<'EOF'
    # Download latest versions
    mkdir -p /p4/common/site/bin
    cd /p4/common/site/bin
    for fname in monitor_metrics.sh monitor_metrics.py monitor_wrapper.sh; do
        [[ -f "$fname" ]] && rm "$fname"
        curl -k -s -O "https://raw.githubusercontent.com/perforce/p4prometheus/master/demo/$fname"
        chmod +x "$fname"
        chown "$OSUSER:$OSGROUP" "$fname"
    done

    # Install in crontab if required
    fname="monitor_metrics.sh"
    if ! crontab -l | grep -q "$fname" ;then
        entry1="*/1 * * * * /p4/common/site/bin/$fname $SDP_INSTANCE > /dev/null 2>&1 ||:"
        (crontab -l && echo "$entry1") | crontab -
    fi
    fname="monitor_wrapper.sh"
    if ! crontab -l | grep -q "$fname" ;then
        entry1="*/1 * * * * /p4/common/site/bin/$fname $SDP_INSTANCE > /dev/null 2>&1 ||:"
        (crontab -l && echo "$entry1") | crontab -
    fi
    # List things out for review
    echo "Crontab after updating - showing monitor entries:"
    crontab -l | grep /monitor_
EOF

}

install_node_exporter
install_p4prometheus
install_monitor_metrics

echo "

Should have installed node_exporter, p4prometheus and friends.
Check crontab -l output above (as user perforce)

To review further, please:

    ls -al $metrics_link/

    curl localhost:9100/metrics | grep -E ^p4_
"
