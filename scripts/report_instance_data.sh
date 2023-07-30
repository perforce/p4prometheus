#!/bin/bash
TempLog="/home/perforce/workspace/command-runner/output.log"
commandRunnerPath="/home/perforce/workspace/command-runner/command-runner"
commandYamlPath="/home/perforce/workspace/command-runner/commands.yaml"
rm -f $TempLog
# report_instance_data.sh
#
# Collects basic instance metadata about a customer environment (for AWS and Azure and ultimately other cloud envs)
#
# If used, put this job into perforce user crontab:
#
#   10 0 * * * /p4/common/site/bin/report_instance_data.sh -c /p4/common/config/.push_metrics.cfg > /dev/null 2>&1 ||:
#
# You can specify a config file as above, with expected format the same as for push_metrics.sh
#
# Uses AWS metadata URLs as defined: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instancedata-data-retrieval.html
#
# Please note you need to make sure that the specified directory below (which may be linked)
# can be read by the node_exporter user (and is setup via --collector.textfile.directory parameter)
#
#TODO Autodectect is this a p4d instance better
#TODO Better logging
#TODO NEEDS AZURE testing
#TODO Expand for support people also ?datapushgateway? lets GO!!
#     - p4 -Ztag info
#     - p4 configure show allservers
#     - p4 servers
#     - p4 servers -J  -- Concerns about this changing
#TODO SWARM
#TODO HAS
# ============================================================
# Configuration section
# Find out if we're in AWS, GCP, or AZURE..

declare -i autoCloud=0
declare -i p4dRunning=0
declare -i swarmRunning=0
declare -i hasRunning=0

#This scripts default config file location
ConfigFile="/p4/common/config/.push_metrics.cfg"

## example .push_metrics.cfg
# ----------------------
# metrics_host=http://some.ip.or.host:9091
# metrics_customer=Customer-Name
# metrics_instance=       <------ #TODO Reconsider this for multiple instances mostly.. I think this should probably be set by the script instead?
# metrics_user=username-for-pushgateway
# metrics_passwd=password-for-pushgateway
# report_instance_logfile=/log/file/location
# metrics_cloudtype=AWS,GCP,AZure
# ----------------------

# May be overwritten in the config file.
declare report_instance_logfile="/p4/1/logs/report_instance_data.log"

