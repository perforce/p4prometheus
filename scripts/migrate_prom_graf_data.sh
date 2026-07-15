#!/bin/bash
# migrate_p4prom_data.sh
#
# Migrates the monitoring server's runtime data directories to a new base path.
# Reads the current install state from /etc/p4prometheus-monitoring/install.env
# (written by install_prom_graf.sh) and moves each component's data directory
# to <new_data_root>/<component>.
#
# Designed for enterprise use cases where:
#   - A dedicated volume (/data, /hxdata, etc.) is being added to an existing host
#   - The initial install used the default /var/lib location
#   - You want to migrate without a full reinstall
#
# Usage:
#   migrate_p4prom_data.sh -d <new_data_root> [--dry-run] [--cleanup-old]
#
# Flags:
#   -d <new_data_root>   REQUIRED. New base directory (e.g. /data or /hxdata)
#   --dry-run            Show what would be done; make no changes
#   --cleanup-old        After successful migration, remove old data directories
#                        (default: leave originals; safe to re-run)
#   -h, --help           Show this usage text
#
# Preflight checks performed:
#   - New data root is on a writable filesystem
#   - Sufficient free space at destination (current usage + 10% headroom)
#   - All affected services are present (will be stopped/started)
#   - No source/dest overlap
#
# Services stopped for migration (then restarted):
#   prometheus, alertmanager, victoria-metrics, grafana-server, pushgateway
#
# After migration:
#   - /etc/grafana/grafana.ini [paths.data] is updated
#   - Systemd service files are regenerated with new paths
#   - State file is updated with new DATA_ROOT
#   - Old directories are left intact (renamed with .migrated-YYYYMMDD suffix)
#     unless --cleanup-old is passed
#
# Recovery:
#   If this script is interrupted, old data is still in place.  Simply run
#   migrate_p4prom_data.sh again; it will skip components whose data has
#   already been moved.

set -euo pipefail

if [[ -z "${BASH_VERSINFO:-}" ]] || [[ "${BASH_VERSINFO[0]}" -lt 4 ]]; then
    echo "This script requires Bash version >= 4"; exit 1
fi

# ============================================================
# Defaults
# ============================================================

state_file="/etc/p4prometheus-monitoring/install.env"
new_data_root=""
dry_run=0
cleanup_old=0
today=$(date +%Y%m%d)

# ============================================================
# Helpers
# ============================================================

function msg     { echo -e "$*"; }
function info    { echo -e "INFO: $*"; }
function warn    { echo -e "WARN: $*" >&2; }
function error   { echo -e "ERROR: $*" >&2; exit 1; }
function dry_msg { echo -e "[DRY-RUN] $*"; }

function usage {
    sed -n '/^# Usage/,/^[^#]/{ /^[^#]/d; s/^# //; s/^#$//; p }' "$0"
    exit 0
}

# ============================================================
# Argument parsing
# ============================================================

while [[ $# -gt 0 ]]; do
    case "$1" in
        -d)             new_data_root="$2"; shift 2;;
        --dry-run)      dry_run=1; shift;;
        --cleanup-old)  cleanup_old=1; shift;;
        -h|--help)      usage;;
        *) error "Unknown argument: $1";;
    esac
done

[[ -n "$new_data_root" ]] || error "Required flag -d <new_data_root> not specified. See --help."
[[ "$dry_run" -eq 1 ]] && msg "\n*** DRY-RUN MODE — no changes will be made ***\n"

# ============================================================
# Load current state
# ============================================================

[[ -f "$state_file" ]] || error "State file not found: $state_file\nRun install_prom_graf.sh first, or specify the state file path."

function state_get {
    grep "^${1}=" "$state_file" 2>/dev/null | cut -d= -f2-
}

old_data_root=$(state_get DATA_ROOT)
bin_dir=$(state_get BIN_DIR)
retention_months=$(state_get RETENTION_MONTHS)

# Apply defaults if state file is missing values (older installs)
old_data_root="${old_data_root:-/var/lib}"
bin_dir="${bin_dir:-/usr/local/bin}"
retention_months="${retention_months:-6}"

# Detect whether pushgateway was installed
pushgateway_present=0
if systemctl list-units --all --type=service 2>/dev/null | grep -q 'pushgateway'; then
    pushgateway_present=1
fi

# ============================================================
# Sanity checks
# ============================================================

if [[ "$old_data_root" == "$new_data_root" ]]; then
    msg "Current data root and new data root are the same (${old_data_root}). Nothing to do."
    exit 0
fi

# Normalise paths (no trailing slash)
old_data_root="${old_data_root%/}"
new_data_root="${new_data_root%/}"

