#!/bin/bash
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Build the Docker containers for p4metrics/p4prometheus scripts testing - but using podman for systemd support

# Usage Examples:
#    build_docker.sh
#
# Goes together with run_docker_tests.sh in the same directory, which runs the tests in the built containers.
# This is provided as a useful tool for testing!

# We calculate dir relative to directory of script - the root of p4prometheus project.
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
    set -x
    podman build --rm=true -t="perforce/p4metricstest-${image}" --target p4metricstest-${image} -f "${dockerfile}" "${docker_dir}"
    cname=p4metricstest
    podman run --cap-add=SYS_RESOURCE,AUDIT_WRITE -d --rm -v $root_dir/cmd/p4metrics:/p4metrics --name $cname perforce/p4metricstest-${image}
    podman exec -it $cname bash -xv /p4metrics/docker/docker_setup_p4metrics_tests.sh
    # Having executed the setup script, we commit the container to a new image so that it can be used for testing without 
    # having to re-run the setup script each time.
    podman commit $cname base_$cname
    podman kill $cname
done
