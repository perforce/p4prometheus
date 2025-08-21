#!/bin/bash
# Run the docker tests

set -ux

script_dir="${0%/*}"
root_dir="$(cd "$script_dir/.."; pwd -P)"

# "$script_dir/build_docker.sh"

cname=p4metricstest
podman rm -f $cname
sleep 1

podman ps -q --filter name=$cname | grep -q . && podman kill $cname
podman run --cap-add=SYS_RESOURCE,AUDIT_WRITE -d --rm -v $root_dir:/p4metrics --name $cname base_$cname

sleep 1
podman exec -it $cname bash -xv /p4metrics/docker/docker_run_p4metrics_tests.sh
