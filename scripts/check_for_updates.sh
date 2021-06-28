#!/bin/bash
# check_for_updates.sh
# 
# Checks github repo for script updates, and downloads them if available.
#
# Uses the github API and stores a local file with current status.
#

repo_path="scripts"
github_url="https://api.github.com/repos/perforce/p4prometheus/commits?per_page=1&path=$repo_path"
github_download_url="https://raw.githubusercontent.com/perforce/p4prometheus/master/scripts"

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

function usage
{
   declare style=${1:--h}
   declare errorMessage=${2:-Unset}
 
   if [[ "$errorMessage" != Unset ]]; then
      echo -e "\\n\\nUsage Error:\\n\\n$errorMessage\\n\\n" >&2
   fi
 
   echo "USAGE for check_for_updates.sh:
 
check_for_updates.sh -c <config_file>
 
   or
 
check_for_updates.sh -h
"
}

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

# Command Line Processing
 
declare -i shiftArgs=0
ConfigFile=".update_config"

set +u
while [[ $# -gt 0 ]]; do
    case $1 in
        (-h) usage -h;;
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

cd "$SCRIPT_DIR" || bail "Can't cd to $SCRIPT_DIR"

# Check for dependencies

curl=$(which curl)
[[ $? -eq 0 ]] || bail "Failed to find curl in path"
jq=$(which jq)
[[ $? -eq 0 ]] || bail "Failed to find jq in path"

last_github_sha=""
last_github_date=""

if [[ -e "$ConfigFile" ]]; then
    last_github_sha=$(grep last_github_sha "$ConfigFile" | cut -d= -f2)
    last_github_date=$(grep last_github_date "$ConfigFile" | cut -d= -f2)
fi

github_sha=$(curl "$github_url" | jq '.[] | .sha')
github_date=$(curl "$github_url" | jq '.[] | .commit.committer.date')

if [[ "$last_github_sha" != "$github_sha" ]]; then
    msg "Updating scripts"
    for fname in monitor_metrics.sh monitor_metrics.py monitor_wrapper.sh push_metrics.sh; do
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
