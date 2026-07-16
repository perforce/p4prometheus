#!/bin/bash
# Updates the following: p4prometheus, node_exporter.
#
# Reads install state from p4prom_install.env (written by install_p4prom.sh)
# so previously-chosen paths are preserved automatically across upgrades.
# Automatically migrates config files from /p4/common/config/ to
# /p4/common/site/config/ for SDP installs upgrading from older versions.
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
# Air-gap: set to a directory of pre-staged release tarballs to skip downloads
local_tarballs_dir=""


VER_NODE_EXPORTER="1.3.1"
VER_P4PROMETHEUS="0.11.1"
VER_VICTORIA_METRICS="1.131.0"

# Default to amd but allow arm architecture
arch="amd64"
[[ $(uname -p) == 'aarch64' ]] && arch="arm64"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMMON_LIB="${SCRIPT_DIR}/p4prom_common.sh"
if [[ ! -f "$COMMON_LIB" ]]; then
    COMMON_LIB_URL="https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/p4prom_common.sh"
    echo "Common library missing: $COMMON_LIB"
    echo "Attempting download from $COMMON_LIB_URL"
    if command -v wget >/dev/null 2>&1; then
        wget -q -O "$COMMON_LIB" "$COMMON_LIB_URL" || {
            echo "Error: Failed to download common library with wget"
            exit 1
        }
    elif command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$COMMON_LIB" "$COMMON_LIB_URL" || {
            echo "Error: Failed to download common library with curl"
            exit 1
        }
    else
        echo "Error: Missing common library and neither wget nor curl is available"
        exit 1
    fi
fi
# shellcheck source=p4prom_common.sh
source "$COMMON_LIB" || { echo "Error: Failed to source common library $COMMON_LIB"; exit 1; }

# ============================================================

