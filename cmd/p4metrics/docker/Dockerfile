FROM rockylinux/rockylinux:9-ubi-init AS p4metricstestbase
LABEL maintainer="Robert Cowham <rcowham@perforce.com>"
ENV PATH="/root/.local/bin:/root/.venv/bin:/home/perforce/.local/bin:/home/perforce/.venv/bin:${PATH}"

RUN yum update -y; \
    echo 'debconf debconf/frontend select Noninteractive' | debconf-set-selections; \
    echo  "postfix postfix/mailname string `hostname`" | debconf-set-selections; \
    echo "postfix postfix/main_mailer_type string 'No configuration'" | debconf-set-selections; \
    yum install -y openssh-server; \
    yum install -y wget; \
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
    yum install -y file; \
    yum autoremove; \
    yum clean all

RUN curl -LsSf https://astral.sh/uv/install.sh | sh && \
    export PATH="/root/.local/bin:$PATH" && \
    uv python install 3.14 && \
    uv venv --python 3.14 /root/.venv && \
    . /root/.venv/bin/activate && \
    uv pip install pytest-testinfra p4python

# -----------------------------
# FROM p4metricstestbase AS p4metricstest-nonsdp

# RUN apt-get -y install helix-p4d

# ADD docker/setup_nonsdp.sh /root/
# ADD docker/run_p4prom_tests.sh /root/
# ADD tests/test_nosdp.py /root/

# -----------------------------
FROM p4metricstestbase AS p4metricstest-sdp

ADD cmd/p4metrics/docker/setup_sdp.sh /root/
RUN /root/setup_sdp.sh
RUN sudo -u perforce bash -lc 'curl -LsSf https://astral.sh/uv/install.sh | sh && \
    export PATH="$HOME/.local/bin:$PATH" && \
    uv python install 3.14 && \
    uv venv --python 3.14 "$HOME/.venv" && \
    . "$HOME/.venv/bin/activate" && \
    uv pip install pytest-testinfra p4python'
# ADD cmd/p4metrics/docker/run_setup_p4metrics_tests.sh /root/
# ADD cmd/p4metrics/docker/run_p4metrics_tests.sh /root/
# ADD cmd/p4metrics/tests/test_sdp.py /root/
# ADD cmd/p4metrics/tests/test_p4metrics.py /root/
ADD scripts/install_p4prom.sh /root/

CMD ["/sbin/init"]

# # -----------------------------
# FROM p4metricstestbase AS p4metricstest-no-p4

# ADD install_node.sh /root/
# RUN /root/install_node.sh

# CMD ["/sbin/init"]
