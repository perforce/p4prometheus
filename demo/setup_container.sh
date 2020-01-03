#!/bin/bash
# This script sets up the base docker container for use with SDP testing.
# It expects to be run as root within the container.

# Create base directories for use by SDP (see mkdirs.sh)
mkdir /hxdepots
mkdir /hxmetadata
mkdir /hxlogs

# Create Perforce group and user within that group, and allow them sudo privileges
groupadd perforce
useradd -d /p4 -s /bin/bash -m perforce -g perforce
echo 'perforce ALL=(ALL) NOPASSWD:ALL'> /tmp/perforce
chmod 0440 /tmp/perforce
chown root:root /tmp/perforce
mv /tmp/perforce /etc/sudoers.d
echo perforce:Password | chpasswd

#
# Helpful profile for perforce user login profile - for manual testing mainly
#
BASH_PROF=/p4/.bash_profile
cat <<"EOF" >$BASH_PROF
export PATH=/sdp/Server/Unix/p4/common/bin:$PATH
export P4CONFIG=.p4config
export P4P4PORT=1666
PS1='\u@\h:\w$ '
EOF
chown perforce:perforce $BASH_PROF

