#!/bin/bash
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Build the Docker containers for P4prometheus testing - now using podman

# Usage Exaxmples:
#    build_docker_image.sh
#    build_docker_image.sh ubuntu20
#    build_docker_image.sh centos7 ubuntu20
#    build_docker_image.sh all
#    build_docker_image.sh ALL
#
# Goes together with run_docker_tests.sh
# This is provided as a useful tool for testing!
#
# Note that this file is expecting to be mapped into the root of the workspace
# and with the sdp directory in the same root.
# So workspace view should look something like:
#    View:
#        //guest/perforce_software/sdp/main/... //myws.sdp/sdp/...
#        //guest/perforce_software/sdp/main/test/* //myws.sdp/*

declare oses=

# This file should be in <workspace-root>/scripts/
# We calculate dir relative to directory of script
script_dir="${0%/*}"
root_dir="$(cd "$script_dir/../.."; pwd -P)"

if [[ "${1:-Unset}" == "Unset" ]]; then
   # Default is currently CentOS 7 only.
   oses="centos7"
else
   for os in $(echo $* | tr ',' ' '); do
      case "$os" in
         (all) oses="rocky8 ubuntu20";;
         (ALL) oses="rocky8 ubuntu20";;
         (ubuntu20) oses+="ubuntu20";;
         (centos7) oses+="centos7";;
         (rocky8) oses+="rocky8";;
         
         (*)
            echo "Warning: Unknown OS [$os]."
            oses+="$os"
         ;;
      esac
   done
fi

echo Building SDP docker containers
for os in $oses; do
    docker_dir="$root_dir/sdp/test/docker"
    dockerfile_base="${docker_dir}/Dockerfile.${os}.base"
    dockerfile_sdp="${docker_dir}/Dockerfile.${os}.sdp"
    # Build the base Docker for the OS, and then the SDP variant on top
    podman build --rm=true -t="perforce/${os}-base" -f "${dockerfile_base}" "${docker_dir}"
    podman build --rm=true -t="perforce/${os}-sdp" -f "${dockerfile_sdp}" "${docker_dir}"
done
