# P4Prometheus tests for sdp

from time import sleep


def test_metrics(host):
    assert host.file("/p4/metrics").exists
    metricsFiles = host.file("/p4/metrics").listdir()

    expectedFiles="p4_license-1-master.1.prom p4_replication-1-master.1.prom p4_version_info-1-master.1.prom p4_filesys-1-master.1.prom  p4_monitor-1-master.1.prom  p4_uptime-1-master.1.prom"
    for f in expectedFiles.split():
        assert f in metricsFiles

def test_node(host):
    assert host.file("/tmp/node.out").exists
    cmd = host.run("grep =error /tmp/node.out | grep -v 'udev device' ")
    assert '=error' not in cmd.stdout
    
    cmd = host.run("curl localhost:9100/metrics")
    assert 'node_time_seconds' in cmd.stdout
    assert 'p4_server_uptime' in cmd.stdout

    sleep(1)
    cmd = host.run("grep =error /tmp/node.out | grep -v 'udev device' ")
    assert '=error' not in cmd.stdout
