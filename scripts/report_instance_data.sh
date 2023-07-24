#!/bin/bash
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
#

#### WARNING
#### SINGLE INSTANCE USE ONLY
#### WARNING


#TODO MAKE IT MULTI INSTANCE USE
# ============================================================
# Configuration section

# Find out if we're in AWS, GCP, or AZURE.. NOT TESTED ON AZURE...YET'
#TODO NEEDS AZURE testing
declare -i autoCloud=1


# May be overwritten in the config file.
declare report_instance_logfile="/p4/1/logs/report_instance_data.log"
#declare report_instance_logfile="./report_instance_data.log" #Ugly stick testing

# Define the commands
declare -A commands=(
    ["Awesome p4 tiggers"]='p4 triggers -o | awk "/^Triggers:/ {flag=1; next} /^$/ {flag=0} flag" | sed "s/^[ \t]*//"'
    ["p4 extensions and configs"]="p4 extension --list --type extensions; p4 extension --list --type configs"
    ["systemD status all"]="systemctl status"
    ["p4 servers"]="p4 servers"
    ["p4 property -Al"]="p4 property -Al"
    ["p4 -Ztag Without the datefield?"]="p4 -Ztag info | awk '!/^... serverDate/'"
    ["p4 property -Al"]="p4 property -Al"
#Meh    ["MAILTO From p4vars"]="cat $P4CCFG/p4_1.vars | awk '/^export MAILTO=/{sub(/^export /, ""); print; exit}'"
)

### Auto Cloud Configs
### Timeout in seconds until we're done attempting to contact internal cloud information
autoCloudTimeout=5


# =============P4 Vars File parser config=====================

declare -A p4varsconfig


function define_config_p4varsfile() {
    local var_name="$1"
    p4varsconfig["$var_name"]="export $var_name="
}

# CONFIGURABLES-Define the what you would like parsed from P4_1.vars
define_config_p4varsfile "MAILTO"
define_config_p4varsfile "P4USER" #EXAMPLE
define_config_p4varsfile "P4MASTER_ID" #EXAMPLE
# Add more variables as needed

# Path to p4_1.vars file
file_path="$P4CCFG/p4_1.vars"



# ============================================================

