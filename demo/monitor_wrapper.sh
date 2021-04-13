#!/bin/bash
# Generate lock monitoring metrics and log file for use with Prometheus (collected via node_exporter)
# Calls the underlying script monitor_metrics.py
# Note that the Python script requires the 'lslocks' utility to be installed.
#
# If used, put this job into perforce user crontab, for SDP, e.g. where INSTANCE=1:
#
#   */1 * * * * /p4/common/site/bin/monitor_wrapper.sh $INSTANCE > /dev/null 2>&1 ||:
#
# For non-SDP installation, either specify port/user or ensure P4PORT and P4USER are set in environment:
#
#   */1 * * * * /p4/common/site/bin/monitor_wrapper.sh -nosdp -p server:1666 -u p4admin > /dev/null 2>&1 ||:
#
# If not using SDP then please ensure that appropriate LONG TERM TICKET is setup in the environment
# that this script is running.
#
# You can specify metrics root director (for use with node_exporter) with: -m /my_metrics
#
# Please note you need to make sure that the specified directory below (which may be linked)
# can be read by the node_exporter user (and is setup via --collector.textfile.directory parameter)
#

# This might also be /hxlogs/metrics or /var/metrics, and can be set via the "-m" parameter to script.
metrics_root=/p4/metrics


function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage
{
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for monitor_wrapper.sh:
 
monitor_wrapper.sh [<instance> | -nosdp] [-p <port>] | [-u <user>] | [-m <metrics_dir>]
 
   or
 
monitor_wrapper.sh -h
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

[[ -d "$metrics_root" ]] || bail "Specified metrics directory '$metrics_root' does not exist!"

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
    sdpinst_label=",sdpinst=\"$SDP_INSTANCE\""
    sdpinst_suffix="-$SDP_INSTANCE"
    p4logfile="$P4LOG"
    errors_file="$LOGS/errors.csv"
else
    p4port=${Port:-$P4PORT}
    p4user=${User:-$P4USER}
    p4="p4 -u $p4user -p $p4port"
    $p4 info -s || bail "Can't connect to P4PORT: $p4port"
    sdpinst_label=""
    sdpinst_suffix=""
    p4logfile=$($p4 configure show | grep P4LOG | sed -e 's/P4LOG=//' -e 's/ .*//')
    errors_file=$($p4 configure show | egrep "serverlog.file.*errors.csv" | cut -d= -f2 | sed -e 's/ (.*//')
    check_for_replica=$($p4 info | grep -c 'Replica of:')
    if [[ "$check_for_replica" -eq "0" ]]; then
        P4REPLICA="FALSE"
    else
        P4REPLICA="TRUE"
    fi
fi

# Get server id
SERVER_ID=$($p4 serverid | awk '{print $3}')
SERVER_ID=${SERVER_ID:-noserverid}
serverid_label="serverid=\"$SERVER_ID\""

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

# Get server id
SERVER_ID=$($p4 serverid | awk '{print $3}')
SERVER_ID=${SERVER_ID:-unset}

# Adjust to your script location if required
/p4/common/site/bin/monitor_metrics.py  -i "$SDP_INSTANCE" -m "$metrics_root"
