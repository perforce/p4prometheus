#!/bin/bash
# Run the docker tests just for p4metrics
# It expects that the run_docker_tests.sh script has been run first to setup the env within the container.

set -ux

script_dir="${0%/*}"
parent_dir="$(cd "$script_dir/.."; pwd -P)"

cname=p4metricstest

# podman cp $parent_dir/bin/p4metrics.linux-arm64.gz $cname:/tmp/
podman exec -it $cname /p4metrics/docker/docker_p4metrics_tests.sh
