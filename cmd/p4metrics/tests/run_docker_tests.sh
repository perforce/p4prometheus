#!/bin/bash
# Run the docker tests - any parameters are passed to the inner script - assume they are pytest parameters

set -ux

args=""
if [ "$#" -ne 0 ]; then
    args="-test $@"
fi

script_dir="${0%/*}"
root_dir="$(cd "$script_dir/.."; pwd -P)"

# "$script_dir/build_docker.sh"

cname=p4metricstest
podman rm -f $cname
sleep 1

podman ps -q --filter name=$cname | grep -q . && podman kill $cname
podman run --cap-add=SYS_RESOURCE,AUDIT_WRITE -d --rm -v $root_dir:/p4metrics --name $cname base_$cname

sleep 1
podman exec -it $cname bash -x /p4metrics/docker/docker_run_p4metrics_tests.sh $args