# ============================================================
# Component table
#   Each entry: service_name  sub_dir_under_data_root
# ============================================================

# Components to migrate (order matters: prometheus last — largest)
declare -A svc_subdir
svc_subdir["alertmanager"]="alertmanager"
svc_subdir["victoria-metrics"]="victoria-metrics"
svc_subdir["pushgateway"]="pushgateway"
svc_subdir["grafana-server"]="grafana"
svc_subdir["prometheus"]="prometheus"

# Only include pushgateway if installed
active_services=()
for svc in alertmanager victoria-metrics grafana-server prometheus; do
    active_services+=("$svc")
done
if [[ "$pushgateway_present" -eq 1 ]]; then
    active_services+=("pushgateway")
fi

# ============================================================
# Preflight
# ============================================================

msg "=== Preflight checks ==="

# Check we're running as root
if [[ "$EUID" -ne 0 ]]; then
    error "This script must be run as root (use sudo)."
fi

# Ensure destination parent exists or can be created
if [[ "$dry_run" -eq 0 ]]; then
    mkdir -p "$new_data_root" 2>/dev/null || true
fi
if [[ "$dry_run" -eq 0 ]] && [[ ! -d "$new_data_root" ]]; then
    error "Cannot create destination: $new_data_root"
fi
if [[ "$dry_run" -eq 0 ]] && [[ ! -w "$new_data_root" ]]; then
    error "Destination is not writable: $new_data_root"
fi
[[ "$dry_run" -eq 1 ]] && dry_msg "Would create/verify writable: $new_data_root"
info "Destination writable: $new_data_root"

# Collect sizes and check free space per unique destination filesystem
total_bytes_needed=0
for svc in "${active_services[@]}"; do
    sub="${svc_subdir[$svc]}"
    src="${old_data_root}/${sub}"
    if [[ -d "$src" ]]; then
        bytes=$(du -sb "$src" 2>/dev/null | awk '{print $1}')
        total_bytes_needed=$(( total_bytes_needed + bytes ))
        info "  $src: $(( bytes / 1024 / 1024 )) MB"
    fi
done

# 10% headroom
required=$(( total_bytes_needed * 11 / 10 ))

if [[ "$dry_run" -eq 0 ]]; then
    avail_bytes=$(df -B1 --output=avail "$new_data_root" 2>/dev/null | tail -1 | tr -d ' ')
    avail_bytes="${avail_bytes:-0}"
    if [[ "$avail_bytes" -lt "$required" ]]; then
        error "Insufficient space at $new_data_root.\n  Required:  $(( required / 1024 / 1024 )) MB\n  Available: $(( avail_bytes / 1024 / 1024 )) MB"
    fi
    info "Disk space OK: $(( avail_bytes / 1024 / 1024 )) MB available, $(( required / 1024 / 1024 )) MB required"
else
    dry_msg "Would check disk space. Total data: $(( total_bytes_needed / 1024 / 1024 )) MB, required with headroom: $(( required / 1024 / 1024 )) MB"
fi

# Check source/dest do not overlap
if [[ "$new_data_root" == "${old_data_root}"* ]] || [[ "$old_data_root" == "${new_data_root}"* ]]; then
    error "Source ($old_data_root) and destination ($new_data_root) paths must not overlap."
fi

# Verify systemctl is available (needed to stop/start services)
command -v systemctl >/dev/null 2>&1 || error "systemctl not found. This script requires systemd."

# Confirm with user unless in dry-run
if [[ "$dry_run" -eq 0 ]]; then
    msg ""
    msg "=== Summary of planned migration ==="
    msg "  Current data root: $old_data_root"
    msg "  New data root:     $new_data_root"
    msg "  Components:        ${active_services[*]}"
    msg "  Cleanup old dirs:  $([ "$cleanup_old" -eq 1 ] && echo "YES (--cleanup-old)" || echo "NO (originals renamed .migrated-${today})")"
    msg ""
    read -r -p "Proceed? Services will be stopped briefly. [y/N] " confirm
    [[ "${confirm,,}" == "y" ]] || { msg "Aborted."; exit 0; }
fi

msg ""
msg "=== Starting migration ==="

# ============================================================
# Helper: safely move a directory (handles cross-device)
# ============================================================

