#!/bin/bash
# Run the docker tests for prometheus and grafana
# Goes together with build_docker.sh in the same directory, which builds the Docker containers for testing.
# This is provided as a useful tool for testing!
set -ux

script_dir="${0%/*}"

"$script_dir/build_docker.sh" -prom-graf

cname=p4promgraftest
podman rm -f $cname
sleep 1

podman ps -q --filter name=$cname | grep -q . && podman kill $cname
podman run --cap-add=SYS_RESOURCE,AUDIT_WRITE -d --rm -p 127.0.0.1:3000:3000 --name $cname perforce/p4promgraftest
sleep 1
podman exec -it $cname /root/run_prom_graf_tests.sh
