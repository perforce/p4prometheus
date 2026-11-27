#!/bin/bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")/.." && pwd)"
test_dir=$(mktemp -d)
cleanup() {
    rm -f "$test_dir/bin/curl" "$test_dir/bin/wget" \
        "$test_dir/install/check_for_updates.sh" "$test_dir/install/check_for_updates.sh.bak" \
        "$test_dir/install/alpha.sh" "$test_dir/install/alpha.sh.bak" \
        "$test_dir/install/beta.py" "$test_dir/install/gamma.sh" "$test_dir/install/delta.sh" \
        "$test_dir/install/install_command-runner.sh" "$test_dir/install/install_command-runner.sh.bak" \
        "$test_dir/install/.update_config" "$test_dir/manifest" "$test_dir/tree" \
        "$test_dir/state" "$test_dir/calls" "$test_dir/output"
    rmdir "$test_dir/install/save" "$test_dir/install" "$test_dir/bin" "$test_dir"
}
trap cleanup EXIT
mkdir "$test_dir/bin" "$test_dir/install"
cp "$script_dir/check_for_updates.sh" "$test_dir/install/check_for_updates.sh"

cat > "$test_dir/bin/curl" <<'EOF'
#!/bin/bash
set -eu
output=
url=
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) output=$2; shift 2 ;;
        -*) shift ;;
        *) url=$1; shift ;;
    esac
done
printf '%s\n' "$url" >> "$MOCK_ROOT/calls"
[[ -z "$output" ]] || exec > "$output"
case "$url" in
    *'/commits?per_page=1'*)
        printf '[{"sha":"%s","commit":{"committer":{"date":"2026-01-01"}}}]\n' "$MOCK_COMMIT"
        ;;
    *'/git/commits/'*) printf '{"tree":{"sha":"tree"}}\n' ;;
    *'/git/trees/'*) cat "$MOCK_ROOT/tree" ;;
    *'/scripts/update_files.txt')
        [[ "${MOCK_FAIL_NAME:-}" != "update_files.txt" ]] || exit 22
        cat "$MOCK_ROOT/manifest"
        ;;
    *'/scripts/check_for_updates.sh') cat "$MOCK_SELF_SOURCE" ;;
    *'/scripts/'*)
        name=${url##*/}
        [[ "${MOCK_FAIL_NAME:-}" != "$name" ]] || exit 22
        printf 'content-%s-%s\n' "$MOCK_COMMIT" "$name"
        ;;
    *) exit 1 ;;
esac
EOF
cat > "$test_dir/bin/wget" <<'EOF'
#!/bin/bash
printf 'workshop script\n'
EOF
chmod +x "$test_dir/bin/curl" "$test_dir/bin/wget"

export PATH="$test_dir/bin:$PATH"
export MOCK_ROOT="$test_dir"
export MOCK_SELF_SOURCE="$script_dir/check_for_updates.sh"
export MOCK_COMMIT=revision1
export MOCK_FAIL_NAME=

make_tree() {
    jq -n --args '{truncated:false,tree:[$ARGS.positional[] | {path:("scripts/"+.),type:"blob",sha:("sha-"+.)}]}' \
        check_for_updates.sh "$@" > "$test_dir/tree"
}
run_checker() {
    /bin/bash "$test_dir/install/check_for_updates.sh" -c "$test_dir/state" > "$test_dir/output" 2>&1
}

make_tree update_files.txt alpha.sh beta.py
printf '# first manifest\nalpha.sh\nbeta.py\n' > "$test_dir/manifest"
printf 'last_github_sha=old\ngithub_file_sha_check_for_updates.sh=old\n' > "$test_dir/state"
printf 'old alpha\n' > "$test_dir/install/alpha.sh"
alpha_inode=$(find "$test_dir/install/alpha.sh" -prune -ls | awk '{print $1}')
run_checker
grep -Fxq 'content-revision1-alpha.sh' "$test_dir/install/alpha.sh"
[[ "$(find "$test_dir/install/alpha.sh" -prune -ls | awk '{print $1}')" == "$alpha_inode" ]]
grep -Fxq 'old alpha' "$test_dir/install/alpha.sh.bak"
grep -Fxq 'content-revision1-beta.py' "$test_dir/install/beta.py"
grep -Fxq 'last_github_sha=revision1' "$test_dir/state"
grep -Fxq 'github_file_sha_check_for_updates.sh=sha-check_for_updates.sh' "$test_dir/state"
[[ ! -e "$test_dir/install/.update_config" ]]
grep -Fq '/revision1/scripts/update_files.txt' "$test_dir/calls"
grep -Fq '/revision1/scripts/alpha.sh' "$test_dir/calls"

