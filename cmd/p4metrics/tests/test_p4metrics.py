# P4Metrics tests - using https://github.com/pytest-dev/pytest-testinfra

from time import sleep
import os
import signal
import re

suffix = "-1-master.1.prom"
base_config = """metrics_root: /hxlogs/metrics
sdp_instance:   1
p4bin:      p4
update_interval: 5s
cmds_by_user:   true
monitor_swarm:   true
"""

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

def test_journal_size_rotate(host):
    # set config value appropriately, then append to journal to exceed size
    # then check that it has rotated
    with open("/p4/common/config/p4metrics.yaml", "w") as f:
        f.write(f"""{base_config}
max_journal_size: 100k
""")

    jnlFiles = host.file("/p4/1/checkpoints/").listdir()
    rotatedJournals = len(jnlFiles)

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

    jnlFiles = host.file("/p4/1/checkpoints/").listdir()
    assert len(jnlFiles) == rotatedJournals + 1

def test_journal_percent_rotate(host):
    # set config value appropriately, then append to journal to exceed percentage size
    # then check that it has rotated
    # Filesystem      Size  Used Avail Use% Mounted on
    # overlay          93G   25G   69G  27% /
    with open("/p4/common/config/p4metrics.yaml", "w") as f:
        f.write(f"""{base_config}
max_journal_percent: 1
""")

    jnlFiles = host.file("/p4/1/checkpoints/").listdir()
    rotatedJournals = len(jnlFiles)

    reload_metrics(host)
    sleep(2)

    jnl = host.file("/p4/1/logs/journal")
    jsize = jnl.size
    line = "A" * 1000 + "\n"
    for i in range(10):
        with open("/p4/1/logs/journal", "a") as f:
            f.write(line * 200 * 1000)  # 1GB addition to file should trigger rotation

    assert jnl.size > jsize + 2 * 1000 * 1000 * 1000

    reload_metrics(host)
    sleep(3)

    jnlFiles = host.file("/p4/1/checkpoints/").listdir()
    assert len(jnlFiles) == rotatedJournals + 1

def test_log_size_rotate(host):
    # set config value appropriately, then append to journal to exceed size
    # then check that it has rotated
    with open("/p4/common/config/p4metrics.yaml", "w") as f:
        f.write(f"""{base_config}
max_log_size: 100k
""")

    logFiles = host.file("/p4/1/logs").listdir()
    rotatedLogs = len([x for x in logFiles if "log." in x])

    reload_metrics(host)
    sleep(2)

    log = host.file("/p4/1/logs/log")
    lsize = log.size
    line = "A" * 2000 + "\n"
    with open("/p4/1/logs/log", "a") as f:
        f.write(line * 60)  # 120k addition to file should trigger rotation

    assert log.size > lsize + 100000

    reload_metrics(host)
    sleep(2)

    logFiles = host.file("/p4/1/logs").listdir()
    assert len([x for x in logFiles if "log." in x]) > rotatedLogs
    assert any(re.match(r"log\..*\.gz$", fname) for fname in logFiles)

def test_log_percent_rotate(host):
    # set config value appropriately, then append to journal to exceed percentage size
    # then check that it has rotated
    # Filesystem      Size  Used Avail Use% Mounted on
    # overlay          93G   25G   69G  27% /
    with open("/p4/common/config/p4metrics.yaml", "w") as f:
        f.write(f"""{base_config}
max_log_percent: 1
""")

    logFiles = host.file("/p4/1/logs").listdir()
    rotatedLogs = len([x for x in logFiles if "log." in x])

    reload_metrics(host)
    sleep(2)

    log = host.file("/p4/1/logs/log")
    lsize = log.size
    line = "A" * 1000 + "\n"
    for i in range(10):
        with open("/p4/1/logs/log", "a") as f:
            f.write(line * 200 * 1000)  # 1GB addition to file should trigger rotation
    
    assert log.size > lsize + 100000

    reload_metrics(host)
    sleep(3)

    logFiles = host.file("/p4/1/logs").listdir()
    assert len([x for x in logFiles if "log." in x]) > rotatedLogs

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
