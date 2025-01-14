#!/bin/bash
# Run the docker tests

set -ux

script_dir="${0%/*}"

"$script_dir/build_docker.sh"

cname=p4promtest
podman rm -f $cname
sleep 1

# docker run -it perforce/p4promtest-nonsdp /root/run_p4prom_tests.sh -nosdp
podman run --cap-add=SYS_RESOURCE,AUDIT_WRITE -d --rm --name $cname perforce/p4promtest-sdp
sleep 1
podman exec -it $cname /root/run_p4prom_tests.sh
