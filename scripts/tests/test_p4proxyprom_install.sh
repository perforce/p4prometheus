#!/bin/bash
set -euo pipefail

# shellcheck source=install_p4proxyprom.sh
source "$(dirname "$0")/../install_p4proxyprom.sh"
test_dir=$(mktemp -d)
trap 'rm -f "$test_dir/assets/p4proxyprometheus.linux-amd64.gz" "$test_dir/assets/node_exporter-1.12.1.linux-amd64.tar.gz" "$test_dir/node_exporter-1.12.1.linux-amd64/node_exporter" "$test_dir/bin/p4proxyprometheus" "$test_dir/bin/node_exporter" "$test_dir/config/p4proxyprometheus.yaml" "$test_dir/config/p4proxyprom_install.env" "$test_dir/systemd/p4proxyprometheus.service" "$test_dir/systemd/node_exporter.service" "$test_dir/calls" "$test_dir/proxy"; rmdir "$test_dir/assets" "$test_dir/node_exporter-1.12.1.linux-amd64" "$test_dir/bin" "$test_dir/config" "$test_dir/other-config" "$test_dir/systemd" "$test_dir/other-systemd" "$test_dir/metrics" "$test_dir"' EXIT
mkdir "$test_dir/assets" "$test_dir/node_exporter-1.12.1.linux-amd64" "$test_dir/bin" "$test_dir/config" "$test_dir/other-config" "$test_dir/systemd" "$test_dir/other-systemd" "$test_dir/metrics"
proxy_systemd_dir="$test_dir/systemd"
calls="$test_dir/calls"

id() {
    case "$*" in
        -u) echo 0 ;;
        -gn*) echo testgroup ;;
        node_exporter) return 0 ;;
        *) return 1 ;;
    esac
}
stat() { echo 0:600; }
uname() { echo x86_64; }
runuser() { return 0; }
sleep() { :; }
systemctl() {
    printf '%s\n' "$*" >> "$calls"
    if [[ $1 == show ]]; then
        if [[ $4 == node_exporter.service ]]; then
            echo "${mock_node_load_state:-not-found}"
        else
            echo not-found
        fi
    fi
}

printf '#!/bin/sh\nexit 0\n' > "$test_dir/proxy"
gzip -c "$test_dir/proxy" > "$test_dir/assets/p4proxyprometheus.linux-amd64.gz"
cp "$test_dir/proxy" "$test_dir/node_exporter-1.12.1.linux-amd64/node_exporter"
tar -czf "$test_dir/assets/node_exporter-1.12.1.linux-amd64.tar.gz" -C "$test_dir" node_exporter-1.12.1.linux-amd64/node_exporter

proxy_main install --version 1.2.3 --instance proxy1 --p4p-log /p4/logs/p4p.log \
    -u perforce -m "$test_dir/metrics" -c "$test_dir/config" -b "$test_dir/bin" \
    --local-tarballs-dir "$test_dir/assets"
grep -Fq "metrics_output: \"$test_dir/metrics/p4_proxy\"" "$test_dir/config/p4proxyprometheus.yaml"
grep -Fq "sdp_instance: \"proxy1\"" "$test_dir/config/p4proxyprometheus.yaml"
grep -Fq -- "--collector.textfile.directory=$test_dir/metrics" "$test_dir/systemd/node_exporter.service"
grep -Fq -- "--config=$test_dir/config/p4proxyprometheus.yaml" "$test_dir/systemd/p4proxyprometheus.service"
[[ -x "$test_dir/bin/p4proxyprometheus" && -x "$test_dir/bin/node_exporter" ]]

echo '# local configuration' >> "$test_dir/config/p4proxyprometheus.yaml"
echo '# local unit' >> "$test_dir/systemd/p4proxyprometheus.service"
proxy_main update --version 1.2.4 -c "$test_dir/config" --local-tarballs-dir "$test_dir/assets"
grep -Fxq '# local configuration' "$test_dir/config/p4proxyprometheus.yaml"
grep -Fxq '# local unit' "$test_dir/systemd/p4proxyprometheus.service"
grep -Fxq 'stop p4proxyprometheus node_exporter' "$calls"

: > "$calls"
printf 'invalid gzip' > "$test_dir/assets/p4proxyprometheus.linux-amd64.gz"
if proxy_main update --version 1.2.5 -c "$test_dir/config" --local-tarballs-dir "$test_dir/assets"; then
    echo "Expected corrupt asset to fail" >&2
    exit 1
fi
if grep -q '^stop ' "$calls"; then
    echo "Stopped services before validating assets" >&2
    exit 1
fi

mock_node_load_state=loaded
proxy_systemd_dir="$test_dir/other-systemd"
if proxy_main install --version 1.2.3 --instance proxy1 --p4p-log /p4/logs/p4p.log \
    -c "$test_dir/other-config" --local-tarballs-dir "$test_dir/assets"; then
    echo "Expected existing service to be rejected" >&2
    exit 1
fi

SELinuxEnabled=1
semanage() { return "${mock_selinux_failure:-0}"; }
restorecon() { :; }
proxy_label_binary "$test_dir/bin/p4proxyprometheus"
mock_selinux_failure=1
if (proxy_label_binary "$test_dir/bin/p4proxyprometheus") >/dev/null 2>&1; then
    echo "Expected SELinux registration failure" >&2
    exit 1
fi
echo "PASS: install, update preservation, corrupt asset, existing service and SELinux failure"
