#!/bin/bash -xv
# Intended to be run on monitor server only
# Does complete setup via ansible etc

function bail () { echo -e "\nError: ${1:-Unknown Error}\n"; exit ${2:-1}; }

# Ensure this script runs as perforce
OSUSER=perforce
if [[ $(id -u) -eq 0 ]]; then
   exec su - $OSUSER -c "$0 $*"
elif [[ $(id -u -n) != $OSUSER ]]; then
   echo "$0 can only be run by root or $OSUSER"
   exit 1
fi

sudo /usr/sbin/sshd -D &

cd /p4
ansible-playbook -i hosts -v install_prometheus.yml

ansible-playbook -i hosts -v install_p4prometheus.yml

while true
do
    sleep 60
done
