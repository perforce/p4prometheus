#!/bin/bash
#------------------------------------------------------------------------------
set -u

#------------------------------------------------------------------------------
# Run the Docker tests for the SDP

# Usage Exaxmples:
#    build_docker_image.sh
#    build_docker_image.sh ubuntu20
#    build_docker_image.sh centos7 ubuntu20
#    build_docker_image.sh all
#    build_docker_image.sh ALL
#
# Goes together with build_docker_image.sh
#
# Note that this file is expecting to be mapped into the root of the workspace
# and with the sdp directory in the same root.
#
# So workspace view should look something like:
#    View:
#        //guest/perforce_software/sdp/main/... //myws.sdp/sdp/...

declare oses=

# This file should be in <workspace-root>/sdp/test/
# We calculate dir relative to directory of script
script_dir="${0%/*}"
root_dir="$(cd "$script_dir/../.."; pwd -P)"

if [[ "${1:-Unset}" == "Unset" ]]; then
   # Default is currently CentOS 7 only.
   oses="centos7"
else
   for os in $(echo $* | tr ',' ' '); do
      case "$os" in
         (all) oses="centos7 rocky8 ubuntu20";;
         (ALL) oses="centos6 centos7 rocky8 ubuntu20";;
         (ubuntu20) oses+"=ubuntu20";;
         (centos7) oses+="centos7";;
         (rocky8) oses+="rocky8";;
         
         (*)
            echo "Warning: Unknown OS [$os]."
            oses+="$os"
         ;;
      esac
   done
fi

# Directory where test output is put by the container
# Easier to make it under sdp which is a mounted volume
test_output_dir="$script_dir/output"
[[ -d "$test_output_dir" ]] || mkdir "$test_output_dir"
all_test_output="$test_output_dir/alltests.out"
if [[ -f "$all_test_output" ]]; then
   rm "$all_test_output"
fi

echo Running SDP tests
tests_failed=0
for os in $oses
do
   test_output="$test_output_dir/test-${os}.out"
   if [[ -f $test_output ]]; then
       rm "$test_output"
   fi
   docker_dir="$root_dir/sdp/test/docker"
   dockerfile_base="${docker_dir}/Dockerfile.${os}.base"
   dockerfile_sdp="${docker_dir}/Dockerfile.${os}.sdp"
   # Build the base Docker for the OS, and then the SDP variant on top
   docker build --rm=true -t="perforce/${os}-base" -f "${dockerfile_base}" "${docker_dir}"
   docker build --rm=true -t="perforce/${os}-sdp" -f "${dockerfile_sdp}" "${docker_dir}"
   # Run the Docker image, mounting the /sdp directory within it. The SDP image
   # has a default RUN command which is configured within it.
   # We don't directly stop on error but process a little later below so that nice
   # messages are written to Jenkins console output.
    set +e
    echo "docker run --rm  -v ${PWD}/sdp:/sdp -e TESTOS=${os} perforce/${os}-sdp"
   docker run --rm  -v "${PWD}/sdp:/sdp" -e "TESTOS=${os}" "perforce/${os}-sdp"
    tests_failed=$?
    set -e
   echo "$os" >> "$all_test_output"
   # Avoid Jenkins immediately failing job without letting us cat the output
   set +e
   cat "$test_output" >> "$all_test_output"
   set -e
   if [[ "$tests_failed" -ne 0 ]]; then
      break
   fi 
done
cat "$all_test_output"

exit "$tests_failed"
