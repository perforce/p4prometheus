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
VER_P4PROMETHEUS="0.9.8"

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
 
   echo "USAGE for update_p4prom.sh:
 
update_p4prom.sh [<instance> | -nosdp] [-m <metrics_root>] [-l <metrics_link>] [-u <osuser>]
 
   or

update_p4prom.sh -h

    <metrics_root> is the directory where metrics will be written - default: $metrics_root
    <metrics_link> is an alternative link to metrics_root where metrics will be written - default: $metrics_link
                Typically only used for SDP installations.
    <osuser>    Operating system user, e.g. perforce, under which p4d process is running

Specify either the SDP instance (e.g. 1), or -nosdp

WARNING: If using -nosdp, then please ensure P4PORT and P4USER are appropriately set and that you can connect
    to your server (e.g. you have done a 'p4 trust' if required, and logged in already)

Examples:

./update_p4prom.sh 1
./update_p4prom.sh -nosdp -m /p4metrics -u perforce

"
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i UseSDP=1
declare -i SELinuxEnabled=0
declare OsUser=""
declare P4LOG=""

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h  && exit 1;;
        # (-man) usage -man;;
        (-m) metrics_root=$2; shiftArgs=1;;
        (-u) OsUser="$2"; shiftArgs=1;;
        (-nosdp) UseSDP=0;;
        (-l) P4LOG="$2"; shiftArgs=1;;
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

[[ -d "$metrics_root" ]] || bail "Specified metrics directory '$metrics_root' does not exist!"

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
    p4prom_config_dir="/etc/p4prometheus"
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
    systemctl start ${service_name}
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
    systemctl start "${service_name}"
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

update_node_exporter
update_p4prometheus
update_p4metrics
# update_monitor_locks
systemctl list-timers | grep -E "^NEXT|monitor"

echo "

Should have updated node_exporter and p4prometheus.
"
