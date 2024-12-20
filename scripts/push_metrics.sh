#!/bin/bash
# push_metrics.sh
# 
# Takes node_exporter metrics and pushes them to a central pushgateway instance.
#
# If used, put this job into perforce user crontab:
#
#   */1 * * * * /p4/common/site/bin/push_metrics.sh -c /p4/common/config/.push_metrics.cfg > /dev/null 2>&1 ||:
#
# You can specify a config file as above, with expected format:
#
#   metrics_host=https://monitorgw.hra.p4demo.com:9091
#   metrics_user=customerid
#   metrics_passwd=MySecurePassword
#   metrics_job=pushgateway
#   metrics_instance=test_hra_custid-prod-hra
#   metrics_customer=test_hra_custid
#   enabled=1
#
# Note that "enabled" should be set to 1 when you have everything correctly defined.
# Otherwise you risk incorrectly named metrics being pushed.
#
# Please note you need to make sure that the specified directory below (which may be linked)
# can be read by the node_exporter user (and is setup via --collector.textfile.directory parameter)
#
# Function to find and set the INSTANCE variable
get_sdp_instances () {
    echo "Searching for p4d/SDP"
    if [ ! -d "/p4" ]; then
        echo "p4d/SDP environment not detected."
        return
    fi

    echo "Finding p4d instances"
    local SDPInstanceList=
    cd /p4 || exit 1  # Exit if cannot change to /p4 directory.
    for e in *; do
        if [[ -r "/p4/$e/root/db.counters" ]]; then
            SDPInstanceList+=" $e"
        fi
    done
    SDPInstanceList=$(echo "$SDPInstanceList")  # Trim leading space.
    echo "Instance List: $SDPInstanceList"

    local instance_count=$(echo "$SDPInstanceList" | wc -w)
    echo "Instances Found: $instance_count"

    if [ "$instance_count" -eq 1 ]; then
        INSTANCE=$SDPInstanceList
        echo "Single instance found: $INSTANCE"
    elif [ "$instance_count" -gt 1 ]; then
        echo "Multiple instances found. Using the first one."
        INSTANCE=$(echo "$SDPInstanceList" | awk '{print $1}')
    else
        echo "No instances found. Using default instance."
        INSTANCE="1"  # Set to a default value or handle as required
    fi
}

# Check SDP_INSTANCE first, then fallback to INSTANCE
if [ -n "$SDP_INSTANCE" ]; then
    INSTANCE=$SDP_INSTANCE
elif [ -z "$INSTANCE" ]; then
    get_sdp_instances
fi


# ============================================================
# Configuration section

node_exporter_url="http://localhost:9100"
# The following may be overwritten in the config_file
metrics_logfile="/p4/${INSTANCE}/logs/push_metrics.log"

# ============================================================

function msg () { echo -e "$*"; }
function log () { dt=$(date '+%Y-%m-%d %H:%M:%S'); echo -e "$dt: $*" >> "$metrics_logfile"; msg "$dt: $*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage
{
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for push_metrics.sh:
 
push_metrics.sh -c <config_file>
 
   or
 
push_metrics.sh -h

Takes node_exporter metrics and pushes them to pushgateway instance centrally.

This is not normally required on customer machines. It assumes an SDP setup.
"
}

# Command Line Processing
 
declare -i shiftArgs=0
ConfigFile="/p4/common/config/.push_metrics.cfg"

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 0;;
        # (-man) usage -man;;
        (-c) ConfigFile=$2; shiftArgs=1;;
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

[[ -f "$ConfigFile" ]] || bail "Can't find config file: ${ConfigFile}!"

# Get config values - format: key=value
metrics_host=$(grep metrics_host "$ConfigFile" | awk -F= '{print $2}')
metrics_job=$(grep metrics_job "$ConfigFile" | awk -F= '{print $2}')
metrics_instance=$(grep metrics_instance "$ConfigFile" | awk -F= '{print $2}')
metrics_customer=$(grep metrics_customer "$ConfigFile" | awk -F= '{print $2}')
metrics_user=$(grep metrics_user "$ConfigFile" | awk -F= '{print $2}')
metrics_passwd=$(grep metrics_passwd "$ConfigFile" | awk -F= '{print $2}')
metrics_logfile=$(grep metrics_logfile "$ConfigFile" | awk -F= '{print $2}')

metrics_host=${metrics_host:-Unset}
metrics_job=${metrics_job:-Unset}
metrics_instance=${metrics_instance:-Unset}
metrics_customer=${metrics_customer:-Unset}
metrics_user=${metrics_user:-Unset}
metrics_passwd=${metrics_passwd:-Unset}
metrics_logfile=${metrics_logfile:-/p4/${INSTANCE}/logs/push_metrics.log}
if [[ $metrics_host == Unset || $metrics_user == Unset || $metrics_passwd == Unset || $metrics_instance == Unset || $metrics_customer == Unset ]]; then
   echo -e "\\nError: Required parameters not supplied.\\n"
   echo "You must set the variables metrics_host, metrics_user, metrics_passwd, metrics_instance, metrics_customer in $ConfigFile."
   exit 1
fi

pushd $(dirname "$metrics_logfile")
TempLog="_push.log"
curl "$node_exporter_url/metrics" > "$TempLog"

# Loop while pushing as there seem to be temporary password failures quite frequently

iterations=0
max_iterations=10
STATUS=1
while [ $STATUS -ne 0 ]; do
    sleep 1
    ((iterations=$iterations+1))
    log "Pushing metrics"
    result=$(curl --retry 5 --user "$metrics_user:$metrics_passwd" --data-binary "@$TempLog" "$metrics_host/metrics/job/$metrics_job/instance/$metrics_instance/customer/$metrics_customer")
    STATUS=0
    log "Checking result: $result"
    if [[ "$result" = '{"message":"invalid username or password"}' ]]; then
        STATUS=1
        log "Retrying due to temporary password failure"
    fi
    if [ "$iterations" -ge "$max_iterations" ]; then
        log "Push loop iterations exceeded"
        exit 1
    fi
done

popd
