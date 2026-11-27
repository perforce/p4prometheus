#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=install_p4proxyprom.sh
source "$SCRIPT_DIR/install_p4proxyprom.sh"
proxy_main update "$@"