function move_dir {
    local src="$1"
    local dst="$2"
    if [[ ! -d "$src" ]]; then
        info "Source not found (skipping): $src"
        return 0
    fi
    if [[ -d "$dst" ]]; then
        info "Destination already exists (skipping move): $dst"
        return 0
    fi
    local dst_parent
    dst_parent=$(dirname "$dst")
    mkdir -p "$dst_parent"

    # Try atomic rename first (works on same filesystem)
    if mv "$src" "$dst" 2>/dev/null; then
        info "Moved $src → $dst (rename)"
    else
        # Cross-device: rsync then remove source
        if ! command -v rsync >/dev/null 2>&1; then
            error "rsync required for cross-device move but not found. Install rsync and retry."
        fi
        info "Cross-device move: rsync $src → $dst"
        rsync -aH --no-compress "$src/" "$dst/"
        info "rsync complete. Verifying file count..."
        src_count=$(find "$src" -type f | wc -l)
        dst_count=$(find "$dst" -type f | wc -l)
        if [[ "$src_count" -ne "$dst_count" ]]; then
            error "File count mismatch after rsync (src: $src_count, dst: $dst_count). Original data preserved at $src."
        fi
        info "  Verified: $dst_count files transferred."
    fi
}

# ============================================================
# Stop all affected services
# ============================================================

stopped_services=()

function stop_svc {
    local svc="$1"
    if systemctl is-active --quiet "$svc" 2>/dev/null; then
        if [[ "$dry_run" -eq 1 ]]; then
            dry_msg "Would stop: $svc"
        else
            msg "Stopping $svc..."
            systemctl stop "$svc"
            stopped_services+=("$svc")
        fi
    else
        info "$svc is already stopped"
    fi
}

for svc in "${active_services[@]}"; do
    stop_svc "$svc"
done

# ============================================================
# Move each component's data directory
# ============================================================

for svc in "${active_services[@]}"; do
    sub="${svc_subdir[$svc]}"
    src="${old_data_root}/${sub}"
    dst="${new_data_root}/${sub}"

    if [[ "$dry_run" -eq 1 ]]; then
        if [[ -d "$src" ]]; then
            dry_msg "Would move $src → $dst"
        else
            dry_msg "Source not present, would skip: $src"
        fi
        continue
    fi

    msg "Migrating $svc data: $src → $dst"
    move_dir "$src" "$dst"

    # Leave a breadcrumb at the old path so stale service configs don't silently fail
    if [[ ! -e "$src" ]] && [[ "$cleanup_old" -eq 0 ]]; then
        mkdir -p "${src}.migrated-${today}"
        echo "Data moved to ${dst} on ${today} by migrate_p4prom_data.sh" \
            > "${src}.migrated-${today}/README.txt"
        info "Breadcrumb left at: ${src}.migrated-${today}"
    fi
done

# ============================================================
# Update Grafana data path in grafana.ini
# ============================================================

grafana_ini="/etc/grafana/grafana.ini"
if [[ -f "$grafana_ini" ]]; then
    if [[ "$dry_run" -eq 1 ]]; then
        dry_msg "Would update $grafana_ini: paths.data → ${new_data_root}/grafana"
    else
        msg "Updating $grafana_ini with new data path..."
        # Update or insert paths.data
        if grep -q '^data\s*=' "$grafana_ini"; then
            sed -i "s|^data\s*=.*|data = ${new_data_root}/grafana|" "$grafana_ini"
        elif grep -q '^\[paths\]' "$grafana_ini"; then
            sed -i "/^\[paths\]/a data = ${new_data_root}/grafana" "$grafana_ini"
        else
            echo -e "\n[paths]\ndata = ${new_data_root}/grafana" >> "$grafana_ini"
        fi
        info "grafana.ini updated."
    fi
fi

# ============================================================
# Regenerate systemd service files with new data paths
# ============================================================

# Prometheus
prom_service="/etc/systemd/system/prometheus.service"
if [[ -f "$prom_service" ]]; then
    if [[ "$dry_run" -eq 1 ]]; then
        dry_msg "Would update $prom_service: storage.tsdb.path → ${new_data_root}/prometheus"
    else
        msg "Updating prometheus service file..."
        sed -i "s|--storage.tsdb.path [^ ]*|--storage.tsdb.path ${new_data_root}/prometheus/|" "$prom_service"
    fi
fi

# Alertmanager
am_service="/etc/systemd/system/alertmanager.service"
if [[ -f "$am_service" ]]; then
    if [[ "$dry_run" -eq 1 ]]; then
        dry_msg "Would update $am_service: storage.path → ${new_data_root}/alertmanager"
    else
        msg "Updating alertmanager service file..."
        sed -i "s|--storage.path=[^ ]*|--storage.path=${new_data_root}/alertmanager|" "$am_service"
    fi
fi

