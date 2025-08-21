# P4Metrics tests - using https://github.com/pytest-dev/pytest-testinfra

from time import sleep


def test_metrics(host):
    assert host.file("/p4/metrics").exists
    metricsFiles = host.file("/p4/metrics").listdir()

    expectedFiles="p4_license-1-master.1.prom p4_version_info-1-master.1.prom p4_filesys-1-master.1.prom  p4_monitor-1-master.1.prom  p4_uptime-1-master.1.prom"
    for f in expectedFiles.split():
        assert f in metricsFiles

    config = host.file("/p4/common/config/p4metrics.yaml")
    assert config.contains("update_interval: 5s")

    sf = host.file("/p4/metrics/p4_status-1-master.1.prom")
    assert sf.contains("p4_monitoring_up.* 1")

def test_service_down(host):
    cmd = host.run("sudo systemctl stop p4d_1")
    assert '=error' not in cmd.stdout
    
    sleep(6)
    metricsFiles = host.file("/p4/metrics").listdir()

    sf = host.file("/p4/metrics/p4_status-1-master.1.prom")
    assert sf.contains("p4_monitoring_up.* 0")

    expectedFiles="p4_license-1-master.1.prom p4_version_info-1-master.1.prom p4_filesys-1-master.1.prom  p4_monitor-1-master.1.prom  p4_uptime-1-master.1.prom"
    for f in expectedFiles.split():
        assert f in metricsFiles

    # cmd = host.run("curl localhost:9100/metrics")
    # assert 'node_time_seconds' in cmd.stdout
    # assert 'p4_server_uptime' in cmd.stdout

    # sleep(1)
    # cmd = host.run("grep =error /tmp/node.out | grep -v 'udev device' ")
    # assert '=error' not in cmd.stdout