generate_random_serial() {
    local characters="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
    local serial=""
    local char_count=${#characters}

    for i in {1..6}; do
        # Generate a random index to pick a character from the list
        local random_index=$((RANDOM % char_count))
        # Append the selected character to the serial
        serial="${serial}${characters:$random_index:1}"
    done

    echo "$serial"
}


### Auto Cloud Configs
### Timeout in seconds until we're done attempting to contact internal cloud information
autoCloudTimeout=5

# ============= DONT TOUCH =============
declare -A p4varsconfig
function define_config_p4varsfile () {
    local var_name="$1"
    p4varsconfig["$var_name"]="export $var_name="
}
# ============= DONT TOUCH =============

# =============P4 Vars File parser config=====================
# Define the what you would like parsed from P4__INSTANCE__.vars
define_config_p4varsfile "MAILTO"
define_config_p4varsfile "P4USER" #EXAMPLE
define_config_p4varsfile "P4MASTER_ID" #EXAMPLE
# Add more variables as needed

# ============================================================

declare ThisScript=${0##*/}

function msg () { echo -e "$*"; }
function log () { dt=$(date '+%Y-%m-%d %H:%M:%S'); echo -e "$dt: $*" >> "$report_instance_logfile"; msg "$dt: $*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }
function upcfg () { echo "metrics_cloudtype=$1" >> "$ConfigFile"; } #TODO This could be way more elegant IE error checking the config file but it works
function p4varsparse_file () {
    local file_path="$1"
    while IFS= read -r line; do
        for key in "${!p4varsconfig[@]}"; do
            if [[ "$line" == "${p4varsconfig[$key]}"* ]]; then
                value=${line#${p4varsconfig[$key]}}
                echo "$key=$value"
            fi
        done
    done < "$file_path"
}
#
# Work instances here
function work_instance () {
    local instance="$1"
    source /p4/common/bin/p4_vars $instance
    file_path="$P4CCFG/p4_$instance.vars"
    echo "Working instance labeled as: $instance"
    # Your processing logic for each instance goes here
    {
        #Thanks tom
        #TODO Command runner path
        run_if_master.sh $instance $commandRunnerPath -instance=$instance -output=$TempLog -comyaml=$commandYamlPath
    }
}


# Instance Counter
# Thanks to ttyler below
function get_sdp_instances () {
    echo "Finding p4d instances"
    SDPInstanceList=
    cd /p4 || bail "Could not cd to /p4."
    for e in *; do
        if [[ -r "/p4/$e/root/db.counters" ]]; then
            SDPInstanceList+=" $e"
        fi
    done

    # Trim leading space.
    # shellcheck disable=SC2116
    SDPInstanceList=$(echo "$SDPInstanceList")
    echo "Instance List: $SDPInstanceList"

    # Count instances
    instance_count=$(echo "$SDPInstanceList" | wc -w)
    echo "Instances Names: $instance_count"

    # Loop through each instance and call the process_instance function
    for instance in $SDPInstanceList; do
        work_instance $instance
    done
}

function findSwarm () {
    SwarmURL=$(p4 -ztag -F %value% property -n P4.Swarm.URL -l)
    if [[ -n "$SwarmURL" ]]; then
        echo -e "There be Swarm here: $SwarmURL";
        swarmRunning=1
    else
        echo "We don't need no stink'n bees.";
fi
}

function findHAS() {
    HASExtensionVersion=$(p4 -ztag -F %ExtVersion% extension --configure Auth::loginhook -o)
    if [[ -n "$SwarmURL" ]]; then
        echo -e "There be HAS here, version: $HASExtensionVersion";
        hasRunning=1
    else
        echo "No HAS installed.";
fi
}
function findP4D () {
    # Function to p4d check if a process is running
    if pgrep -f "p4d_*" >/dev/null; then
        echo "p4d service is running."
        p4dRunning=1
    else
        echo "p4d service is not running."
    fi
}
##SwarmURL=$(p4d -k db.property -jd - 2>&1 | grep @P4.Swarm.URL@|cut -d @ -f 6)
##if [[ -n "$SwarmURL" ]]; then echo -e "There be Swarm here: $SwarmURL"; else echo "We don't need no stink'n bees."; fi



function usage () {
    local style=${1:-"-h"}  # Default to "-h" if no style argument provided
    local errorMessage=${2:-"Unset"}
    if [[ "$errorMessage" != "Unset" ]]; then
        echo -e "\n\nUsage Error:\n\n$errorMessage\n\n" >&2
    fi

    echo "USAGE for $ThisScript:

$ThisScript -c <config_file> [-azure|-gcp]

    or

$ThisScript -h

    -azure      Specifies to collect Azure specific data
    -aws        Specifies to collect GCP specific data
    -gcp        Specifies to collect GCP specific data
    -acoff      Turns autoCloud off
    -acon       Turns autoCloud on
    -timeout    Sets timeout(In seconds) to wait for Cloud provider responds (default is $autoCloudTimeout seconds)

Collects metadata about the current instance and pushes the data centrally.

This is not normally required on customer machines. It assumes an SDP setup."
}

# Command Line Processing

declare -i shiftArgs=0

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 0;;
        # (-man) usage -man;;
        (-c) ConfigFile=$2; shiftArgs=1;;
        (-azure) IsAzure=1; IsGCP=0; IsAWS=0; autoCloud=0; echo "Forced GCP by -azure";;
        (-aws) IsAWS=1; IsGCP=0; IsAzure=0; autoCloud=0; echo "Forced GCP by -aws";;
        (-gcp) IsGCP=1; IsAWS=0; IsAzure=0; autoCloud=0; echo "Forced GCP by -gcp";;
        (-acoff) autoCloud=3; echo "AutoCloud turned OFF";;
        (-acon) autoCloud=1; echo "AutoCloud turned ON";;
        (-timeout) shift; autoCloudTimeout=$1; echo "Setting autoCloudTimeout to $autoCloudTimeout";;
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

# Get config values from config file- format: key=value
metrics_host=$(grep metrics_host "$ConfigFile" | awk -F= '{print $2}')
metrics_customer=$(grep metrics_customer "$ConfigFile" | awk -F= '{print $2}')
metrics_instance=$(grep metrics_instance "$ConfigFile" | awk -F= '{print $2}')
metrics_user=$(grep metrics_user "$ConfigFile" | awk -F= '{print $2}')
metrics_passwd=$(grep metrics_passwd "$ConfigFile" | awk -F= '{print $2}')
metrics_logfile=$(grep metrics_logfile "$ConfigFile" | awk -F= '{print $2}')
report_instance_logfile=$(grep report_instance_logfile "$ConfigFile" | awk -F= '{print $2}')
metrics_cloudtype=$(grep metrics_cloudtype "$ConfigFile" | awk -F= '{print $2}')
# Set all thats not set to Unset
metrics_host=${metrics_host:-Unset}
metrics_customer=${metrics_customer:-Unset}
metrics_instance=${metrics_instance:-Unset}
metrics_user=${metrics_user:-Unset}
metrics_passwd=${metrics_passwd:-Unset}
report_instance_logfile=${report_instance_logfile:-/p4/1/logs/report_instance_data.log}
metrics_cloudtype=${metrics_cloudtype:-Unset}
if [[ $metrics_host == Unset || $metrics_user == Unset || $metrics_passwd == Unset || $metrics_customer == Unset || $metrics_instance == Unset ]]; then
    echo -e "\\nError: Required parameters not supplied.\\n"
    echo "You must set the variables metrics_host, metrics_user, metrics_passwd, metrics_customer, metrics_instance in $ConfigFile."
    exit 1
fi
echo autocloud is set to $autoCloud
## Auto set cloudtype in config?
if [[ $metrics_cloudtype == Unset ]]; then
    echo -e "No Instance Type Defined"
    if [[ $autoCloud != 3 ]]; then
        echo -e "using autoCloud"
        autoCloud=1
    fi
fi
cloudtype="${metrics_cloudtype^^}"

# Convert host from 9091 -> 9092 (pushgateway -> datapushgateway default)
# TODO - make more configurable
metrics_host=${metrics_host/9091/9092}

# Collect various metrics into a tempt report file we post off

pushd $(dirname "$metrics_logfile")

if [ $autoCloud -eq 1 ]; then
{
    echo "Using autoCloud"
    #==========================
    # Check if running on AZURE
    echo "Checking for AZURE"
    curl --connect-timeout $autoCloudTimeout -s -H Metadata:true --noproxy "*" "http://169.254.169.254/metadata/instance?api-version=2021-02-01" | grep -q "location"
    if [ $? -eq 0 ]; then
        curl --connect-timeout $autoCloudTimeout -s curl --connect-timeout $autoCloudTimeout -s -H Metadata:true --noproxy "*" "http://169.254.169.254/metadata/instance?api-version=2021-02-01" | grep "location"  | awk -F\" '{print $4}' >/dev/null
        echo "You are on an AZURE machine."
        declare -i IsAzure=1
        upcfg "Azure"
    else
        echo "You are not on an AZURE machine."
        declare -i IsAzure=0
    fi
    #==========================
    # Check if running on AWS
    echo "Checking for AWS"
    #aws_region_check=$(curl --connect-timeout $autoCloudTimeout -s http://169.254.169.254/latest/dynamic/instance-identity/document | grep -q "region")
    curl --connect-timeout $autoCloudTimeout -s http://169.254.169.254/latest/dynamic/instance-identity/document | grep -q "region"
    if [ $? -eq 0 ]; then
        curl --connect-timeout $autoCloudTimeout -s http://169.254.169.254/latest/dynamic/instance-identity/document | grep "region"  | awk -F\" '{print $4}' >/dev/null
        echo "You are on an AWS machine."
        declare -i IsAWS=1
        upcfg "AWS"
    else
        echo "You are not on an AWS machine."
        declare -i IsAWS=0
    fi
    #==========================
    # Check if running on GCP
    echo "Checking for GCP"
    curl --connect-timeout $autoCloudTimeout -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/?recursive=true" -s | grep -q "google"
    if [ $? -eq 0 ]; then
        echo "You are on a GCP machine."
        declare -i IsGCP=1
        upcfg "GCP"
    else
        echo "You are not on a GCP machine."
        declare -i IsGCP=0
    fi
    }
    if [[ $IsAWS -eq 0 && $IsAzure -eq 0 && $IsGCP -eq 0 ]]; then
        echo "No cloud detected setting to OnPrem"
        upcfg "OnPrem"
        declare -i IsOnPrem=1
    fi

    else {
        echo "Not using autoCloud"
        declare -i IsAWS=0
        declare -i IsAzure=0
        declare -i IsGCP=0
        declare -i IsOnPrem=0
    }
fi


if [[ $cloudtype == AZURE ]]; then
    echo -e "Config says cloud type is: Azure"
    declare -i IsAzure=1
fi
if [[ $cloudtype == AWS ]]; then
    echo -e "Config says cloud type is: AWS"
    declare -i IsAWS=1
fi
if [[ $cloudtype == GCP ]]; then
    echo -e "Config says cloud type is: GCP"
    declare -i IsGCP=1
fi
if [[ $cloudtype == ONPREM ]]; then
    echo -e "Config says cloud type is: OnPrem"
    declare -i IsOnPrem=1
fi

if [[ $IsAWS -eq 1 ]]; then
    echo "Doing the AWS meta-pull"
    $commandRunnerPath -output=$TempLog -yaml=$commandYamlPath -server -cloud=aws
fi

if [[ $IsAzure -eq 1 ]]; then
    echo "Doing the Azure meta-pull"
    # DO Azure command-runner stuff
    # $commandRunnerPath -output=$TempLog -comyaml=$commandYamlPath -server -cloud=azure
fi

if [[ $IsGCP -eq 1 ]]; then
    echo "Doing the GCP meta-pull"
    # DO GCP command-runner stuff
    $commandRunnerPath -output=$TempLog -comyaml=$commandYamlPath -server -cloud=gcp
fi

if [[ $IsOnPrem -eq 1 ]]; then
    echo "Doing the OnPrem stuff"
    $commandRunnerPath -output=$TempLog -comyaml=$commandYamlPath -server -server
fi
get_sdp_instances
findSwarm
findHAS


# Loop while pushing as there seem to be temporary password failures quite frequently
# TODO Look into this.. (Note: Looking at the go build it's potentially related datapushgate's go build)
iterations=0
max_iterations=10
STATUS=1

#Disabling for testing
#while [ $STATUS -ne 0 ]; do
#    sleep 1
#    ((iterations=$iterations+1))
#    log "Pushing Support data"
#    result=$(curl --connect-timeout $autoCloudTimeout --retry 5 --user "$metrics_user:$metrics_passwd" --data-binary "@$TempSMd" "$metrics_host/support/?customer=$metrics_customer&instance=$metrics_instance")
#    STATUS=0
#    log "Checking result: $result"
#    if [[ "$result" = '{"message":"invalid username or password"}' ]]; then
#        STATUS=1
#        log "Retrying due to temporary password failure"
#    fi
#    if [ "$iterations" -ge "$max_iterations" ]; then
#        log "Push loop iterations exceeded"
#        exit 1
#    fi
#done
popd