function usage
{
   declare errorMessage=${2:-Unset}

   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi

   echo "USAGE for update_p4prom.sh:

update_p4prom.sh [<instance> | -nosdp] [-m <metrics_root>] [-l <metrics_link>]
                 [-u <osuser>] [-c <p4prom_config_dir>] [-b <bin_dir>]
                 [--local-tarballs-dir <path>]

   or

update_p4prom.sh -h

    <metrics_root>        Directory where metrics are written - default: $metrics_root
    <metrics_link>        Symlink to metrics_root (SDP installs only) - default: $metrics_link
    <osuser>              OS user running p4d (e.g. perforce)
    <p4prom_config_dir>   Config file directory (non-SDP installs) - default: /etc/p4prometheus
    -b <bin_dir>          Binary installation directory - default: $local_bin_dir
    --local-tarballs-dir  Directory of pre-staged release tarballs (air-gap installs).
                          Skips all downloads; file names must match GitHub release assets.
    -vmagent              Install vmagent to replace legacy push_metrics cron jobs.
                          Not relevant for most installations - intended for P4RA only.

Specify either the SDP instance (e.g. 1), or -nosdp

WARNING: If using -nosdp, ensure P4PORT and P4USER are set and you can connect
    (e.g. 'p4 trust' done, already logged in)

Note: If a p4prom_install.env state file exists from a prior install_p4prom.sh
    run, paths are loaded from it automatically. CLI flags always override.

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
# Track whether CLI explicitly overrode path defaults (to avoid state file clobbering)
declare -i cli_bin_dir_set=0
declare -i cli_config_dir_set=0

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
        (-c) p4prom_config_dir="$2"; cli_config_dir_set=1; shiftArgs=1;;
        (-b) local_bin_dir="$2"; cli_bin_dir_set=1; shiftArgs=1;;
        (--local-tarballs-dir) local_tarballs_dir="$2"; shiftArgs=1;;
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

    # New default: site/config (SDP-upgrade safe). Will migrate from old location if needed.
    [[ $cli_config_dir_set -eq 0 ]] && p4prom_config_dir="/p4/common/site/config"
    p4prom_bin_dir="/p4/common/site/bin"

    # Load state file to restore previously-chosen paths (CLI flags already override defaults)
    local_state_file="${p4prom_config_dir}/p4prom_install.env"
    legacy_state_file="/p4/common/config/p4prom_install.env"
    for sf in "$local_state_file" "$legacy_state_file"; do
        if [[ -f "$sf" ]]; then
            msg "Loading install state from: $sf"
            saved_bin=$(grep '^LOCAL_BIN_DIR=' "$sf" 2>/dev/null | cut -d= -f2)
            [[ $cli_bin_dir_set -eq 0 && -n "$saved_bin" ]] && local_bin_dir="$saved_bin"
            break
        fi
    done

    # Automatically migrate config files from /p4/common/config/ to site/config/
    # for installs upgrading from versions before this feature was added.
    old_config_dir="/p4/common/config"
    migrate_count=0
    for conf in p4prometheus.yaml p4metrics.yaml monitor_metrics.yaml; do
        old_file="${old_config_dir}/${conf}"
        new_file="${p4prom_config_dir}/${conf}"
        if [[ -f "$old_file" ]] && [[ ! -f "$new_file" ]]; then
            mkdir -p "$p4prom_config_dir"
            cp "$old_file" "$new_file"
            chown "$OSUSER:$OSGROUP" "$new_file" 2>/dev/null || true
            # Annotate old file so operators know what happened
            printf '\n# NOTICE: This file was automatically copied to %s\n' "$new_file" >> "$old_file"
            printf '# by update_p4prom.sh on %s.\n' "$(date)" >> "$old_file"
            printf '# The active configuration is now at the location above.\n' >> "$old_file"
            msg "  Migrated: $old_file → $new_file"
            migrate_count=$(( migrate_count + 1 ))
        fi
    done
    if [[ $migrate_count -gt 0 ]]; then
        msg "Auto-migrated $migrate_count config file(s) to SDP-upgrade-safe location: $p4prom_config_dir"
        msg "Original files preserved at $old_config_dir with deprecation notices."
    fi
else
    SDP_INSTANCE=""
    p4port=${Port:-$P4PORT}
    p4user=${User:-$P4USER}
    OSUSER="$OsUser"
    OSGROUP=$(id -gn "$OSUSER")
    p4="p4 -u $p4user -p $p4port"
    $p4 info -s || bail "Can't connect to P4PORT: $p4port"
    [[ $cli_config_dir_set -eq 0 ]] && p4prom_config_dir=${p4prom_config_dir:-"/etc/p4prometheus"}
    p4prom_bin_dir="$p4prom_config_dir"

    # Load state file for non-SDP installs
    nosdp_state_file="${p4prom_config_dir}/p4prom_install.env"
    if [[ -f "$nosdp_state_file" ]]; then
        msg "Loading install state from: $nosdp_state_file"
        saved_bin=$(grep '^LOCAL_BIN_DIR=' "$nosdp_state_file" 2>/dev/null | cut -d= -f2)
        [[ $cli_bin_dir_set -eq 0 && -n "$saved_bin" ]] && local_bin_dir="$saved_bin"
        saved_metrics=$(grep '^METRICS_ROOT=' "$nosdp_state_file" 2>/dev/null | cut -d= -f2)
        [[ -n "$saved_metrics" ]] && metrics_root="$saved_metrics"
    fi
fi

p4prom_config_file="$p4prom_config_dir/p4prometheus.yaml"
p4metrics_config_file="$p4prom_config_dir/p4metrics.yaml"
monitor_metrics_config_file="$p4prom_config_dir/monitor_metrics.yaml"

[[ -f "$p4prom_config_file" ]] || bail "Config file '$p4prom_config_file' does not exist - please run install_p4prom.sh instead of this script!"

update_node_exporter () {

    userid="node_exporter"
    progname="node_exporter"
    if ! grep -q "^$userid:" /etc/passwd ;then
        useradd --no-create-home --shell /bin/false "$userid" || bail "Failed to create user"
        msg "Created user $userid"
    fi

    curr_ver=$(${progname} --version 2>&1 | grep ' version ' | awk '{print $3}')
    if [[ "$curr_ver" == "$VER_NODE_EXPORTER" ]]; then
        msg "Current version $curr_ver of node_exporter is up-to-date"
        return
    fi

    service_file="/etc/systemd/system/node_exporter.service"
    service_name="node_exporter"
    sudo systemctl stop "$service_name"

    cd /tmp || bail "Failed to cd to /tmp"
    PVER="$VER_NODE_EXPORTER"
    fname="${progname}-$PVER.linux-${arch}.tar.gz"
    [[ -d ${progname}-$PVER.linux-${arch} ]] && rm -rf ${progname}-$PVER.linux-${arch}
    download_and_untar "$fname" "https://github.com/prometheus/node_exporter/releases/download/v$PVER/$fname"

    msg "Installing node_exporter"
    mv ${progname}-$PVER.linux-${arch}/${progname} ${local_bin_dir}/

    if [[ $SELinuxEnabled -eq 1 ]]; then
        bin_file=${local_bin_dir}/${progname}
        semanage fcontext -a -t bin_t $bin_file
        restorecon -vF $bin_file
    fi

    ensure_metrics_root_and_link

    msg "Creating service file for ${service_name}"
    write_node_exporter_service_file "${service_file}" "$userid"
    systemd_enable_and_restart "${service_file}" "${service_name}"
}

update_p4prometheus () {
    service_name="p4prometheus"
    progname="p4prometheus"
    service_file="/etc/systemd/system/${service_name}.service"
    curr_ver=$($progname --version 2>&1 | grep "$progname, version " | awk '{print $3}')
    if [[ "$curr_ver" == "v$VER_P4PROMETHEUS" ]]; then
        if [[ -f "${service_file}" ]]; then
            msg "Updating existing service file for $service_name"
            write_p4prometheus_service_file "${service_file}"
            systemd_enable_and_restart "${service_file}" "${service_name}"
        fi
        msg "Current version $curr_ver of $progname is up-to-date"
    else
        systemctl stop $service_name

        cd /tmp || bail "Failed to cd to /tmp"
        PVER="$VER_P4PROMETHEUS"
        fname="${progname}.linux-${arch}.gz"
        url="https://github.com/perforce/p4prometheus/releases/download/v$PVER/$fname"
        [[ -e ${progname}.linux-${arch} ]] && rm -f ${progname}.linux-${arch}
        download_gz "$fname" "$url"

        gunzip "$fname"
        chmod +x ${progname}.linux-${arch}
        mv ${progname}.linux-${arch} ${local_bin_dir}/${progname}
        if [[ $SELinuxEnabled -eq 1 ]]; then
            bin_file=${local_bin_dir}/${progname}
            semanage fcontext -a -t bin_t $bin_file
            restorecon -vF $bin_file
        fi

        msg "Creating service file for $service_name"
        write_p4prometheus_service_file "${service_file}"
        systemd_enable_and_restart "${service_file}" "${service_name}"
    fi

    ensure_hms_wrapper_script p4prometheus "Refreshing"
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

        cd /tmp || bail "Failed to cd to /tmp"
        PVER="$VER_P4PROMETHEUS"
        fname="${progname}.linux-${arch}.gz"
        url="https://github.com/perforce/p4prometheus/releases/download/v$PVER/$fname"
        download_gz "$fname" "$url"

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

    write_or_update_p4metrics_config_file

    service_name="${progname}"
    service_file="/etc/systemd/system/${service_name}.service"
    msg "Creating service file for ${service_name}"
    write_p4metrics_service_file "${service_file}"

    systemd_enable_and_restart "${service_file}" "${service_name}"

    ensure_hms_wrapper_script p4metrics "Refreshing"

    comment_out_legacy_monitor_cron "$OSUSER"
}

update_monitor_locks_service() {
    local service_name="monitor_locks"
    local service_file="/etc/systemd/system/${service_name}.service"
    local updates_dir="/p4/common/site/bin"
    local venv_dir="${updates_dir}/.venv"
    local updates_script="${updates_dir}/check_for_updates.sh"
    if [[ ! -f "${service_file}" ]]; then
        return
    fi

    if ! grep -qE '^[[:space:]]*ExecStart=.*monitor_wrapper\.sh' "${service_file}"; then
        return
    fi

    if grep -qE '^[[:space:]]*ExecStart=.*monitor_wrapper\.sh.*[[:space:]]-c[[:space:]]' "${service_file}"; then
        msg "monitor_locks service already has a monitor_metrics config argument"
    else
        msg "Updating monitor_locks service to include monitor_metrics config argument"
        sed -i "/^[[:space:]]*ExecStart=.*monitor_wrapper\\.sh/ s|$| -c ${monitor_metrics_config_file}|" "${service_file}"
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
        bootstrap_monitor_python_env "$updates_dir"
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

update_node_exporter
update_p4prometheus
update_p4metrics
ensure_monitor_metrics_config_file_exists
update_monitor_locks_service
check_aws_cli_version
update_vmagent_service_if_present
if [[ $InstallVMAgent -eq 1 ]]; then
    install_vmagent
fi
# update_monitor_locks
systemctl list-timers | grep -E "^NEXT|monitor"

# Write updated state file so future upgrades stay on the same paths
p4d_state_file="${p4prom_config_dir}/p4prom_install.env"
write_p4d_state_file "$p4d_state_file"

echo "
======================================================================
Update complete.

Config dir:    ${p4prom_config_dir}
Binaries dir:  ${local_bin_dir}
Metrics dir:   ${metrics_root}

Install state saved to: ${p4d_state_file}
  (Future upgrades will use these paths automatically)
======================================================================"

if [[ $InstallVMAgent -eq 1 ]]; then
    echo "
Please start the vmagent service when you have checked its configuration:

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
Manual review recommended:

If p4metrics/monitor_metrics were installed or updated, please review these files:
    - $p4metrics_config_file
    - $monitor_metrics_config_file
"
fi
