#!/bin/bash

set -euo pipefail

script_dir="${0%/*}"

"$script_dir/build_docker.sh" -rules
podman run --rm perforce/p4promrulestest