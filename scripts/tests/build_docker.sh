#!/bin/bash
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Build the Docker containers for P4Prometheus testing

# Usage Exaxmples:
#    build_docker_image.sh
#
# Goes together with run_docker_tests.sh
# This is provided as a useful tool for testing!

# We calculate dir relative to directory of script
script_dir="${0%/*}"
root_dir="$(cd "$script_dir/.."; pwd -P)"

# Set progress for docker build
export BUILDKIT_PROGRESS=plain

echo Building SDP docker containers
for image in nonsdp sdp; do
    docker_dir="$root_dir"
    dockerfile="${docker_dir}/docker/Dockerfile"
    # Build the base Docker for the OS, and then the SDP variant on top
    docker build --rm=true -t="perforce/p4promtest-${image}" --target p4promtest-${image} -f "${dockerfile}" "${docker_dir}"
done
