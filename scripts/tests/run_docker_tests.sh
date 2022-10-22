#!/bin/bash
# Run the docker tests

script_dir="${0%/*}"

"$script_dir/build_docker.sh"

docker run -it perforce/p4prom-nonsdp-base /root/run_install.sh
