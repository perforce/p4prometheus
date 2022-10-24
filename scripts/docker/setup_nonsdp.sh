#!/bin/bash
# This script sets up the docker container for use with nonsdp p4prometheus testing
# It expects to be run as root within the container.

# Create base directories for use by SDP (see mkdirs.sh)
mkdir /p4metrics
chmod 755 /p4metrics

# Allow perforce user sudo privileges
echo 'perforce ALL=(ALL) NOPASSWD:ALL'> /tmp/perforce
chmod 0440 /tmp/perforce
chown root:root /tmp/perforce
mv /tmp/perforce /etc/sudoers.d/
echo perforce:Password | chpasswd

#
# Helpful profile for perforce user login profile - for manual testing mainly
#
BASH_PROF=/opt/perforce/.bash_profile
cat <<"EOF" >$BASH_PROF
export PATH=/sdp/Server/Unix/p4/common/bin:$PATH
export P4CONFIG=.p4config
export P4P4PORT=1666
PS1='\u@\h:\w$ '
EOF
chown perforce:perforce $BASH_PROF

server_root=/opt/perforce/servers/test
mkdir -p $server_root
chown perforce:perforce $server_root
echo "test.server" > "$server_root/server.id"

cat <<"EOF" > /etc/perforce/p4dctl.conf.d/test.conf
p4d test
{
   Owner          =        perforce
   Execute        =        /opt/perforce/sbin/p4d
   Enabled        =        true
   Environment
   {
      P4ROOT      =        /opt/perforce/servers/test
      P4JOURNAL   =        journal
      P4LOG       =        log
      P4PORT      =        1666
      PATH        =        /bin:/usr/bin:/usr/local/bin:/opt/perforce/sbin
   }
}
EOF