# VictoriaMetrics
vm_service="/etc/systemd/system/victoria-metrics.service"
if [[ -f "$vm_service" ]]; then
    if [[ "$dry_run" -eq 1 ]]; then
        dry_msg "Would update $vm_service: storageDataPath → ${new_data_root}/victoria-metrics"
    else
        msg "Updating victoria-metrics service file..."
        sed -i "s|-storageDataPath [^ ]*|-storageDataPath ${new_data_root}/victoria-metrics/|" "$vm_service"
    fi
fi

# Pushgateway
pg_service="/etc/systemd/system/pushgateway.service"
if [[ -f "$pg_service" ]]; then
    if [[ "$dry_run" -eq 1 ]]; then
        dry_msg "Would update $pg_service: persistence.file → ${new_data_root}/pushgateway"
    else
        msg "Updating pushgateway service file..."
        sed -i "s|--persistence.file=[^ ]*|--persistence.file=${new_data_root}/pushgateway/metric.store|" "$pg_service"
    fi
fi

# ============================================================
# Reload systemd and restart services
# ============================================================

if [[ "$dry_run" -eq 0 ]]; then
    msg "Reloading systemd daemon..."
    systemctl daemon-reload

    msg "Restarting services..."
    for svc in "${stopped_services[@]}"; do
        msg "  Starting $svc..."
        systemctl start "$svc"
    done
else
    dry_msg "Would reload systemd daemon and restart: ${active_services[*]}"
fi

# ============================================================
# Health checks
# ============================================================

if [[ "$dry_run" -eq 0 ]]; then
    msg ""
    msg "=== Health checks (20s wait for services to start) ==="
    sleep 20

    declare -A healthcheck_urls
    healthcheck_urls["prometheus"]="http://localhost:9090/-/healthy"
    healthcheck_urls["alertmanager"]="http://localhost:9093/-/healthy"
    healthcheck_urls["victoria-metrics"]="http://localhost:8428/health"
    healthcheck_urls["grafana-server"]="http://localhost:3000/api/health"
    healthcheck_urls["pushgateway"]="http://localhost:9091/-/healthy"

    all_healthy=1
    for svc in "${active_services[@]}"; do
        url="${healthcheck_urls[$svc]:-}"
        [[ -z "$url" ]] && continue
        if curl -sf --max-time 5 "$url" >/dev/null 2>&1; then
            info "$svc: healthy"
        else
            warn "$svc: health check FAILED at $url"
            all_healthy=0
        fi
    done

    if [[ "$all_healthy" -eq 0 ]]; then
        warn "One or more health checks failed. Check service logs (journalctl -u <service>)."
        warn "Old data is preserved at ${old_data_root}/*.migrated-${today}"
    fi
fi

# ============================================================
# Clean up old directories if requested
# ============================================================

if [[ "$cleanup_old" -eq 1 ]] && [[ "$dry_run" -eq 0 ]]; then
    msg ""
    msg "=== Cleaning up old data directories ==="
    for svc in "${active_services[@]}"; do
        sub="${svc_subdir[$svc]}"
        old_dir="${old_data_root}/${sub}"
        breadcrumb="${old_dir}.migrated-${today}"
        if [[ -d "$breadcrumb" ]]; then
            msg "Removing $breadcrumb"
            rm -rf "$breadcrumb"
        fi
    done
elif [[ "$cleanup_old" -eq 1 ]] && [[ "$dry_run" -eq 1 ]]; then
    dry_msg "Would remove old data directories (--cleanup-old)"
fi

# ============================================================
# Update state file
# ============================================================

function update_state_file {
    local tmp
    tmp=$(mktemp)
    # Rewrite state file with updated DATA_ROOT; preserve all other keys
    while IFS= read -r line; do
        if [[ "$line" =~ ^DATA_ROOT= ]]; then
            echo "DATA_ROOT=${new_data_root}"
        else
            echo "$line"
        fi
    done < "$state_file" > "$tmp"
    mv "$tmp" "$state_file"
    chmod 644 "$state_file"
    info "State file updated: DATA_ROOT=${new_data_root}"
}

if [[ "$dry_run" -eq 1 ]]; then
    dry_msg "Would update $state_file: DATA_ROOT=${new_data_root}"
else
    msg ""
    msg "=== Updating state file ==="
    update_state_file
fi

# ============================================================
# Done
# ============================================================

msg ""
if [[ "$dry_run" -eq 1 ]]; then
    msg "=== Dry run complete. Re-run without --dry-run to apply changes. ==="
else
    msg "=== Migration complete ==="
    msg ""
    msg "  Data moved to: $new_data_root"
    msg "  Old paths:     retained with .migrated-${today} suffix (safe to delete)"
    msg "  State file:    $state_file"
    msg ""
    msg "To remove old data once you've confirmed the migration is healthy, run:"
    msg "  $0 -d $new_data_root --cleanup-old"
fi
