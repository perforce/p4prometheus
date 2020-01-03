#!/bin/bash
# This script sets up ssh for use within container

mkdir /p4/.ssh

mv /tmp/insecure_ssh_key.pub /p4/.ssh/authorized_keys
mv /tmp/insecure_ssh_key /p4/.ssh/id_rsa

cat << EOF > /p4/.ssh/config
Host *
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  User perforce
  LogLevel QUIET
EOF

chown -R perforce:perforce /p4/.ssh

chmod 700 /p4/.ssh
chmod 644 /p4/.ssh/authorized_keys
chmod 400 /p4/.ssh/id_rsa
chmod 400 /p4/.ssh/config
