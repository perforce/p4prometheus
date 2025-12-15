#!/bin/bash
# Updates node_exporter and optionally replaces push_metrics cron job with vmagent service
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

# For SELinux compatibility, place vmagent config files here
vmagent_config_dir=/var/vmagent

local_bin_dir=/usr/local/bin

# Version to download
VER_NODE_EXPORTER="1.3.1"
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
 
   echo "USAGE for update_node.sh:
 
update_node.sh [-m <metrics_root>] [-vmagent]
 
   or

update_node.sh -h

    <metrics_root>  is the directory where metrics will be written - default: $metrics_root
    -vmagent        Install vmagent service to replace push_metrics cron job.
                    This will remove the push_metrics cron entry and install vmagent instead.

This expects to be run as root/sudo.

Examples:

./update_node.sh
./update_node.sh -vmagent
./update_node.sh -m /p4metrics
./update_node.sh -m /p4metrics -vmagent

"
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i SELinuxEnabled=0
declare -i InstallVMAgent=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h  && exit 1;;
        (-m) metrics_root=$2; shiftArgs=1;;
        (-vmagent) InstallVMAgent=1;;
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

# Check if local_bin_dir exists
if [[ ! -d "$local_bin_dir" ]]; then
    echo "Error: Directory $local_bin_dir does not exist. Please create it before running this script."
    exit 1
fi

wget=$(which wget)
[[ $? -eq 0 ]] || bail "Failed to find wget in path"

if command -v getenforce > /dev/null; then
    selinux=$(getenforce)
    [[ "$selinux" == "Enforcing" ]] && SELinuxEnabled=1
fi

[[ -d "$metrics_root" ]] || bail "Specified metrics directory '$metrics_root' does not exist - please create it!"

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "downloading and extracting $url"
    $wget -q "$url"
    tar zxvf "$fname"
}

update_node_exporter () {
    msg "Updating node_exporter..."

    # Check if service is running and stop it
    if check_service_exists node_exporter && systemctl is-active --quiet node_exporter; then
        msg "Stopping node_exporter service..."
        systemctl stop node_exporter
    fi

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
        semanage fcontext -a -t bin_t $bin_file 2>/dev/null || true
        restorecon -vF $bin_file
    fi

    mkdir -p "$metrics_root"
    chmod 755 "$metrics_root"
    f=$(readlink -f "$metrics_root")
    while [[ $f != / ]]; do chmod 755 "$f"; f=$(dirname "$f"); done;

    msg "Updating service file for node_exporter"
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

install_vmagent () {
    msg "Installing Victoria Metrics Agent..."
    
    # Check if already installed and stop if running
    if check_service_exists vmagent && systemctl is-active --quiet vmagent; then
        msg "Stopping existing vmagent service..."
        systemctl stop vmagent
    fi

    userid="root"

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

    cat << EOF > /etc/systemd/system/vmagent.service
[Unit]
Description=Victoria Metrics Agent
Wants=network-online.target
After=network-online.target

[Service]
User=$userid
Type=simple
WorkingDirectory=$vmagent_config_dir
EnvironmentFile=$vmagent_config_dir/vmagent.env
ExecStart=/usr/local/bin/vmagent-prod \
  -promscrape.config=vmagent.yml \
  -remoteWrite.basicAuth.username=\${VM_CUSTOMER} \
  -remoteWrite.basicAuth.passwordFile=.vmpassword \
  -remoteWrite.urlRelabelConfig=relabelConfig.yml \
  -remoteWrite.url=\${VM_METRICS_HOST}/api/v1/write

[Install]
WantedBy=multi-user.target
EOF

    # Create vmagent configuration files by parsing .push_metrics.cfg
    create_vmagent_configs
    
    # Remove push_metrics cron job if it exists
    remove_push_metrics_cron
    
    systemctl daemon-reload
    systemctl enable vmagent
    # Don't start service yet - prompt user to do so at the end after verifying config files created!
}

create_vmagent_configs() {
    local config_file="$metrics_bin_dir/.push_metrics.cfg"
    
    if [[ ! -f "$config_file" ]]; then
        msg "Warning: Config file $config_file not found, skipping vmagent config creation"
        msg "You will need to manually create the following files:"
        msg "  - $vmagent_config_dir/vmagent.env"
        msg "  - $vmagent_config_dir/relabelConfig.yml"
        msg "  - $vmagent_config_dir/vmagent.yml"
        msg "  - $vmagent_config_dir/.vmpassword"
        return
    fi
    
    # Parse the push_metrics.cfg file
    # shellcheck disable=SC1090
    source "$config_file"
    
    # Extract customer and instance from metrics values
    local customer="${metrics_customer:-}"
    local instance="${metrics_instance:-}"
    local host="${metrics_host:-}"
    local password="${metrics_passwd:-}"
    
    if [[ -z "$customer" || -z "$instance" || -z "$host" ]]; then
        msg "Warning: Required metrics values not found in $config_file"
        return
    fi
    
    # Convert pushgateway port (9091) to vmagent port (9092)
    local vm_host="${host/:9091/:9092}"
    
    mkdir -p "$vmagent_config_dir"
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
    
    chmod 644 "$relabel_config_file"
    msg "Created vmagent relabel config: $relabel_config_file"
    
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
    
    chmod 644 "$vmagent_config_file"
    msg "Created vmagent config: $vmagent_config_file"
    
    # Create .vmpassword file from metrics_passwd
    if [[ -n "$password" ]]; then
        local vmpassword_file="$vmagent_config_dir/.vmpassword"
        echo "$password" > "$vmpassword_file"
        chmod 600 "$vmpassword_file"
        msg "Created vmagent password file: $vmpassword_file"
    else
        msg "Warning: metrics_passwd not found in $config_file"
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

remove_push_metrics_cron() {
    msg "Removing push_metrics cron job if it exists..."
    
    # Check if cron job exists
    if crontab -l 2>/dev/null | grep -q "push_metrics.sh"; then
        # Create temp file with crontab minus push_metrics entries
        TEMP_FILE=$(mktemp)
        crontab -l 2>/dev/null | grep -v "push_metrics.sh" > "$TEMP_FILE"
        crontab "$TEMP_FILE"
        rm -f "$TEMP_FILE"
        msg "Removed push_metrics.sh from crontab"
    else
        msg "No push_metrics.sh cron job found"
    fi
}

# Main execution
update_node_exporter

if [[ $InstallVMAgent -eq 1 ]]; then
    install_vmagent
    remove_push_metrics_cron
fi

echo "

Have updated node_exporter.
"

if [[ $InstallVMAgent -eq 1 ]]; then
    echo "vmagent has been installed and configured.

The push_metrics.sh cron job has been removed.

Please verify the configuration files in $vmagent_config_dir and then start the service:

    systemctl start vmagent
    systemctl status vmagent --no-pager

To check vmagent is working:
    journalctl -u vmagent -f
"
fi

echo "
To review further, please:

    ls -al $metrics_root/

    curl localhost:9100/metrics
"
