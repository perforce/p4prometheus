# P4Metrics tests - using https://github.com/pytest-dev/pytest-testinfra

from time import sleep
import os
import signal

suffix = "-1-master.1.prom"

def test_metrics(host):
    assert host.file("/p4/metrics").exists
    metricsFiles = host.file("/p4/metrics").listdir()

    expectedFiles="p4_license p4_version_info p4_filesys p4_monitor p4_uptime p4_status"
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

def test_service_down(host):
    cmd = host.run("sudo systemctl stop p4d_1")
    assert '=error' not in cmd.stdout
    
    reload_metrics(host)
    sleep(1)
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
