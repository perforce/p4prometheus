# P4Metrics tests - using https://github.com/pytest-dev/pytest-testinfra

from time import sleep
import os
import signal
import re

suffix = "-1-master.1.prom"

def test_metrics(host):
    assert host.file("/p4/metrics").exists
    metricsFiles = host.file("/p4/metrics").listdir()

    expectedFiles="p4_license p4_version_info p4_filesys p4_monitor p4_uptime p4_status p4_journal_logs"
    for f in expectedFiles.split():
        assert f"{f}{suffix}" in metricsFiles

    config = host.file("/p4/common/config/p4metrics.yaml")
    assert config.contains("^update_interval: 5s")

    sf = host.file(f"/p4/metrics/p4_status{suffix}")
    assert sf.contains("^p4_monitoring_up.* 1")

    # Uptime should be > 0
    sf = host.file(f"/p4/metrics/p4_uptime{suffix}")
    assert sf.contains("^p4_server_uptime.* [1-9]$")

def reload_metrics(host):
    p = host.process.get(user="perforce", comm="p4metrics")
    os.kill(p.pid, signal.SIGHUP)

def test_journal_rotate(host):
    # set config value appropriately, then append to journal to exceed size
    # then check that it has rotated
    with open("/p4/common/config/p4metrics.yaml", "a") as f:
        f.write("max_journal_size: 100k\n")

    reload_metrics(host)
    sleep(2)

    jnl = host.file("/p4/1/logs/journal")
    jsize = jnl.size
    line = "A" * 2000 + "\n"
    with open("/p4/1/logs/journal", "a") as f:
        f.write(line * 60)  # 120k addition to file should trigger rotation

    assert jnl.size > jsize + 100000

    reload_metrics(host)
    sleep(2)

    reload_metrics(host)
    sleep(2)

    sf = host.file(f"/p4/metrics/p4_journal_logs{suffix}")
    contents = sf.content_string
    assert re.search(r"^p4_journal_size.* ([0-9]{2,})$", contents, re.MULTILINE)
    assert not re.search(r"^p4_journal_size.* ([0-9]{5,})$", contents, re.MULTILINE)

def test_service_down_up(host):
    cmd = host.run("sudo systemctl stop p4d_1")
    assert cmd.rc == 0

    reload_metrics(host)
    sleep(2)
    metricsFiles = host.file("/p4/metrics").listdir()

    notExpectedFiles = "p4_license p4_filesys p4_monitor"
    for f in notExpectedFiles.split():
        assert f"{f}{suffix}" not in metricsFiles

    expectedFiles="p4_version_info p4_uptime p4_status"
    for f in expectedFiles.split():
        assert f"{f}{suffix}" in metricsFiles

    # Status should be down
    sf = host.file(f"/p4/metrics/p4_status{suffix}")
    assert sf.contains("^p4_monitoring_up.* 0")

    # Uptime should be 0
    sf = host.file(f"/p4/metrics/p4_uptime{suffix}")
    assert sf.contains("^p4_server_uptime.* 0$")

    # Restart the service
    host.run("echo 'restarting service p4d_1' | systemd-cat")
    cmd = host.run("sudo systemctl start p4d_1")
    assert cmd.rc == 0

    # Wait for the service to come up
    sleep(3)
    reload_metrics(host)
    sleep(2)

    host.run("echo 'checking metrics files' | systemd-cat")
    metricsFiles = host.file("/p4/metrics").listdir()
    expectedFiles="p4_license p4_version_info p4_filesys p4_monitor p4_uptime p4_status"
    for f in expectedFiles.split():
        assert f"{f}{suffix}" in metricsFiles

    # Status should be down
    sf = host.file(f"/p4/metrics/p4_status{suffix}")
    assert sf.contains("^p4_monitoring_up.* 1")

    # Uptime should be non zero
    sf = host.file(f"/p4/metrics/p4_uptime{suffix}")
    assert sf.contains("^p4_server_uptime.* [1-9]$")

    sf = host.file(f"/p4/metrics/p4_journal_logs{suffix}")
    assert sf.contains("^p4_journal_size.* [1-9][0-9]*$")
    assert sf.contains("^p4_log_size.* [1-9][0-9]*$")
    assert sf.contains("^p4_logs_filecount.* [1-9][0-9]*$")
