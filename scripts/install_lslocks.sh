#!/bin/bash
# install_lslocks.sh
# Installs the following: monitor_metrics.py and its wrapper monitor_wrapper.sh
#
# Can be done for SDP or non-SDP.
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

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

function usage
{
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for install_lslocks.sh:

install_lslocks.sh [<instance> | -nosdp] [-m <metrics_root>] [-osuser <osuser>] 
        [-p <P4PORT>] [-u <p4user>]

   or

install_lslocks.sh -h

    <metrics_root>  is the directory where metrics will be written - default: $metrics_root
    <osuser>        Operating system user, e.g. perforce, under which p4d process is running and to install crontab
    <P4PORT>        P4PORT to use within any installed scripts
    <P4USER>        P4USER to use within any installed scripts

IMPORTANT: Specify either the SDP instance (e.g. 1), or -nosdp and other parameters

WARNING: If using -nosdp, then please ensure P4PORT and P4USER are provided or are appropriately set and that you can connect
    to your server (e.g. you have done a 'p4 trust' if required, and logged in already)

Examples:

./install_lslocks.sh 1
./install_lslocks.sh -nosdp -osuser perforce -m /p4metrics -p 1666 -u p4admin 

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

if [[ $UseSDP -eq 1 ]]; then
    SDP_INSTANCE=${SDP_INSTANCE:-Unset}
    SDP_INSTANCE=${1:-$SDP_INSTANCE}
    if [[ $SDP_INSTANCE == Unset ]]; then
        echo -e "\\nError: Instance parameter not supplied.\\n"
        echo "You must supply the Perforce SDP instance as a parameter to this script. E.g."
        echo "    install_lslocks.sh 1"
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
    config_dir="/p4/common/config"
    bin_dir="/p4/common/site/bin"
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
    config_dir=${config_dir:-"/etc/p4prometheus"}
    bin_dir="$config_dir"
    mkdir -p "$config_dir"
    chown "$OSUSER:$OSGROUP" "$config_dir"
fi

download_and_untar () {
    fname=$1
    url=$2
    [[ -f "$fname" ]] && rm -f "$fname"
    msg "downloading and extracting $url"
    wget -q "$url"
    tar zxvf "$fname"
}

install_monitor_metrics () {

    if [[ $UseSDP -eq 1 ]]; then
        cron_args="$SDP_INSTANCE"
    else
        cron_args="-p $P4PORT -u $P4USER -nosdp -m $metrics_root"
    fi
    mon_installer="/tmp/_install_mon.sh"
    cat << EOF > $mon_installer
# Download latest versions
mkdir -p $bin_dir
cd $bin_dir
for scriptname in monitor_metrics.py monitor_wrapper.sh; do
    [[ -f "\$scriptname" ]] && rm "\$scriptname"
    echo "downloading \$scriptname"
    wget "https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts/\$scriptname"
    chmod +x "\$scriptname"
    chown "$OSUSER:$OSGROUP" "\$scriptname"
done

# Install in crontab if required
mytab="/tmp/mycron"
scriptname="monitor_wrapper.sh"
if ! grep -q "\$scriptname" "\$mytab" ;then
    entry1="*/1 * * * * $bin_dir/\$scriptname $cron_args > /dev/null 2>&1 ||:"
    echo "\$entry1" >> "\$mytab"
fi
crontab "\$mytab"

# List things out for review
echo "Crontab after updating - showing monitor entries:"
crontab -l | grep /monitor_

EOF

    chmod 755 "$mon_installer"
    sudo -u "$OSUSER" bash "$mon_installer"

}

install_monitor_metrics

echo "

Should have installed monitor_metrics.py and wrapper script.
Check crontab -l output above (as user $OSUSER)

"
