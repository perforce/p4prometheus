#!/bin/bash
# This script sets up the base docker container for use with SDP testing.
# It expects to be run as root within the container.

# Create base directories for use by SDP (see mkdirs.sh)
mkdir /hxdepots /hxmetadata /hxlogs

# Create Perforce group and user within that group, and allow them sudo privileges
groupadd perforce
useradd -d /home/perforce -s /bin/bash -m perforce -g perforce
echo 'perforce ALL=(ALL) NOPASSWD:ALL'> /tmp/perforce
chmod 0440 /tmp/perforce
chown root:root /tmp/perforce
mv /tmp/perforce /etc/sudoers.d
echo perforce:Password | chpasswd

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

chown -R perforce:perforce /hx*

# Following copied from https://github.com/perforce/helix-core-sdp-packer/blob/develop/scripts/10-perforce_install-sdp.sh

cd /hxdepots/

wget https://swarm.workshop.perforce.com/projects/perforce-software-sdp/download/downloads/sdp.Unix.tgz
tar -zxvf sdp.Unix.tgz
chown -R perforce:perforce sdp

cd sdp/helix_binaries

# Adjust platform if required
# declare Platform=linux26x86_64
arch=$(uname -m)
if [[ "$arch" = "aarch64" ]]; then
    get_script="get_helix_binaries.sh"
    mv "${get_script}" "${get_script}.bak"
    cat "${get_script}.bak" | sed -e 's/declare Platform=linux26x86_64/declare Platform=linux26aarch64/' > "${get_script}"
    chmod +x "${get_script}"
    chown perforce: "${get_script}"
fi
# Need to hard-code 24.1 for now to get ARM support
sudo -u perforce ./get_helix_binaries.sh -r r24.1

cd /hxdepots/sdp/Server/Unix/setup

cp mkdirs.cfg mkdirs.1.cfg
chmod +w mkdirs.1.cfg

sed -i "s/P4ADMINPASS=adminpass/P4ADMINPASS=F@stSCM!/" mkdirs.1.cfg
sed -i "s/SSL_PREFIX=ssl:/SSL_PREFIX=/" mkdirs.1.cfg
sed -i "s/P4MASTERHOST=.*/P4MASTERHOST=localhost/" mkdirs.1.cfg

./mkdirs.sh 1

cp /hxdepots/sdp/Server/Unix/setup/systemd/p4d_1.service /etc/systemd/system/
chmod 644 /etc/systemd/system/p4d_1.service

systemctl enable p4d_1
systemctl start p4d_1

cd /hxdepots/sdp/Server/setup
sudo -u perforce ./configure_new_server.sh 1

# TODO: the following two perforce account modifications should be in the helix installer
# Allow crontab for perforce
echo perforce >> /etc/cron.allow

# disable the perforce password from expiring otherwise crontab for the perforce user will stop working
chage -E -1 -I -1 -m 0 -M -1 perforce

echo "Configuration of Perforce is complete."

# # TODO: the case checker trigger needs credentials from a user where the creds do not expire
# # this should be handled by the helix installer
# # I am purposly putting this logic here so that the unlimited group will be available for both case sensitive and case insensitive
# # if it is anywhere else and the case is changed the group would be removed
# cat <<EOT > /tmp/unlimited.cfg
# Group:  unlimited
# MaxResults:     unset
# MaxScanRows:    unset
# MaxLockTime:    unset
# MaxOpenFiles:   unset
# Timeout:        unlimited
# PasswordTimeout:        unlimited
# Subgroups:
# Owners:
# Users:    
#     perforce
# EOT
# cat /tmp/unlimited.cfg | sudo -i -u perforce p4 group -i

# cat <<EOT > /tmp/superusers.cfg
# Group:  SuperUsers
# MaxResults:     unset
# MaxScanRows:    unset
# MaxLockTime:    unset
# MaxOpenFiles:   unset
# Timeout:        unset
# PasswordTimeout:        unset
# Subgroups:
# Owners:
# Users:    
#     perforce
# EOT
# cat /tmp/superusers.cfg | sudo -i -u perforce p4 group -i

# cat <<EOT > /tmp/protect.cfg
# Protections:
#     write user * * //...
#     list user * * -//spec/...  ## Remove access to ordinary users here
#     ## Ensure super (and admin) lines are always at the end
#     super user perforce * //...
#     super group SuperUsers * //...
# EOT
# cat /tmp/protect.cfg | sudo -i -u perforce p4 protect -i
