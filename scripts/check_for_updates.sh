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
workshop_url="https://swarm.workshop.perforce.com/downloads/guest/perforce_software/command-runner"

# Just in case you want to customize this
local_bin_dir=/usr/local/bin

function msg () { echo -e "$*"; }
function bail () { msg "\nError: ${1:-Unknown Error}\n"; exit "${2:-1}"; }

function usage
{
   declare errorMessage=${1:-Unset}
 
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

# Wrapped in a function: bash must fully parse this block (up to the matching
# closing brace) before running it, so the update loop below can safely
# overwrite this very script file without corrupting the running process.
main() {
    FILE_LIST="install_p4prom.sh update_p4prom.sh p4prom_common.sh monitor_metrics.py monitor_wrapper.sh check_for_updates.sh get_volume_info.sh create_dashboard.py dashboard.yaml upload_grafana_dashboard.sh"
    WORKSHOP_SCRIPT_LIST="install_command-runner.sh"
    DEPRECATED_FILE_LIST="push_metrics.sh report_instance_data.sh monitor_metrics.sh"

    # Command Line Processing
    
    declare -i shiftArgs=0
    ConfigFile=".update_config"

    set +u
    while [[ $# -gt 0 ]]; do
        case $1 in
            (-h) usage && exit 0;;
            # (-man) usage -man;;
            (-c) ConfigFile=$2; shiftArgs=1;;
            (-*) usage "Unknown command line option ($1)." && exit 1;;
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

    for f in curl jq; do
        command -v $f 2> /dev/null || bail "Failed to find $f in path"
    done

    last_github_sha=""
    last_github_date=""

    if [[ -e "$ConfigFile" ]]; then
        last_github_sha=$(grep last_github_sha "$ConfigFile" | cut -d= -f2)
        last_github_date=$(grep last_github_date "$ConfigFile" | cut -d= -f2)
    fi

    github_sha=$(curl -fsSL "$github_url" | jq -r '.[0].sha')
    github_date=$(curl -fsSL "$github_url" | jq -r '.[0].commit.committer.date')
    [[ -n "$github_sha" && "$github_sha" != "null" ]] || bail "Failed to determine latest GitHub commit"

    github_tree_sha=$(curl -fsSL "https://api.github.com/repos/perforce/p4prometheus/git/commits/$github_sha" | jq -r '.tree.sha')
    [[ -n "$github_tree_sha" && "$github_tree_sha" != "null" ]] || bail "Failed to determine GitHub tree for $github_sha"
    github_tree=$(curl -fsSL "https://api.github.com/repos/perforce/p4prometheus/git/trees/$github_tree_sha?recursive=1")

    mkdir -p save
    for fname in $DEPRECATED_FILE_LIST; do
        if [[ -f "$fname" ]]; then
            msg "Removing deprecated file $fname"
            mv "$fname" "save/$fname"
        fi
    done

    # For the sake of SELinux and systemd timers, we need to avoid changing attributes for the file (ls -alZ)
    # Thus we overwrite the existing file (having saved a copy) - as that keeps attributes
    if [[ "$last_github_sha" != "$github_sha" ]]; then
        msg "Updating scripts"
        for fname in $FILE_LIST; do
            remote_sha=$(jq -r --arg path "scripts/$fname" '.tree[] | select(.path == $path) | .sha' <<< "$github_tree")
            [[ -n "$remote_sha" && "$remote_sha" != "null" ]] || bail "Failed to find scripts/$fname in GitHub tree"
            local_sha=$(grep "^github_file_sha_${fname}=" "$ConfigFile" 2>/dev/null | cut -d= -f2-)
            if [[ "$remote_sha" == "$local_sha" ]]; then
                msg "unchanged $fname"
                continue
            fi
            [[ -f "$fname" ]] && cp "$fname" "$fname.bak"
            msg "downloading $fname"
            wget -O - "$github_download_url/$fname" > "$fname"
            chmod +x "$fname"
        done
        echo "last_github_sha=$github_sha" > "$ConfigFile"
        echo "last_github_date=$github_date" >> "$ConfigFile"
        for fname in $FILE_LIST; do
            remote_sha=$(jq -r --arg path "scripts/$fname" '.tree[] | select(.path == $path) | .sha' <<< "$github_tree")
            echo "github_file_sha_${fname}=$remote_sha" >> "$ConfigFile"
        done
        msg "Scripts updated"

        for fname in $WORKSHOP_SCRIPT_LIST; do
            [[ -f "$fname" ]] && cp "$fname" "$fname.bak"
            msg "downloading $fname"
            wget -O - "$workshop_url/scripts/$fname" > "$fname"
            chmod +x "$fname"
        done

    else
        msg "Scripts are up-to-date - nothing to do"
    fi
}

main "$@"
