FROM ubuntu:20.04 AS p4promtestbase
MAINTAINER Robert Cowham "rcowham@perforce.com"

RUN apt -y update; \
    echo 'debconf debconf/frontend select Noninteractive' | debconf-set-selections; \
    echo  "postfix postfix/mailname string `hostname`" | debconf-set-selections; \
    echo "postfix postfix/main_mailer_type string 'No configuration'" | debconf-set-selections; \
    apt install -y apt-utils; \
    apt install -y openssh-server; \
    apt install -y wget; \
    apt install -y python3; \
    apt install -y python3-dev; \
    apt install -y python3-pip; \
    apt install -y libssl-dev; \
    apt install -y sudo; \
    apt install -y curl; \
    apt install -y vim; \
    apt autoremove; \
    apt clean

# Replace systemctl for use within docker
RUN cd /usr/local/bin; \
  wget https://raw.githubusercontent.com/gdraheim/docker-systemctl-replacement/master/files/docker/systemctl3.py; \
  chmod 755 systemctl3.py; \
  cd /usr/local/bin; \
  mv systemctl systemctl.old; \
  ln -s /usr/local/bin/systemctl3.py systemctl; \
  cd /usr/bin; \
  mv systemctl systemctl.old; \
  ln -s /usr/local/bin/systemctl3.py systemctl

RUN echo "deb http://package.perforce.com/apt/ubuntu  focal  release" > /etc/apt/sources.list.d/perforce.list; \
    wget -qO - https://package.perforce.com/perforce.pubkey | sudo apt-key add -; \
    apt-get -y update; \
    apt-get -y install cron; \
    apt-get -y install gawk

RUN pip3 install pytest-testinfra
ADD install_p4prom.sh /root/

# -----------------------------
FROM p4promtestbase AS p4promtest-nonsdp

RUN apt-get -y install helix-p4d

ADD docker/setup_nonsdp.sh /root/
ADD docker/run_install.sh /root/
ADD tests/test_nosdp.py /root/

# -----------------------------
FROM p4promtestbase AS p4promtest-sdp

ADD docker/setup_sdp.sh /root/
RUN /root/setup_sdp.sh
ADD docker/run_install.sh /root/
ADD tests/test_sdp.py /root/

# -----------------------------
FROM p4promtestbase AS p4promtest-no-p4

ADD install_node.sh /root/
# RUN /root/install_node.sh