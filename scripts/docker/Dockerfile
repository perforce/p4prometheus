# FROM ubi8-init AS p4promtestbase
FROM rockylinux/rockylinux:9-ubi-init AS p4promtestbase
MAINTAINER Robert Cowham "rcowham@perforce.com"

RUN yum update -y; \
    echo 'debconf debconf/frontend select Noninteractive' | debconf-set-selections; \
    echo  "postfix postfix/mailname string `hostname`" | debconf-set-selections; \
    echo "postfix postfix/main_mailer_type string 'No configuration'" | debconf-set-selections; \
    yum install -y openssh-server; \
    yum install -y wget; \
    yum install -y python3; \
    yum install -y python3-dev; \
    yum install -y python3-pip; \
    yum install -y libssl-dev; \
    yum install -y sudo; \
    yum install -y curl; \
    yum install -y vim; \
    yum install -y cron; \
    yum install -y gawk; \
    yum install -y cronie; \
    yum install -y hostname; \
    yum install -y rsync; \
    yum install -y policycoreutils-python-utils; \
    yum install -y passwd; \
    yum install -y bc; \
    yum autoremove; \
    yum clean all

RUN pip3 install pytest-testinfra p4python
ADD install_p4prom.sh /root/

# -----------------------------
# FROM p4promtestbase AS p4promtest-nonsdp

# RUN apt-get -y install helix-p4d

# ADD docker/setup_nonsdp.sh /root/
# ADD docker/run_p4prom_tests.sh /root/
# ADD tests/test_nosdp.py /root/

# -----------------------------
FROM p4promtestbase AS p4promtest-sdp

ADD docker/setup_sdp.sh /root/
# ADD docker/bin.linuxarm64/p4 /tmp/
# ADD docker/bin.linuxarm64/p4d /tmp/
RUN /root/setup_sdp.sh
ADD docker/run_p4prom_tests.sh /root/
ADD tests/test_sdp.py /root/

CMD ["/sbin/init"]

# # -----------------------------
# FROM p4promtestbase AS p4promtest-no-p4

# ADD install_node.sh /root/
# RUN /root/install_node.sh

# CMD ["/sbin/init"]
