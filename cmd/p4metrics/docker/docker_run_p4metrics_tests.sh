#!/bin/bash
# Updates local p4metrics executable and runs tests
# It expects to be run as root within the container.

# shellcheck disable=SC2128
if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi

set -euxo pipefail

# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

function usage
{
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for run_p4metrics_tests.sh:
 
run_p4metrics_tests.sh [-nosdp] [-h]
 
    -nosdp      Means do the non-SDP version. Default is the SDP version.

Examples:

./run_p4metrics_tests.sh
./run_p4metrics_tests.sh -nosdp

"
}

# Command Line Processing
 
declare -i shiftArgs=0
declare -i UseSDP=1

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h  && exit 1;;
        # (-man) usage -man;;
        (-nosdp) UseSDP=0;;
        (-l) P4LOG="$2"; shiftArgs=1;;
        (-*) usage -h "Unknown command line option ($1)." && exit 1;;
        (*) usage -h  && exit 1;;
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

# Copy in latest (local) build of p4metrics
cp /p4metrics/bin/p4metrics.linux-arm64.gz /tmp/
cd /tmp
gunzip p4metrics.linux-arm64.gz
systemctl stop p4metrics
mv p4metrics.linux-arm64 /usr/local/bin/p4metrics
sed -i -E 's/ExecStart=(.*)/ExecStart=\1 --debug/' /etc/systemd/system/p4metrics.service
systemctl daemon-reload

rm /p4/metrics/*.prom

sleep 2
source /p4/common/bin/p4_vars 1
p4 info
p4 depots

if [[ $UseSDP -eq 1 ]]; then
    cd /p4/common/config
    sed -i -e 's/update_interval: .*/update_interval: 5s/' p4metrics.yaml
    sudo systemctl restart p4metrics
    sleep 7
    ls -ltr /p4/metrics/*.prom
    cd /p4metrics/tests
    pytest -vvv test_p4metrics.py
else
    echo "Skipping SDP tests as -nosdp was specified"
fi