export MOCK_COMMIT=revision2
make_tree update_files.txt beta.py gamma.sh
printf 'beta.py\ngamma.sh\n' > "$test_dir/manifest"
run_checker
grep -Fxq 'content-revision2-gamma.sh' "$test_dir/install/gamma.sh"
grep -Fxq 'content-revision1-alpha.sh' "$test_dir/install/alpha.sh"
grep -Fxq 'last_github_sha=revision2' "$test_dir/state"
if grep -Fq 'github_file_sha_alpha.sh=' "$test_dir/state"; then
    echo "removed manifest entry still tracked" >&2
    exit 1
fi

export MOCK_COMMIT=revision3
make_tree update_files.txt beta.py gamma.sh
printf '../bad\n' > "$test_dir/manifest"
if run_checker; then
    echo "invalid manifest accepted" >&2
    exit 1
fi
grep -Fq 'Invalid filename' "$test_dir/output"
grep -Fxq 'last_github_sha=revision2' "$test_dir/state"

printf 'beta.py\nbeta.py\n' > "$test_dir/manifest"
if run_checker; then
    echo "duplicate manifest entry accepted" >&2
    exit 1
fi
grep -Fq 'Duplicate filename' "$test_dir/output"

printf '# no files\n' > "$test_dir/manifest"
if run_checker; then
    echo "empty manifest accepted" >&2
    exit 1
fi
grep -Fq 'contains no files' "$test_dir/output"

printf 'missing.sh\n' > "$test_dir/manifest"
if run_checker; then
    echo "missing manifest entry accepted" >&2
    exit 1
fi
grep -Fq 'Failed to find scripts/missing.sh' "$test_dir/output"

export MOCK_FAIL_NAME=update_files.txt
if run_checker; then
    echo "manifest download failure accepted" >&2
    exit 1
fi
grep -Fq 'Failed to download update_files.txt' "$test_dir/output"

export MOCK_FAIL_NAME=delta.sh
make_tree update_files.txt delta.sh
printf 'delta.sh\n' > "$test_dir/manifest"
printf 'keep existing\n' > "$test_dir/install/delta.sh"
if run_checker; then
    echo "file download failure accepted" >&2
    exit 1
fi
grep -Fxq 'keep existing' "$test_dir/install/delta.sh"
grep -Fxq 'last_github_sha=revision2' "$test_dir/state"

export MOCK_FAIL_NAME=
export MOCK_COMMIT=revision2
make_tree update_files.txt beta.py
printf 'beta.py\n' > "$test_dir/manifest"
printf 'last_github_sha=revision2\ngithub_file_sha_check_for_updates.sh=old\n' > "$test_dir/install/.update_config"
/bin/bash "$test_dir/install/check_for_updates.sh" > "$test_dir/output" 2>&1
grep -Fxq 'github_file_sha_check_for_updates.sh=sha-check_for_updates.sh' "$test_dir/install/.update_config"
grep -Fq 'Scripts are up-to-date' "$test_dir/output"

printf '{"truncated":true,"tree":[]}\n' > "$test_dir/tree"
if run_checker; then
    echo "incomplete GitHub tree accepted" >&2
    exit 1
fi
grep -Fq 'GitHub tree is incomplete' "$test_dir/output"

set --
while IFS= read -r filename || [[ -n "$filename" ]]; do
    [[ -z "$filename" || "$filename" == \#* ]] && continue
    set -- "$@" "$filename"
done < "$script_dir/update_files.txt"
make_tree "$@"
run_checker
grep -Fq 'using built-in file list' "$test_dir/output"
grep -Fq 'Scripts are up-to-date' "$test_dir/output"

echo "PASS: manifest updates, self-reexec, fallback and error handling"
