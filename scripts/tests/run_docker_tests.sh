#!/bin/bash
# Run the docker tests

script_dir="${0%/*}"

"$script_dir/build_docker.sh"

docker run -it perforce/p4promtest-nonsdp /root/run_install.sh -nosdp
docker run -it perforce/p4promtest-sdp /root/run_install.sh