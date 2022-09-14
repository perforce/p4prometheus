#!/bin/bash
# push_metrics.sh
# 
# Takes node_exporter metrics and pushes them to pushgateway instance centrally.
#
# If used, put this job into perforce user crontab, for SDP, e.g. where INSTANCE=1:
#
#   */1 * * * * /p4/common/site/bin/push_metrics.sh -c /p4/common/config/.push_metrics.cfg > /dev/null 2>&1 ||:
#
# You can specify a config file as above, with expected format:
#
#   metrics_host=https://monitorgw.hra.p4demo.com:9100
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

node_exporter_url="http://localhost:9100"
metrics_logfile="/p4/1/logs/push_metrics.log"

function msg () { echo -e "$*"; }
function log () { dt=$(date '+%Y-%m-%d %H:%M:%S'); echo -e "$dt: $*" >> "$metrics_logfile"; msg "$dt: $*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

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

   if [[ "$style" == -man ]]; then
       # Add full manual page documentation here.
      true
   fi
   exit 2
}

# Command Line Processing
 
declare -i shiftArgs=0
ConfigFile=/p4/common/config/.push_metrics.cfg

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h;;
        (-man) usage -man;;
        (-c) ConfigFile=$2; shiftArgs=1;;
        (-*) usage -h "Unknown command line option ($1).";;
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
metrics_logfile=${metrics_logfile:-/p4/1/logs/push_metrics.log}
if [[ $metrics_host == Unset || $metrics_user == Unset || $metrics_passwd == Unset || $metrics_instance == Unset || $metrics_customer == Unset ]]; then
   echo -e "\\nError: Required parameters not supplied.\\n"
   echo "You must set the variables metrics_host, metrics_user, metrics_passwd, metrics_instance, metrics_custmer in $ConfigFile."
   exit 1
fi

curl "$node_exporter_url/metrics" > _push.log

# Loop while pushing as there seem to be temporary password failures quite frequently

iterations=0
max_iterations=10
STATUS=1
while [ $STATUS -ne 0 ]; do
    sleep 1
    iterations=$((iterations+1))
    log "Pushing metrics"
    result=$(curl --retry 5 --user "$metrics_user:$metrics_passwd" --data-binary @_push.log "$metrics_host/metrics/job/$metrics_job/instance/$metrics_instance/customer/$metrics_customer")
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
