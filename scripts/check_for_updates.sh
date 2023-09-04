#!/bin/bash

repo_path="scripts"
github_url="https://api.github.com/repos/perforce/p4prometheus/commits?per_page=1&path=$repo_path"
github_download_url="https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts"
command_runner_releases_url="https://api.github.com/repos/willKman718/command-runner/releases/latest"

p4prom_bin_dir="/p4/common/site/bin"

binary_inside_tar="command-runner-linux-amd64"
config_inside_tar="cmd_config.yaml"

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage {
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for check_for_updates.sh:
 
check_for_updates.sh -c <config_file>
 
   or
 
check_for_updates.sh -h

Checks github repo for script updates, and downloads them if available.
Uses the github API and stores a local file with current status.

Depends on 'curl' and 'jq' being in the path.
"
}

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
FILE_LIST="monitor_metrics.sh monitor_metrics.py monitor_wrapper.sh push_metrics.sh check_for_updates.sh create_dashboard.py dashboard.yaml upload_grafana_dashboard.sh"
declare -i shiftArgs=0
ConfigFile=".update_config"

#Where to put command-runner
if [[ -d "/etc/metrics" ]]; then
    echo "/etc/metrics exists."
    bin_dir=/etc/metrics
    cron_cmd="$bin_dir/command-runner --server --instance=\${INSTANCE} --mcfg=/etc/metrics/.push_metrics.cfg --log=/var/metrics/command-runner.log"
elif [[ -d "/p4/common/site/bin" ]]; then
    echo "/p4/common/site/bin exists, but /etc/metrics does not."
    bin_dir=/p4/common/site/bin
    cron_cmd="$bin_dir/command-runner --server --instance=\\\${INSTANCE} --log=/p4/\$INSTANCE/logs/command-runner.log"
else
    echo "Neither /etc/metrics nor /p4/common/site/bin exist."
    bin_dir=$SCRIPT_DIR
    cron_cmd="$bin_dir/command-runner --server --instance=\\\${INSTANCE} --log=/p4/\$INSTANCE/logs/command-runner.log"
fi

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h && exit 0;;
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

cd "$SCRIPT_DIR" || bail "Can't cd to $SCRIPT_DIR"

curl=$(which curl)
[[ $? -eq 0 ]] || bail "Failed to find curl in path"
jq=$(which jq)
[[ $? -eq 0 ]] || bail "Failed to find jq in path"

last_github_sha=""
last_github_date=""
last_command_runner_version=""

if [[ -e "$ConfigFile" ]]; then
    last_github_sha=$(grep last_github_sha "$ConfigFile" | cut -d= -f2)
    last_github_date=$(grep last_github_date "$ConfigFile" | cut -d= -f2)
    last_command_runner_version=$(grep last_command_runner_version "$ConfigFile" | cut -d= -f2)
fi

github_sha=$(curl -s "$github_url" | jq -r '.[0].sha')
github_date=$(curl -s "$github_url" | jq -r '.[0].commit.committer.date')

# Get the latest tag info
#latest_command_runner_version=$(curl -s "$command_runner_tags_url" | jq -r '.[0].name')
#tar_download_url=$(curl -s "$command_runner_tags_url" | jq -r '.[0].tarball_url')
# Get the latest release info
latest_command_runner_version=$(curl -s "$command_runner_releases_url" | jq -r '.tag_name')
tar_download_url=$(curl -s "$command_runner_releases_url" | jq -r '.assets[] | select(.name | endswith(".tar.gz")).browser_download_url')



if [[ "$last_command_runner_version" != "$latest_command_runner_version" ]]; then
    msg "Updating command-runner binary and config"
    
    # Assuming you want to download to the current directory
    tar_file_name="${latest_command_runner_version}.tar.gz"
    wget "$tar_download_url" -O "$tar_file_name"
    
    # Extract binary and config
    tar -xvf "$tar_file_name" "$binary_inside_tar" "$config_inside_tar"
    
    # Move binary to script directory and config to its location
    mv "$binary_inside_tar" "$bin_dir/command-runner"
    chmod +x "$bin_dir/command-runner"
    mv "$config_inside_tar" "/p4/common/config/"
    
    # Clean up the downloaded tar.gz file
    #rm -f "$tar_file_name"

    echo "last_command_runner_version=$latest_command_runner_version" >> "$ConfigFile"
    msg "Command-runner binary and config updated to version $latest_command_runner_version"
else
    msg "Command-runner binary and config are up-to-date - nothing to do"
fi

# Check for report_instance_data.sh and replace crontab
old_scriptname="report_instance_data.sh"
scriptname="command-runner"

msg "checking for report_instance_data.sh"
# Check if the old_scriptname exists in the crontab
if crontab -l | grep -q "$old_scriptname" ; then
    msg "Removing old cron entry for $old_scriptname..."
    crontab -l | grep -v "$old_scriptname" | crontab -
    
    if ! crontab -l | grep -q "$scriptname" ; then
        msg "Adding new cron entry for $scriptname..."
        entry1="0 23 * * * $cron_cmd > /dev/null 2>&1 ||:"
        (crontab -l && echo "$entry1") | crontab -
    else
        msg "$scriptname is already present in crontab. No changes made."
    fi

else
    msg "$old_scriptname not found in crontab. Continuing without changes..."
fi

if [[ "$last_github_sha" != "$github_sha" ]]; then
    msg "Updating scripts"
    for fname in $FILE_LIST; do
        [[ -f "$fname" ]] && mv "$fname" "$fname.bak"
        msg "downloading $fname"
        wget "$github_download_url/$fname"
        chmod +x "$fname"
    done
    echo "last_github_sha=$github_sha" > "$ConfigFile"
    echo "last_github_date=$github_date" >> "$ConfigFile"
    msg "Scripts updated"
else
    msg "Scripts are up-to-date - nothing to do"
fi
