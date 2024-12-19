#!/bin/bash
# This script sets up the base docker container for use with SDP testing.
# It expects to be run as root within the container.

set -ux

# Create base directories for use by SDP (see mkdirs.sh)
mkdir /hxdepots /hxmetadata /hxlogs

mkdir /root/sdp_install
cd /root/sdp_install
curl -L -O https://swarm.workshop.perforce.com/download/guest/perforce_software/sdp/main/Server/Unix/setup/install_sdp.sh
chmod +x install_sdp.sh

./install_sdp.sh -C > sdp_install.cfg
./install_sdp.sh -c sdp_install.cfg -init -demo -no_pkgs -y

#
# Helpful profile for perforce user login profile - for manual testing mainly
#
BASH_PROF=/home/perforce/.bash_profile
cat <<"EOF" >$BASH_PROF
export PATH=/sdp/Server/Unix/p4/common/bin:$PATH
export P4CONFIG=.p4config
export P4PORT=1666
source ~/.bashrc
PS1='\u@\h:\w$ '
EOF
echo "source /p4/common/bin/p4_vars 1" >> /home/perforce/.bashrc
chown perforce:perforce $BASH_PROF /home/perforce/.bashrc

sudo su - perforce -c "p4d -Gc"
