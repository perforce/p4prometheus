#!/bin/bash
# check_for_updates.sh
# 
# Checks github repo for script updates, and downloads them if available.
#
# Uses the github API and stores a local file with current status.
#

repo_path="scripts"
github_url="https://api.github.com/repos/perforce/p4prometheus/commits?per_page=1&path=$repo_path"
github_download_url="https://raw.githubusercontent.com/perforce/p4prometheus"
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

Depends on 'curl', 'jq' and 'wget' being in the path.
"
}

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

# Wrapped in a function: bash must fully parse this block (up to the matching
# closing brace) before running it, so the update loop below can safely
# overwrite this very script file without corrupting the running process.
main() {
    local -a original_args=("$@")
    local -a FILE_LIST=(install_p4prom.sh update_p4prom.sh install_p4proxyprom.sh update_p4proxyprom.sh p4prom_common.sh monitor_locks.py p4monitor_locks.sh README.md get_volume_info.sh create_dashboard.py dashboard.yaml upload_grafana_dashboard.sh)
    WORKSHOP_SCRIPT_LIST="install_command-runner.sh"
    DEPRECATED_FILE_LIST="push_metrics.sh report_instance_data.sh monitor_metrics.sh monitor_metrics.py monitor_wrapper.sh"

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

    for f in curl jq wget; do
        command -v "$f" 2> /dev/null || bail "Failed to find $f in path"
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
    github_tree=$(curl -fsSL "https://api.github.com/repos/perforce/p4prometheus/git/trees/$github_tree_sha?recursive=1") || bail "Failed to fetch GitHub tree"
    jq -e '.truncated == false and (.tree | type == "array")' <<< "$github_tree" >/dev/null || bail "GitHub tree is incomplete or invalid"
    github_scripts_url="$github_download_url/$github_sha/scripts"

    # Update and restart first so the remainder runs from the current script version.
    self_name="check_for_updates.sh"
    self_remote_sha=$(jq -r --arg path "scripts/$self_name" '.tree[] | select(.path == $path and .type == "blob") | .sha' <<< "$github_tree")
    [[ -n "$self_remote_sha" && "$self_remote_sha" != "null" ]] || bail "Failed to find scripts/$self_name in GitHub tree"
    self_local_sha=$(awk -F= -v key="github_file_sha_${self_name}" '$1 == key { print substr($0, length(key) + 2) }' "$ConfigFile" 2>/dev/null)
    if [[ "$self_remote_sha" != "$self_local_sha" ]]; then
        self_temp=$(mktemp) || bail "Failed to create temporary file for $self_name"
        msg "downloading $self_name"
        if ! curl -fsSL -o "$self_temp" "$github_scripts_url/$self_name" || ! bash -n "$self_temp"; then
            rm -f "$self_temp"
            bail "Failed to download a valid $self_name"
        fi
        if [[ -f "$self_name" ]] && ! cp "$self_name" "$self_name.bak"; then
            rm -f "$self_temp"
            bail "Failed to back up $self_name"
        fi
        if ! cat "$self_temp" > "$self_name"; then
            rm -f "$self_temp"
            bail "Failed to write $self_name"
        fi
        rm -f "$self_temp"
        chmod +x "$self_name" || bail "Failed to set permissions on $self_name"
        config_temp=$(mktemp) || bail "Failed to create temporary state file"
        if [[ -f "$ConfigFile" ]] && ! awk -F= -v key="github_file_sha_${self_name}" '$1 != key' "$ConfigFile" > "$config_temp"; then
            rm -f "$config_temp"
            bail "Failed to read $ConfigFile"
        fi
        if ! echo "github_file_sha_${self_name}=$self_remote_sha" >> "$config_temp" || ! cat "$config_temp" > "$ConfigFile"; then
            rm -f "$config_temp"
            bail "Failed to update $ConfigFile"
        fi
        rm -f "$config_temp"
        exec /bin/bash "$SCRIPT_DIR/$self_name" ${original_args[@]+"${original_args[@]}"} || bail "Failed to re-execute $self_name"
    fi

    manifest_name="update_files.txt"
    manifest_sha=$(jq -r --arg path "scripts/$manifest_name" '.tree[] | select(.path == $path and .type == "blob") | .sha' <<< "$github_tree")
    if [[ -n "$manifest_sha" ]]; then
        manifest=$(curl -fsSL "$github_scripts_url/$manifest_name") || bail "Failed to download $manifest_name"
        FILE_LIST=()
        seen_names=" "
        while IFS= read -r fname || [[ -n "$fname" ]]; do
            fname=${fname%$'\r'}
            [[ -z "$fname" || "$fname" == \#* ]] && continue
            [[ "$fname" =~ ^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$ && "$fname" != "$self_name" ]] || bail "Invalid filename in $manifest_name: $fname"
            [[ "$seen_names" != *" $fname "* ]] || bail "Duplicate filename in $manifest_name: $fname"
            seen_names+="$fname "
            FILE_LIST+=("$fname")
        done <<< "$manifest"
        [[ ${#FILE_LIST[@]} -gt 0 ]] || bail "$manifest_name contains no files"
    else
        msg "No $manifest_name in GitHub tree; using built-in file list"
    fi

    for fname in "${FILE_LIST[@]}"; do
        remote_sha=$(jq -r --arg path "scripts/$fname" '.tree[] | select(.path == $path and .type == "blob") | .sha' <<< "$github_tree")
        [[ -n "$remote_sha" && "$remote_sha" != "null" ]] || bail "Failed to find scripts/$fname in GitHub tree"
    done

    mkdir -p save || bail "Failed to create save directory"
    for fname in $DEPRECATED_FILE_LIST; do
        if [[ -f "$fname" ]]; then
            msg "Removing deprecated file $fname"
            mv "$fname" "save/$fname" || bail "Failed to save deprecated file $fname"
        fi
    done

    # For the sake of SELinux and systemd timers, we need to avoid changing attributes for the file (ls -alZ)
    # Thus we overwrite the existing file (having saved a copy) - as that keeps attributes
    if [[ "$last_github_sha" != "$github_sha" ]]; then
        msg "Updating scripts"
        for fname in "${FILE_LIST[@]}"; do
            remote_sha=$(jq -r --arg path "scripts/$fname" '.tree[] | select(.path == $path and .type == "blob") | .sha' <<< "$github_tree")
            local_sha=$(awk -F= -v key="github_file_sha_${fname}" '$1 == key { print substr($0, length(key) + 2) }' "$ConfigFile" 2>/dev/null)
            if [[ "$remote_sha" == "$local_sha" ]]; then
                msg "unchanged $fname"
                continue
            fi
            msg "downloading $fname"
            file_temp=$(mktemp) || bail "Failed to create temporary file for $fname"
            if ! curl -fsSL -o "$file_temp" "$github_scripts_url/$fname"; then
                rm -f "$file_temp"
                bail "Failed to download $fname"
            fi
            if [[ -f "$fname" ]] && ! cp "$fname" "$fname.bak"; then
                rm -f "$file_temp"
                bail "Failed to back up $fname"
            fi
            if ! cat "$file_temp" > "$fname"; then
                rm -f "$file_temp"
                bail "Failed to write $fname"
            fi
            rm -f "$file_temp"
            chmod +x "$fname" || bail "Failed to set permissions on $fname"
        done
        config_temp=$(mktemp) || bail "Failed to create temporary state file"
        if ! printf 'last_github_sha=%s\nlast_github_date=%s\ngithub_file_sha_%s=%s\n' \
            "$github_sha" "$github_date" "$self_name" "$self_remote_sha" > "$config_temp"; then
            rm -f "$config_temp"
            bail "Failed to write temporary state file"
        fi
        for fname in "${FILE_LIST[@]}"; do
            remote_sha=$(jq -r --arg path "scripts/$fname" '.tree[] | select(.path == $path and .type == "blob") | .sha' <<< "$github_tree")
            if ! printf 'github_file_sha_%s=%s\n' "$fname" "$remote_sha" >> "$config_temp"; then
                rm -f "$config_temp"
                bail "Failed to write temporary state file"
            fi
        done
        if ! cat "$config_temp" > "$ConfigFile"; then
            rm -f "$config_temp"
            bail "Failed to update $ConfigFile"
        fi
        rm -f "$config_temp"
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
