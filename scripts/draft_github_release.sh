#!/usr/bin/env bash
# Build p4prometheus and p4metrics release assets, then create or update a GitHub draft release.

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: draft_github_release.sh <tag> [--dry-run] [--skip-build]

Builds the p4prometheus and p4metrics distribution binaries for the supplied
release tag, then creates a GitHub draft release and uploads the assets.

Arguments:
  <tag>          Release tag in vX.Y.Z form, for example v0.11.5.

Options:
  --dry-run      Build and list assets, but do not call GitHub.
  --skip-build   Reuse existing distribution assets instead of rebuilding.
  -h, --help     Show this help text.

The script requires a clean tracked worktree and an authenticated GitHub CLI.
If a draft release already exists for the tag, its assets are replaced.
EOF
}

fail() {
    printf 'Error: %s\n' "$*" >&2
    exit 1
}

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
cd "$repo_root"

tag=""
dry_run=false
skip_build=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)
            dry_run=true
            ;;
        --skip-build)
            skip_build=true
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        -*)
            usage >&2
            fail "Unknown option: $1"
            ;;
        *)
            [[ -z "$tag" ]] || fail "Only one release tag may be supplied"
            tag="$1"
            ;;
    esac
    shift
done

[[ -n "$tag" ]] || {
    usage >&2
    fail "Release tag is required"
}
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]] || \
    fail "Release tag must use vX.Y.Z form: $tag"

git diff --quiet || fail "Tracked worktree changes must be committed before releasing"
git diff --cached --quiet || fail "Staged changes must be committed before releasing"

if git rev-parse -q --verify "refs/tags/${tag}" >/dev/null; then
    tag_commit=$(git rev-parse "${tag}^{commit}")
    head_commit=$(git rev-parse HEAD)
    [[ "$tag_commit" == "$head_commit" ]] || \
        fail "Existing tag $tag does not point at HEAD"
fi

if [[ "$skip_build" == false ]]; then
    make VERSION="$tag" dist
    make -C cmd/p4metrics VERSION="$tag" dist
fi

assets=(
    bin/p4prometheus.linux-amd64.gz
    bin/p4prometheus.linux-arm64.gz
    bin/p4prometheus.windows-amd64.exe.gz
    bin/p4prometheus.darwin-amd64.gz
    bin/p4prometheus.darwin-arm64.gz
    cmd/p4metrics/bin/p4metrics.linux-amd64.gz
    cmd/p4metrics/bin/p4metrics.linux-arm64.gz
    cmd/p4metrics/bin/p4metrics.windows-amd64.exe.gz
    cmd/p4metrics/bin/p4metrics.darwin-amd64.gz
    cmd/p4metrics/bin/p4metrics.darwin-arm64.gz
)

for asset in "${assets[@]}"; do
    [[ -s "$asset" ]] || fail "Missing or empty release asset: $asset"
done

printf 'Release assets for %s:\n' "$tag"
printf '  %s\n' "${assets[@]}"

if [[ "$dry_run" == true ]]; then
    printf 'Dry run: GitHub release was not created or updated.\n'
    exit 0
fi

command -v gh >/dev/null 2>&1 || fail "GitHub CLI (gh) is required"
gh auth status --hostname github.com >/dev/null 2>&1 || \
    fail "Authenticate GitHub CLI first with: gh auth login --hostname github.com"

if gh release view "$tag" >/dev/null 2>&1; then
    is_draft=$(gh release view "$tag" --json isDraft --jq '.isDraft')
    [[ "$is_draft" == true ]] || fail "Release $tag already exists and is not a draft"
    gh release upload "$tag" "${assets[@]}" --clobber
else
    gh release create "$tag" "${assets[@]}" \
        --draft \
        --target HEAD \
        --title "$tag" \
        --generate-notes
fi

printf 'Draft release %s is ready for review.\n' "$tag"
