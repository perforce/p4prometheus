#!/bin/bash
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Build the Docker containers for P4Prometheus testing - but using podman for systemd support

# Usage Examples:
#    build_docker.sh
#    build_docker.sh -prom-graf
#
# Goes together with run_docker_tests.sh
# This is provided as a useful tool for testing!

# We calculate p4prometheus project root dir relative to directory of script
script_dir="${0%/*}"
root_dir="$(cd "$script_dir/.."; pwd -P)"

# Set progress for docker build
export BUILDKIT_PROGRESS=plain

build_prom_graf=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        -prom-graf)
            build_prom_graf=1
            ;;
        -h|--help)
            echo "Usage: $0 [-prom-graf]"
            echo "  -prom-graf   Build perforce/p4promgraftest (target: p4promgraftest)"
            echo "  default      Build perforce/p4promtest-sdp (target: p4promtest-sdp)"
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Usage: $0 [-prom-graf]" >&2
            exit 1
            ;;
    esac
    shift
done

docker_dir="$root_dir"
dockerfile="${docker_dir}/docker/Dockerfile"

if [[ $build_prom_graf -eq 1 ]]; then
    echo "Building Prometheus/Grafana podman/docker container"
    podman build --rm=true -t="perforce/p4promgraftest" --target p4promgraftest -f "${dockerfile}" "${docker_dir}"
else
    echo "Building SDP podman/docker container"
    podman build --rm=true -t="perforce/p4promtest-sdp" --target p4promtest-sdp -f "${dockerfile}" "${docker_dir}"
fi
