#!/bin/bash
# Docker entry point - intended to be run on the master only
# Installs a p4d installation
# Generates some test data
# Runs the benchmar scripts

function bail () { echo -e "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

# Ensure this script runs as perforce
OSUSER=perforce
if [[ $(id -u) -eq 0 ]]; then
   exec su - $OSUSER -c "$0 $*"
elif [[ $(id -u -n) != $OSUSER ]]; then
   echo "$0 can only be run by root or $OSUSER"
   exit 1
fi

# Change default server as installed by reset_sdp.sh
cd /p4/common/config
mv p4_1.vars p4_1.vars.old
cat p4_1.vars.old | sed -e 's/=helix/=master/' > p4_1.vars

# Start the server - some issues with doing sysctl in docker so we do it old way
/p4/1/bin/p4d_1_init start

# Set configurables - but without restarting server
. /p4/common/bin/p4_vars 1
p4 configure set server.depot.root=/p4/1/depots
p4 configure set journalPrefix=/p4/1/checkpoints/p4_1
p4 configure set track=1
p4 configure set track=1
p4 configure set rpl=4
p4 configure set monitor=2
p4 configure show

p4 server -o master.1 | sed -e "s/Services:\s*standard/Services:\tcommit-server/g" | p4 server -i

# Now run mkrep.sh
cp /p4/sdp/Server/Unix/p4/common/config/SiteTags.cfg /p4/common/config/

/p4/common/bin/mkrep.sh -i 1 -t edge -s bos -r replica_edge -p

cd /p4

ansible-playbook -i hosts install_sdp.yml
