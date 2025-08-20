#!/bin/bash
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Build the Docker containers for p4metrics testing - but using podman for systemd support

# Usage Examples:
#    build_docker.sh
#
# Goes together with run_docker_tests.sh
# This is provided as a useful tool for testing!

# We calculate dir relative to directory of script
script_dir="${0%/*}"
root_dir="$(cd "$script_dir/../../.."; pwd -P)"

# Set progress for docker build
export BUILDKIT_PROGRESS=plain

echo Building SDP podman/docker containers
# for image in sdp no-p4; do
for image in sdp; do
    docker_dir="$root_dir"
    dockerfile="${docker_dir}/cmd/p4metrics/docker/Dockerfile"
    # Build the base Docker for the OS, and then the SDP variant on top
    podman build --rm=true -t="perforce/p4metricstest-${image}" --target p4metricstest-${image} -f "${dockerfile}" "${docker_dir}"
done
