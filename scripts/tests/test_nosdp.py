# Tests for non-sdp

from time import sleep


def test_metrics(host):
    assert host.file("/p4metrics").exists
    metricsFiles = host.file("/p4metrics").listdir()

    expectedFiles="p4_license-test.server.prom p4_replication-test.server.prom p4_version_info-test.server.prom p4_filesys-test.server.prom  p4_monitor-test.server.prom  p4_uptime-test.server.prom"
    for f in expectedFiles.split():
        assert f in metricsFiles

def test_node(host):
    assert host.file("/tmp/node.out").exists
    cmd = host.run("grep =error /tmp/node.out")
    assert '=error' not in cmd.stdout
    
    cmd = host.run("curl localhost:9100/metrics")
    assert 'node_time_seconds' in cmd.stdout
    assert 'p4_server_uptime' in cmd.stdout

    sleep(1)
    cmd = host.run("grep =error /tmp/node.out")
    assert '=error' not in cmd.stdout
