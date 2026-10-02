#!/bin/bash
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Build the Docker containers for P4Prometheus testing - but using podman for systemd support

# Usage Examples:
#    build_docker.sh
#    build_docker.sh -prom-graf
#    build_docker.sh -rules
#
# Goes together with run_docker_tests.sh
# This is provided as a useful tool for testing!

# We calculate p4prometheus project root dir relative to directory of script
script_dir="${0%/*}"
root_dir="$(cd "$script_dir/.."; pwd -P)"

# Set progress for docker build
export BUILDKIT_PROGRESS=plain

build_prom_graf=0
build_rules=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        -prom-graf)
            build_prom_graf=1
            ;;
        -rules)
            build_rules=1
            ;;
        -h|--help)
            echo "Usage: $0 [-prom-graf|-rules]"
            echo "  -prom-graf   Build perforce/p4promgraftest (target: p4promgraftest)"
            echo "  -rules       Build perforce/p4promrulestest (target: p4promrulestest)"
            echo "  default      Build perforce/p4promtest-sdp (target: p4promtest-sdp)"
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Usage: $0 [-prom-graf|-rules]" >&2
            exit 1
            ;;
    esac
    shift
done

repo_dir="$(cd "$root_dir/.." && pwd -P)"
dockerfile="${root_dir}/docker/Dockerfile"

if [[ $build_rules -eq 1 ]]; then
    echo "Building Prometheus rules test container"
    podman build --rm=true -t="perforce/p4promrulestest" --target p4promrulestest -f "${dockerfile}" "${repo_dir}"
elif [[ $build_prom_graf -eq 1 ]]; then
    echo "Building Prometheus/Grafana podman/docker container"
    podman build --rm=true -t="perforce/p4promgraftest" --target p4promgraftest -f "${dockerfile}" "${repo_dir}"
else
    echo "Building SDP podman/docker container"
    podman build --rm=true -t="perforce/p4promtest-sdp" --target p4promtest-sdp -f "${dockerfile}" "${repo_dir}"
fi
