#!/bin/bash
# Install or update p4proxyprometheus and node_exporter on a proxy host.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=p4prom_common.sh
source "$SCRIPT_DIR/p4prom_common.sh"

proxy_systemd_dir=/etc/systemd/system
proxy_node_version=1.12.1

proxy_usage() {
    cat <<'EOF'
Usage:
  install_p4proxyprom.sh --version <version> --instance <id> --p4p-log <path> [-u <user>] [-m <metrics-dir>] [-c <config-dir>] [-b <bin-dir>] [--local-tarballs-dir <dir>]
  update_p4proxyprom.sh --version <version> [-c <config-dir>] [--local-tarballs-dir <dir>]

Install one proxy collector and node_exporter as systemd services. The proxy
user defaults to perforce, metrics to /var/metrics, config to /etc/p4prometheus,
and binaries to /usr/local/bin. Custom metrics and binary directories must
already exist. The version is the p4prometheus release version without 'v'.
Air-gap mode expects p4proxyprometheus.linux-<arch>.gz and
node_exporter-1.12.1.linux-<arch>.tar.gz in the specified directory.

Updates preserve configuration and service units, using the paths recorded by
the installer. Existing node_exporter or proxy services are not overwritten.
EOF
}

proxy_write_units() {
    cat > "$proxy_systemd_dir/p4proxyprometheus.service" <<EOF
[Unit]
Description=Perforce proxy Prometheus metrics
After=network-online.target
Wants=network-online.target

[Service]
User=$proxy_user
Group=$proxy_group
ExecStart=$proxy_bin_dir/p4proxyprometheus --config=$proxy_config_dir/p4proxyprometheus.yaml
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
EOF
    cat > "$proxy_systemd_dir/node_exporter.service" <<EOF
[Unit]
Description=Node Exporter
After=network-online.target
Wants=network-online.target

[Service]
User=node_exporter
ExecStart=$proxy_bin_dir/node_exporter --collector.systemd --collector.systemd.unit-include=node_exporter.service --collector.textfile.directory=$proxy_metrics_dir
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
EOF
}

