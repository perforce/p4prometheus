#!/bin/bash
# This script sets up the docker container for use with nonsdp p4prometheus testing
# It expects to be run as root within the container.

cd /root

./setup_nonsdp.sh

p4dctl start -a

export P4PORT=1666
export P4USER=perforce

./install_p4prom.sh -nosdp -m /p4metrics -u perforce -m /p4metrics

config_file="/etc/p4prometheus/p4prometheus.yaml"
sed -i 's@log_path:.*@log_path: /opt/perforce/servers/test/log@' "$config_file"
sed -i 's@server_id:.*@server_id: test.server@' "$config_file"

# echo test.server > /opt/perforce/servers/test/server.id

# Need to restart as the config file won't have been valid
systemctl restart p4prometheus

sleep 1

p4 info
p4 depots

sleep 1

sudo -u perforce crontab -l > /tmp/c.out

# Extract the script to run and run it
script=$(grep monitor_metrics /tmp/c.out | sed -e 's@.*/etc@/etc@' | sed -e "s/ >.*//")
sudo -u perforce $script

# Restart where we can see output
systemctl stop node_exporter
nohup /usr/local/bin/node_exporter --collector.textfile.directory=/p4metrics > /tmp/node.out &

sleep 3
p4 configure show

py.test -v test_nosdp.py
