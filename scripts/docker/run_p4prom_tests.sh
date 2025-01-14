#!/bin/bash
# This script sets up the docker container for use with nonsdp p4prometheus testing
# It expects to be run as root within the container.

# shellcheck disable=SC2128
if [[ -z "${BASH_VERSINFO}" ]] || [[ -z "${BASH_VERSINFO[0]}" ]] || [[ ${BASH_VERSINFO[0]} -lt 4 ]]; then
    echo "This script requires Bash version >= 4";
    exit 1;
fi


# ============================================================

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

function usage
{
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for run_p4prom_tests.sh:
 
run_p4prom_tests.sh [-nosdp] [-h]
 
    -nosdp      Means do the non-SDP version. Default is the SDP version.

Examples:

./run_p4prom_tests.sh
./run_p4prom_tests.sh -nosdp

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

cd /root

if [[ $UseSDP -eq 1 ]]; then
    su - perforce -c "p4d -Gc"
    systemctl start p4d_1
    sleep 3
    su - perforce -c "p4 trust -y"
    su - perforce -c "/p4/sdp/Server/setup/configure_new_server.sh 1"

    metrics_dir=/hxlogs/metrics
    ./install_p4prom.sh 1
    source /p4/common/bin/p4_vars 1
else
    ./setup_nonsdp.sh

    p4dctl start -a

    export P4PORT=1666
    export P4USER=perforce
    metrics_dir=/p4metrics
    ./install_p4prom.sh -nosdp -m /p4metrics -u perforce -m "$metrics_dir"
    config_file="/etc/p4prometheus/p4prometheus.yaml"
    sed -i 's@log_path:.*@log_path: /opt/perforce/servers/test/log@' "$config_file"
    sed -i 's@server_id:.*@server_id: test.server@' "$config_file"

    # echo test.server > /opt/perforce/servers/test/server.id
fi

# Need to restart as the config file won't have been valid
sudo systemctl restart p4prometheus

sleep 1

p4 info
p4 depots

sleep 1

sudo -u perforce crontab -l > /tmp/c.out
chmod 644 /tmp/c.out

# Extract the script to run and run it
if [[ $UseSDP -eq 1 ]]; then
    script="/p4/common/site/bin/monitor_metrics.sh 1"
else
    script=$(grep monitor_metrics /tmp/c.out | sed -e 's@.*/etc@/etc@' | sed -e "s/ >.*//")
fi
sudo -u perforce $script

# Restart where we can see output
sudo systemctl stop node_exporter
nohup /usr/local/bin/node_exporter --collector.textfile.directory="$metrics_dir" > /tmp/node.out &

sleep 3
sudo -u perforce p4 configure show

if [[ $UseSDP -eq 1 ]]; then
    py.test -v test_sdp.py
else
    py.test -v test_nosdp.py
fi