proxy_label_binary() {
    local binary=$1 resolved pattern
    [[ $SELinuxEnabled -eq 1 ]] || return 0
    resolved=$(readlink -f "$binary") || bail "Cannot resolve binary path: $binary"
    pattern=$(printf '%s\n' "$resolved" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
    if ! semanage fcontext -a -t bin_t "$pattern" 2>/dev/null; then
        semanage fcontext -m -t bin_t "$pattern" || bail "Cannot register SELinux context for $resolved"
    fi
    restorecon -vF "$resolved" || bail "Cannot restore SELinux context for $resolved"
}

proxy_main() (
    local mode=$1
    shift
    local version="" instance="" p4p_log="" local_tarballs_dir="" config_dir=/etc/p4prometheus
    local proxy_user=perforce proxy_metrics_dir=/var/metrics proxy_bin_dir=/usr/local/bin
    local proxy_config_dir proxy_group proxy_arch state_file work_dir asset node_archive load_state binary staged
    local SELinuxEnabled=0

    while [[ $# -gt 0 ]]; do
        case "$1" in
            -h|--help) proxy_usage; return 0 ;;
            --version|--instance|--p4p-log|-u|-m|-c|-b|--local-tarballs-dir)
                [[ $# -ge 2 && -n "$2" ]] || bail "Missing value for $1"
                case "$1" in
                    --version) version=$2 ;;
                    --instance) [[ $mode == install ]] || bail "$1 is only valid for installation"; instance=$2 ;;
                    --p4p-log) [[ $mode == install ]] || bail "$1 is only valid for installation"; p4p_log=$2 ;;
                    -u) [[ $mode == install ]] || bail "$1 is only valid for installation"; proxy_user=$2 ;;
                    -m) [[ $mode == install ]] || bail "$1 is only valid for installation"; proxy_metrics_dir=$2 ;;
                    -c) config_dir=$2 ;;
                    -b) [[ $mode == install ]] || bail "$1 is only valid for installation"; proxy_bin_dir=$2 ;;
                    --local-tarballs-dir) local_tarballs_dir=$2 ;;
                esac
                shift 2 ;;
            *) bail "Unknown option: $1" ;;
        esac
    done
    [[ $(id -u) -eq 0 ]] || bail "Run this script as root"
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]] || bail "Specify a release version without 'v' using --version"
    [[ "$config_dir" =~ ^/[a-zA-Z0-9_./-]+$ ]] || bail "Config directory must be an absolute path without spaces"
    proxy_config_dir=$config_dir
    state_file="$proxy_config_dir/p4proxyprom_install.env"

    if [[ $mode == update ]]; then
        [[ -f "$state_file" && ! -L "$state_file" ]] || bail "Install state not found: $state_file"
        [[ $(stat -c '%u:%a' "$state_file") == 0:600 ]] || bail "Install state must be owned by root and mode 600: $state_file"
        # shellcheck disable=SC1090
        source "$state_file"
        [[ "$proxy_config_dir" == "$config_dir" ]] || bail "Install state has an unexpected config directory"
        [[ -f "$proxy_config_dir/p4proxyprometheus.yaml" ]] || bail "Proxy configuration not found"
        [[ -f "$proxy_systemd_dir/p4proxyprometheus.service" && -f "$proxy_systemd_dir/node_exporter.service" ]] || bail "Installed service units not found"
    else
        [[ "$instance" =~ ^[a-zA-Z0-9_-]+$ ]] || bail "Specify an instance identifier using --instance"
        [[ "$p4p_log" =~ ^/[a-zA-Z0-9_./-]+$ ]] || bail "Specify an absolute proxy log path without spaces using --p4p-log"
        [[ ! -e "$state_file" && ! -L "$state_file" && ! -e "$proxy_config_dir/p4proxyprometheus.yaml" && ! -L "$proxy_config_dir/p4proxyprometheus.yaml" ]] || bail "Proxy configuration or install state already exists; use update_p4proxyprom.sh"
        for service in p4proxyprometheus node_exporter; do
            [[ ! -e "$proxy_systemd_dir/$service.service" && ! -L "$proxy_systemd_dir/$service.service" ]] || bail "$service.service already exists; refusing to overwrite it"
            load_state=$(systemctl show --property=LoadState --value "$service.service") || bail "Cannot inspect $service.service"
            [[ "$load_state" == not-found ]] || bail "$service.service already exists; refusing to overwrite it"
        done
    fi

    for path in "$proxy_bin_dir" "$proxy_metrics_dir"; do
        [[ "$path" =~ ^/[a-zA-Z0-9_./-]+$ ]] || bail "Paths must be absolute and contain no spaces: $path"
    done
    proxy_group=$(id -gn "$proxy_user") || bail "Proxy OS user does not exist: $proxy_user"
    [[ "$proxy_user" =~ ^[a-zA-Z0-9_.-]+$ && "$proxy_group" =~ ^[a-zA-Z0-9_.-]+$ ]] || bail "User and group names must not contain spaces"
    preflight_check_dirs -b "$proxy_bin_dir" /usr/local/bin -m "$proxy_metrics_dir" /var/metrics
    [[ -d "$proxy_bin_dir" ]] || bail "Binary directory does not exist: $proxy_bin_dir"
    if [[ $mode == install ]]; then
        for binary in p4proxyprometheus node_exporter; do
            [[ ! -e "$proxy_bin_dir/$binary" && ! -L "$proxy_bin_dir/$binary" ]] || bail "$proxy_bin_dir/$binary already exists; refusing to overwrite it"
        done
    fi
    if [[ $mode == install && ! -d "$proxy_metrics_dir" ]]; then
        install -d -o "$proxy_user" -g "$proxy_group" -m 755 "$proxy_metrics_dir"
    fi
    [[ -d "$proxy_metrics_dir" ]] || bail "Metrics directory does not exist: $proxy_metrics_dir"
    runuser -u "$proxy_user" -- test -w "$proxy_metrics_dir" || bail "$proxy_user cannot write to $proxy_metrics_dir"
    if [[ $mode == install && -e "$p4p_log" ]]; then
        runuser -u "$proxy_user" -- test -r "$p4p_log" || bail "$proxy_user cannot read $p4p_log"
    fi

    case "$(uname -m)" in
        x86_64) proxy_arch=amd64 ;;
        aarch64|arm64) proxy_arch=arm64 ;;
        *) bail "Unsupported architecture: $(uname -m)" ;;
    esac
    if command -v getenforce >/dev/null && [[ $(getenforce) == Enforcing ]]; then
        SELinuxEnabled=1
    fi
    if [[ -z "$local_tarballs_dir" ]]; then
        command -v wget >/dev/null || bail "wget is required for downloads"
    else
        [[ "$local_tarballs_dir" == /* && -d "$local_tarballs_dir" ]] || bail "Air-gap directory must be an existing absolute path"
    fi
    work_dir=$(mktemp -d) || bail "Cannot create temporary directory"
    asset="p4proxyprometheus.linux-${proxy_arch}.gz"
    node_archive="node_exporter-${proxy_node_version}.linux-${proxy_arch}.tar.gz"
    trap 'rm -f -- "$work_dir/$asset" "$work_dir/$node_archive" "$work_dir/p4proxyprometheus" "$work_dir/node_exporter"; rmdir "$work_dir"' EXIT
    (
        cd "$work_dir"
        download_gz "$asset" "https://github.com/perforce/p4prometheus/releases/download/v${version}/${asset}"
        download_gz "$node_archive" "https://github.com/prometheus/node_exporter/releases/download/v${proxy_node_version}/${node_archive}"
        gzip -dc "$asset" > p4proxyprometheus || exit 1
        tar -xOzf "$node_archive" "node_exporter-${proxy_node_version}.linux-${proxy_arch}/node_exporter" > node_exporter || exit 1
        chmod 755 p4proxyprometheus node_exporter || exit 1
        ./p4proxyprometheus --version >/dev/null || exit 1
        ./node_exporter --version >/dev/null || exit 1
    ) || bail "Failed to prepare release binaries in $work_dir"

    if [[ $mode == install ]]; then
        if ! id node_exporter >/dev/null 2>&1; then
            useradd --no-create-home --shell /bin/false node_exporter
        fi
    fi
    runuser -u node_exporter -- test -r "$proxy_metrics_dir" || bail "node_exporter cannot read $proxy_metrics_dir"
    runuser -u node_exporter -- test -x "$proxy_metrics_dir" || bail "node_exporter cannot access $proxy_metrics_dir"
    if [[ $mode == update ]]; then
        systemctl stop p4proxyprometheus node_exporter
    else
        mkdir -p "$proxy_config_dir"
        cat > "$proxy_config_dir/p4proxyprometheus.yaml" <<EOF
p4p_log: "$p4p_log"
metrics_output: "$proxy_metrics_dir/p4_proxy"
sdp_instance: "$instance"
poll_interval_seconds: 1
read_all: false
fail_on_missing_logfile: false
EOF
        chmod 644 "$proxy_config_dir/p4proxyprometheus.yaml"
        proxy_write_units
        ( umask 077; printf 'proxy_user=%q\nproxy_group=%q\nproxy_metrics_dir=%q\nproxy_bin_dir=%q\nproxy_config_dir=%q\n' \
            "$proxy_user" "$proxy_group" "$proxy_metrics_dir" "$proxy_bin_dir" "$proxy_config_dir" > "$state_file" )
    fi
    for binary in p4proxyprometheus node_exporter; do
        staged=$(mktemp "$proxy_bin_dir/.$binary.XXXXXX") || bail "Cannot stage $binary in $proxy_bin_dir"
        if ! install -m 755 "$work_dir/$binary" "$staged" || ! mv -f "$staged" "$proxy_bin_dir/$binary"; then
            rm -f "$staged"
            bail "Failed to install $binary"
        fi
        proxy_label_binary "$proxy_bin_dir/$binary"
    done
    systemd_enable_and_restart "$proxy_systemd_dir/node_exporter.service" node_exporter
    systemd_enable_and_restart "$proxy_systemd_dir/p4proxyprometheus.service" p4proxyprometheus
    echo "Proxy configuration: $proxy_config_dir/p4proxyprometheus.yaml"
    echo "Metrics directory: $proxy_metrics_dir"
)

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    proxy_main install "$@"
fi
