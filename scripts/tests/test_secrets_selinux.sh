#!/bin/bash
set -euo pipefail

source "$(dirname "$0")/../p4prom_common.sh"
test_dir=$(mktemp -d)
trap 'rm -f "$test_dir/real.config/p4metrics.env" "$test_dir/real.config/p4monitor_locks.env" "$test_dir/link" "$test_dir/calls"; rmdir "$test_dir/real.config" "$test_dir"' EXIT
mkdir "$test_dir/real.config"
ln -s "$test_dir/real.config" "$test_dir/link"
resolved_dir=$(readlink -f "$test_dir/real.config")
calls="$test_dir/calls"

chown() { :; }
semanage() {
    printf 'semanage %s\n' "$*" >> "$calls"
    if [[ $2 == -a && ${existing_rule:-0} == 1 ]]; then
        return 1
    fi
    return "${register_status:-0}"
}
restorecon() {
    printf 'restorecon %s\n' "$*" >> "$calls"
    return "${restore_status:-0}"
}

for component in p4metrics p4monitor_locks; do
    secrets_file="$test_dir/link/$component.env"
    ensure_function="ensure_${component}_secrets_file_exists"
    SELinuxEnabled=1
    : > "$calls"
    "$ensure_function" "$secrets_file"
    [[ -f "$secrets_file" ]]
    [[ $(find "$resolved_dir/$component.env" -perm 0600 -print) == "$resolved_dir/$component.env" ]]
    [[ $(wc -l < "$calls" | tr -d ' ') == 2 ]]
    grep -Fx "restorecon -vF $resolved_dir/$component.env" "$calls"
    escaped_file=$(printf '%s\n' "$resolved_dir/$component.env" | sed 's/[][\\.^$*+?(){}|]/\\&/g')
    grep -Fx "semanage fcontext -a -t etc_t $escaped_file" "$calls"

    printf 'EXISTING=value\n' > "$secrets_file"
    existing_rule=1
    : > "$calls"
    "$ensure_function" "$secrets_file"
    [[ $(cat "$secrets_file") == EXISTING=value ]]
    [[ $(wc -l < "$calls" | tr -d ' ') == 3 ]]
    grep -Fx "semanage fcontext -m -t etc_t $escaped_file" "$calls"

    restore_status=1
    if ("$ensure_function" "$secrets_file") > /dev/null 2>&1; then
        bail "Relabel failure was ignored"
    fi
    restore_status=0
    register_status=1
    if ("$ensure_function" "$secrets_file") > /dev/null 2>&1; then
        bail "Registration failure was ignored"
    fi
    register_status=0
    existing_rule=0

    SELinuxEnabled=0
    : > "$calls"
    "$ensure_function" "$secrets_file"
    [[ ! -s "$calls" ]]
    rm "$secrets_file"
    "$ensure_function" "$secrets_file"
    [[ -f "$secrets_file" && ! -s "$calls" ]]
done
echo "PASS: new/existing secrets, resolved and escaped paths, existing rules, disabled SELinux, and failures"
