# P4Metrics tests - using https://github.com/pytest-dev/pytest-testinfra

from time import sleep
import os
import signal
import re

suffix = "-1-master.1.prom"
base_config = """metrics_root: /hxlogs/metrics
sdp_instance:   1
p4bin:      p4
update_interval: 1m
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
    # assert config.contains("^update_interval: 5s")

    sf = host.file(f"/p4/metrics/p4_status{suffix}")
    assert sf.contains("^p4_monitoring_up.* 1")

    # Uptime should be > 0
    reload_metrics(host)
    sleep(1)
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

    sf = host.file(f"/p4/metrics/p4_journal_logs{suffix}")
    assert sf.contains("^p4_journals_rotated.* 0")

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

    sf = host.file(f"/p4/metrics/p4_journal_logs{suffix}")
    assert sf.contains("^p4_journals_rotated.* [1-9]$")

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

    logsRotated = 0
    sf = host.file(f"/p4/metrics/p4_journal_logs{suffix}")
    m = re.search(r"^p4_logs_rotated.* ([0-9]+)", sf.content_string, re.M)
    if m:
        logsRotated = int(m.group(1))

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

    sf = host.file(f"/p4/metrics/p4_journal_logs{suffix}")
    m = re.search(r"^p4_logs_rotated.* ([0-9]+)", sf.content_string, re.M)
    if m:
        assert int(m.group(1)) > logsRotated
    else:
        assert False, "Could not find p4_logs_rotated metric"

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
    
    assert log.size > lsize + 2 * 1000 * 1000 * 1000

    reload_metrics(host)
    sleep(3)

    logFiles = host.file("/p4/1/logs").listdir()
    assert len([x for x in logFiles if "log." in x]) > rotatedLogs
    assert any(re.match(r"log\..*\.gz$", fname) for fname in logFiles)

# Test that creation of checkpoints and removing of it causes correct metrics to be reported
def test_checkpoint_metrics(host):
    metricsFiles = host.file("/p4/metrics").listdir()
    notExpectedFiles = "p4_checkpoint"
    for f in notExpectedFiles.split():
        assert f"{f}{suffix}" not in metricsFiles

    cmd = host.run("yum install -y file")
    assert cmd.rc == 0
    cmd = host.run("su -l perforce live_checkpoint.sh 1")
    assert cmd.rc == 0
    sleep(1)
    cmd = host.run("su -l perforce daily_checkpoint.sh 1")
    assert cmd.rc == 0
    sleep(1)

    reload_metrics(host)
    sleep(2)
    metricsFiles = host.file("/p4/metrics").listdir()
    expectedFiles = "p4_checkpoint"
    for f in expectedFiles.split():
        assert f"{f}{suffix}" in metricsFiles

    sf = host.file(f"/p4/metrics/p4_checkpoint{suffix}")
    assert sf.contains("^p4_sdp_checkpoint_error.* 0$")

    # Now cause an error in checkpointing and check metric is created
    sleep(1)
    cmd = host.run("rm -f /p4/1/offline_db/offline_db_usable.txt")
    assert cmd.rc == 0
    cmd = host.run("su -l perforce daily_checkpoint.sh 1")
    assert cmd.rc != 0
    sleep(1)

    reload_metrics(host)
    sleep(2)
    metricsFiles = host.file("/p4/metrics").listdir()
    expectedFiles = "p4_checkpoint"
    for f in expectedFiles.split():
        assert f"{f}{suffix}" in metricsFiles

    sf = host.file(f"/p4/metrics/p4_checkpoint{suffix}")
    assert sf.contains("^p4_sdp_checkpoint_error.* 1$")

    # Now remove checkpoint log and make sure metrics file is deleted
    cmd = host.run("rm -f /p4/1/logs/checkpoint.log*")
    assert cmd.rc == 0

    reload_metrics(host)
    sleep(2)

    metricsFiles = host.file("/p4/metrics").listdir()
    notExpectedFiles = "p4_checkpoint"
    for f in notExpectedFiles.split():
        assert f"{f}{suffix}" not in metricsFiles

def test_service_down_up(host):
    cmd = host.run("sudo systemctl stop p4d_1")
    assert cmd.rc == 0

    reload_metrics(host)
    sleep(2)
    metricsFiles = host.file("/p4/metrics").listdir()

    notExpectedFiles = "p4_filesys p4_monitor"
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
    assert sf.contains("^p4_logs_file_count.* [1-9][0-9]*$")