declare ThisScript=${0##*/}

function msg () { echo -e "$*"; }
function log () { dt=$(date '+%Y-%m-%d %H:%M:%S'); echo -e "$dt: $*" >> "$report_instance_logfile"; msg "$dt: $*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage() {
    local style=${1:-"-h"}  # Default to "-h" if no style argument provided
    local errorMessage=${2:-"Unset"}

    if [[ "$errorMessage" != "Unset" ]]; then
        echo -e "\n\nUsage Error:\n\n$errorMessage\n\n" >&2
    fi

    echo "USAGE for $ThisScript:

$ThisScript -c <config_file> [-azure|-gcp]

    or

$ThisScript -h

    -azure      Specifies to collect Azure specific data (default is autoCloud on)
    -aws        Specifies to collect GCP specific data (default is autoCloud on)
    -gcp        Specifies to collect GCP specific data (default is autoCloud on)
    -acoff      Turns autoCloud off (default is autoCloud on)
    -timeout    Sets timeout(In seconds) to wait for Cloud provider responds (default is $autoCloudTimeout seconds)

Collects metadata about the current instance and pushes the data centrally.

This is not normally required on customer machines. It assumes an SDP setup."
}

function p4varsparse_file() {
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

# Command Line Processing

declare -i shiftArgs=0
ConfigFile=/p4/common/config/.push_metrics.cfg

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 0;;
        # (-man) usage -man;;
        (-c) ConfigFile=$2; shiftArgs=1;;
        (-azure) IsAzure=1; IsGCP=0; IsAWS=0; autoCloud=0; echo "Forced GCP by -azure";;
        (-aws) IsAWS=1; IsGCP=0; IsAzure=0; autoCloud=0; echo "Forced GCP by -aws";;
        (-gcp) IsGCP=1; IsAWS=0; IsAzure=0; autoCloud=0; echo "Forced GCP by -gcp";;
        (-acoff) autoCloud=0; echo "AutoCloud turned OFF";;
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

# Get config values - format: key=value
metrics_host=$(grep metrics_host "$ConfigFile" | awk -F= '{print $2}')
metrics_customer=$(grep metrics_customer "$ConfigFile" | awk -F= '{print $2}')
metrics_instance=$(grep metrics_instance "$ConfigFile" | awk -F= '{print $2}')
metrics_user=$(grep metrics_user "$ConfigFile" | awk -F= '{print $2}')
metrics_passwd=$(grep metrics_passwd "$ConfigFile" | awk -F= '{print $2}')
metrics_logfile=$(grep metrics_logfile "$ConfigFile" | awk -F= '{print $2}')
report_instance_logfile=$(grep report_instance_logfile "$ConfigFile" | awk -F= '{print $2}')

metrics_host=${metrics_host:-Unset}
metrics_customer=${metrics_customer:-Unset}
metrics_instance=${metrics_instance:-Unset}
metrics_user=${metrics_user:-Unset}
metrics_passwd=${metrics_passwd:-Unset}
report_instance_logfile=${report_instance_logfile:-/p4/1/logs/report_instance_data.log}
if [[ $metrics_host == Unset || $metrics_user == Unset || $metrics_passwd == Unset || $metrics_customer == Unset || $metrics_instance == Unset ]]; then
    echo -e "\\nError: Required parameters not supplied.\\n"
    echo "You must set the variables metrics_host, metrics_user, metrics_passwd, metrics_customer, metrics_instance in $ConfigFile."
    exit 1
fi

# Convert host from 9091 -> 9092 (pushgateway -> datapushgateway default)
# TODO - make more configurable
metrics_host=${metrics_host/9091/9092}

# Collect various metrics into a tempt report file we post off

pushd $(dirname "$metrics_logfile")
TempLog="_instance_data.log"

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
    else
        echo "You are not on a GCP machine."
        declare -i IsGCP=0
    fi
    }
    else {
        echo "Not using autoCloud"
        # Default to AWS
#TODO Look into this defaults to AWS... I think defaulting to 0 on all 3 is good (open to suggestions)
        declare -i IsAWS=0
        declare -i IsAzure=0
        declare -i IsGCP=0
    }

fi




# For AWS:
# curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/dynamic/instance-identity/document
# {
#   "accountId" : "251689412290",
#   "architecture" : "x86_64",
#   "availabilityZone" : "us-east-1a",
#   "billingProducts" : null,
#   "devpayProductCodes" : null,
#   "marketplaceProductCodes" : null,
#   "imageId" : "ami-047261a33f6dcc468",
#   "instanceId" : "i-0fce0e35c7b971d6a",
#   "instanceType" : "c5.18xlarge",
#   "kernelId" : null,
#   "pendingTime" : "2022-05-22T05:08:09Z",
#   "privateIp" : "10.0.0.239",
#   "ramdiskId" : null,
#   "region" : "us-east-1",
#   "version" : "2017-09-30"
# }

rm -f $TempLog

# Start creating report in Markdown format - being careful to quote backquotes properly!

# TODO Can probably put this in the commands list to ran.. Output of commands are not always sequential(?As of yet?) that they were ran. Deciding to keep hostnamectl
{
    echo "# Output of hostnamectl"
    echo ""
    echo '```'
    hostnamectl
    echo '```'
    echo ""
} >> $TempLog 2>&1
{
    echo "# Output of P4VARS PARSER"
    echo ""
    echo '```'
    # Grab stuff from p4_1.vars file
    p4varsparse_file "$file_path" >> $TempLog 2>&1
    echo '```'
    echo ""
} >> $TempLog 2>&1


if [[ $IsAWS -eq 1 ]]; then
    TOKEN=$(curl --connect-timeout $autoCloudTimeout -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
    Doc1=$(curl --connect-timeout $autoCloudTimeout -H "X-aws-ec2-metadata-token: $TOKEN" "http://169.254.169.254/latest/dynamic/instance-identity/document")
    Doc2=$(curl --connect-timeout $autoCloudTimeout -H "X-aws-ec2-metadata-token: $TOKEN" "http://169.254.169.254/latest/meta-data/tags/instance/")
    {
        echo "# AWS Metadata"
        echo ""
        echo '```'
        echo "$Doc1"
        echo '```'
        echo ""
        echo "# AWS Tags"
        echo ""
        echo '```'
        if echo $Doc2 | grep -q '404 - Not Found'; then
            echo "Not available - check Instance permissions"
        else
            for t in $Doc2; do
                v=$(curl --connect-timeout $autoCloudTimeout -s -H "X-aws-ec2-metadata-token: $TOKEN" "http://169.254.169.254/latest/meta-data/tags/instance/$t")
                echo "$t: $v"
            done
        fi
        echo '```'
        echo ""
    } >> $TempLog 2>&1
fi

if [[ $IsAzure -eq 1 ]]; then
    Doc=$(curl --connect-timeout $autoCloudTimeout -s -H Metadata:true --noproxy "*" "http://169.254.169.254/metadata/instance?api-version=2021-02-01" | python -m json.tool)
    {
        echo "# Azure Metadata"
        echo ""
        echo '```'
        echo "$Doc"
        echo '```'
    } >> $TempLog
fi

if [[ $IsGCP -eq 1 ]]; then
    Doc=$(curl --connect-timeout $autoCloudTimeout "http://metadata.google.internal/computeMetadata/v1/?recursive=true&alt=text" -H "Metadata-Flavor: Google")
    {
        echo "# GCP Metadata"
        echo ""
        echo '```'
        echo "$Doc"
        echo '```'
    } >> $TempLog
fi
{
for label in "${!commands[@]}"; do

    command="${commands[$label]}"
    echo "# Output of $label"
    echo ""
    echo '```'
    eval "$command"
    echo '```'
    echo ""
done
} >> $TempLog 2>&1


# Loop while pushing as there seem to be temporary password failures quite frequently
# TODO Look into this


iterations=0
max_iterations=10
STATUS=1
while [ $STATUS -ne 0 ]; do
    sleep 1
    ((iterations=$iterations+1))
    log "Pushing data"
    result=$(curl --connect-timeout $autoCloudTimeout --retry 5 --user "$metrics_user:$metrics_passwd" --data-binary "@$TempLog" "$metrics_host/data/?customer=$metrics_customer&instance=$metrics_instance")
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
