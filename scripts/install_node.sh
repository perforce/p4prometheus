#!/bin/bash
# Installs the following: node_exporter, and optionally the push gateway cron job
#
# Intended for use on servers not running p4d, e.g. running swarm/hansoft/hth/h4g
#

# shellcheck disable=SC2128
if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# ============================================================
# Configuration section

# This might also be passed as a parameter (with -m flag)
metrics_root=/var/metrics

metrics_bin_dir=/etc/metrics

# Version to download
VER_NODE_EXPORTER="1.8.0"

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
 
   echo "USAGE for install_node.sh:
 
install_node.sh [-m <metrics_root>] [-push]
 
   or

install_node.sh -h

    <metrics_root>  is the directory where metrics will be written - default: $metrics_root
    -push           Means install pushgateway cronjob and config file.

This expects to be run as root/sudo. If -push is specified then a crontab entry will be written.

Examples:

./install_node.sh
./install_node.sh -m /p4metrics
./install_node.sh -m /p4metrics -push

"
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i SELinuxEnabled=0
declare -i InstallPushgateway=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h  && exit 1;;
        # (-man) usage -man;;
        (-m) metrics_root=$2; shiftArgs=1;;
        (-push) InstallPushgateway=1;;
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

wget=$(which wget)
[[ $? -eq 0 ]] || bail "Failed to find wget in path"

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "downloading and extracting $url"
    $wget -q "$url"
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
    mv node_exporter-$PVER.linux-${arch}/node_exporter /usr/local/bin/

    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=/usr/local/bin/node_exporter
        semanage fcontext -a -t bin_t $bin_file
        restorecon -vF $bin_file
    fi

    mkdir -p "$metrics_root"
    chmod 755 "$metrics_root"
    f=$(readlink -f "$metrics_root")
    while [[ $f != / ]]; do chmod 755 "$f"; f=$(dirname "$f"); done;

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
  --collector.systemd.unit-include=node_exporter.service \
  --collector.textfile.directory=$metrics_root

[Install]
WantedBy=multi-user.target
EOF

    sudo systemctl daemon-reload
    sudo systemctl enable node_exporter
    sudo systemctl start node_exporter
    sudo systemctl status node_exporter --no-pager
}


install_push_gateway () {

    mon_installer="/tmp/_install_mon.sh"
    cat << EOF > $mon_installer
# Download latest versions
mkdir -p $metrics_bin_dir
cd $metrics_bin_dir
for scriptname in push_metrics.sh check_for_updates.sh report_instance_data.sh; do
    [[ -f "\$scriptname" ]] && rm "\$scriptname"
    echo "downloading \$scriptname"
    wget "https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/\$scriptname"
    chmod +x "\$scriptname"
done
EOF

    chmod 755 "$mon_installer"
    sudo bash "$mon_installer"

    if [[ $InstallPushgateway -eq 0 ]]; then
        return
    fi

    config_file="$metrics_bin_dir/.push_metrics.cfg"
    cat << EOF > $config_file
# Set these values as appropriate according to HRA Procedures document
metrics_host=https://monitor.hra.p4demo.com:9091
metrics_user=customerid_CHANGEME
metrics_passwd=MySecurePassword_CHANGEME
metrics_job=pushgateway
metrics_instance=customerid-prod-hra_CHANGEME
metrics_customer=customerid_CHANGEME
metrics_logfile=$metrics_root/push_metrics.log
report_instance_logfile=$metrics_root/report_instance.log
# Modify the value below when everything above is ready - avoids getting bad metrics
enabled=0
EOF

    push_installer="/tmp/_install_push.sh"
    cat << EOF > $push_installer
if ! crontab -l ;then
    echo "" | crontab
fi

scriptname="push_metrics.sh"
if ! crontab -l | grep -q "\$scriptname" ;then
    entry1="*/1 * * * * $metrics_bin_dir/\$scriptname -c $config_file > /dev/null 2>&1 ||:"
    (crontab -l && echo "\$entry1") | crontab -
fi

scriptname="report_instance_data.sh"
if ! crontab -l | grep -q "\$scriptname" ;then
    entry1="0 23 * * * $metrics_bin_dir/\$scriptname -c $config_file > /dev/null 2>&1 ||:"
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
    bash "$push_installer"

}

install_node_exporter
install_push_gateway

echo "

Should have installed node_exporter and friends.
Check crontab -l output above (as root user) if you installed push gateway

To review further, please:

    ls -al $metrics_root/

    curl localhost:9100/metrics
"
