#!/bin/bash
# Generate monitoring metrics for use with Prometheus (collected via node_explorer)
# If required, put this job into perforce user crontab:
#
#   */1 * * * * /p4/common/site/bin/monitor_metrics.sh $INSTANCE > /dev/null 2>&1 ||:
#
# Please note you need to make sure that the specified directory below (which may be linked)
# can be read by the node_exporter user (and is setup via --collector.textfile.directory parameter)
#
# Note we use a tempfile for each metric to avoid partial reads. Textfile collector only looks for files
# ending in .prom so we do a finale rename when ready

if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

# This might also be /hxlogs/metrics
metrics_root=/p4/metrics

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

/p4/common/site/bin/monitor_metrics.py  -i 1
