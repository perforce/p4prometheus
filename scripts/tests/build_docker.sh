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

echo Building SDP docker containers
for dfile in nonsdp; do
    docker_dir="$root_dir"
    dockerfile_base="${docker_dir}/docker/Dockerfile.${dfile}"
    # Build the base Docker for the OS, and then the SDP variant on top
    docker build --rm=true -t="perforce/p4prom-${dfile}-base" -f "${dockerfile_base}" "${docker_dir}"
done
