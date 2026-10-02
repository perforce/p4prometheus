#!/bin/bash

set -euo pipefail

cp perforce_rules.yml /tmp/perforce_rules.yml.expected
cp custom_perforce_rules.yml /tmp/custom_perforce_rules.yml.expected
make render
cmp -s /tmp/perforce_rules.yml.expected perforce_rules.yml
cmp -s /tmp/custom_perforce_rules.yml.expected custom_perforce_rules.yml
promtool check rules perforce_rules.yml custom_perforce_rules.yml