#!/usr/bin/env python3
# -*- coding: utf-8 -*-

# ==============================================================================
# Copyright and license info is available in the LICENSE file included with
# the Server Deployment Package (SDP), and also available online:
# https://swarm.workshop.perforce.com/projects/perforce-software-sdp/view/main/LICENSE
# ------------------------------------------------------------------------------

"""
NAME:
    monitor_metrics.py

DESCRIPTION:
    This script monitors locks using lslocks and p4 monitor show for Perforce server metrics for use with Prometheus.

    Assumes it is wrapped by a simple bash script monitor_wrapper.sh

    That configures SDP env or equivalent env vars.

"""

# Python 2.7/3.3 compatibility.
from __future__ import print_function

import sys
import os
import textwrap
import argparse
import logging
import re
import subprocess
import datetime
import json

python3 = (sys.version_info[0] >= 3)

LOGGER_NAME = 'monitor_metrics'
logger = logging.getLogger(LOGGER_NAME)

metrics_root = "/p4/metrics"
metrics_file = "locks.prom"

script_name = os.path.basename(os.path.splitext(__file__)[0])
LOGDIR = os.getenv('LOGS', '/p4/1/logs')

DEFAULT_LOG_FILE = "%s.log" % script_name
if os.path.exists(LOGDIR):
    DEFAULT_LOG_FILE = os.path.join(LOGDIR, "%s.log" % script_name)
DEFAULT_VERBOSITY = 'DEBUG'
LOGGER_NAME = 'monitor_metrics'


class MonitorPid:
    """Monitor table pid"""

    def __init__(self, pid, user, cmd, args, elapsed) -> None:
        self.pid = pid
        self.user = user
        self.cmd = cmd
        self.args = args
        self.elapsed = elapsed


class Blocker:
    """Blocking pid"""

    def __init__(self, pid, user, cmd, elapsed, table) -> None:
        self.pid = pid
        self.user = user
        self.cmd = cmd
        self.elapsed = elapsed
        self.table = table
        self.blockedPids = []


class MonitorMetrics:
    """Metric counts"""

    def __init__(self):
        self.dbReadLocks = 0
        self.dbWriteLocks = 0
        self.clientEntityReadLocks = 0
        self.clientEntityWriteLocks = 0
        self.metaReadLocks = 0
        self.metaWriteLocks = 0
        self.replicaReadLocks = 0
        self.replicaWriteLocks = 0
        self.blockedCommands = 0
        self.msgs = []
        self.blockingCommands = {}
        self.monitorCommands = {}

def build_blocking_tree(logger, blockingCommands):
    """
    Build a tree structure representing blocking relationships between PIDs.
    Args:
    blockingCommands (dict): A dictionary of Blocker objects, indexed by pid
    Returns:
    dict: A tree-like dictionary where each key is a root PID and value is its blocking tree
    """

    def create_subtree(pid, parents):
        """
        Recursively create a subtree for a given PID
        Args:
        pid (str): The PID to create a subtree for
        parents(list): Parent pids to this point in the tree
        Returns:
        dict: A subtree representing the blocking relationships
        """
        # If the PID is not in blockingCommands, it means it doesn't block anything
        if pid not in blockingCommands:
            return {pid: {}}
        blocker = blockingCommands[pid]
        if not blocker.blockedPids:
            return {pid: {}}

        subtree = {pid: {}}
        for blocked_pid in blocker.blockedPids:
            try:
                if blocked_pid in parents:
                    logger.debug(f"warning: Recursive add of {blocked_pid} in {str(parents)}")
                    blocked_subtree = {pid: {}}
                else:
                    blocked_subtree = create_subtree(blocked_pid, parents + [blocked_pid])
            except RecursionError:
                logger.fatal(f"recursion error processing pid {pid} recursing {blocked_pid}")
                # raise
            for key, value in blocked_subtree.items():
                subtree[pid][key] = value
        return subtree

    # Check for cyclic dependencies - and break them!
    for pid, blocker in blockingCommands.items():
        for bpid in blocker.blockedPids:
            if bpid in blockingCommands and pid in blockingCommands[bpid].blockedPids:
                blockingCommands[bpid].blockedPids.remove(pid)
                blockingCommands[bpid].blockedPids.append("cylic_dependency_%s" % pid)
                logger.warning(f"cyclic dependency pid {pid} recursingblocked by {bpid}")

    # Build the full blocking tree
    blocking_tree = {}
    for pid in blockingCommands:
        # Only include root-level PIDs (those not blocked by any other PID)
        if not any(pid in blocker.blockedPids for blocker in blockingCommands.values()):
            blocking_tree.update(create_subtree(pid, [pid]))
    return blocking_tree

def count_blocking(blocking_tree):
    """
    Recursively traverse the blocking tree and count descendants up to 9 levels.
    Args:
    blocking_tree (dict): A tree-like dictionary of blocking relationships
    Returns:
    list: A list of strings describing the blocking counts for each root PID
    """
    def recursive_descendant_count(subtree, max_depth=9):
        """
        Recursively count descendants at each level, up to max_depth.
        Args:
        subtree (dict): A subtree of the blocking relationships
        max_depth (int): Maximum depth of descendant counting
        Returns:
        blockingCounts (dict): For each pid a list of descendant counts at each level
        """
        if not subtree or max_depth == 0:
            return []
        # There should be only one key in the subtree (the current PID)
        pid = list(subtree.keys())[0]
        children = subtree[pid]
        # If no children, return empty list
        if not children:
            return []
        # Initialize counts with direct children count and recursively count descendants for next levels
        level_counts = [len(children)]
        for child_pid, child_subtree in children.items():
            child_counts = recursive_descendant_count({child_pid: child_subtree}, max_depth - 1)
            for i, count in enumerate(child_counts, 1):
                if i >= len(level_counts):
                    level_counts.append(count)
                else:
                    level_counts[i] += count
        return level_counts

    blockingCounts = {}
    # Traverse each root PID in the blocking tree
    for root_pid, subtree in blocking_tree.items():
        level_counts = recursive_descendant_count({root_pid: subtree})
        # Remove trailing zeros
        while level_counts and level_counts[-1] == 0:
            level_counts.pop()
        if level_counts:
            blockingCounts[root_pid] = level_counts
    return blockingCounts

def tree_with_metadata(tree, blockingCommands, monitorCommands, blockingCounts):
    """
    Add metadata to each PID in the tree.
    Args:
    tree (dict): The blocking tree
    blockingCommands: list of Blockers indexed by pid
    Returns:
    dict: The tree with metadata added for information
    """
    result = {}
    for pid, subtree in tree.items():
        # Get metadata for this PID and create a new key
        new_key = pid
        blocking = ""
        if pid in blockingCounts:
            bcount = sum(blockingCounts[pid])
            blocking = f" (blocks direct/indirect {'/'.join(map(str, blockingCounts[pid]))}: total {bcount})"
        p = monitorCommands.get(pid, {})
        args = ""
        if p:
            args = p.args
            if len(args) > 10:
                args = f"{args[:10]}..."
        b = blockingCommands.get(pid, {})
        if b:
            new_key = f"{pid} {b.table}{blocking}, {b.cmd} {args}"
        else:
            p = monitorCommands.get(pid, {})
            if p:
                new_key = f"{pid} {p.cmd} {args}"
        # If the subtree is empty, just add the new key with empty dict or recurse
        if not subtree:
            result[new_key] = {}
        else:
            result[new_key] = tree_with_metadata(subtree, blockingCommands, monitorCommands, blockingCounts)
    return result


class P4Monitor(object):
    """See module doc string for details"""

    def __init__(self, *args, **kwargs):
        self.parse_args(__doc__, args)
        self.now = datetime.datetime.now()
        self.sdpinst_label = ""
        self.serverid_label = ""
        if self.options.sdp_instance:
            self.sdpinst_label = 'sdpinst="%s"' % self.options.sdp_instance
            with open("/p4/%s/root/server.id" % self.options.sdp_instance, "r") as f:
                self.serverid_label = 'serverid="%s"' % f.read().rstrip()

    def parse_args(self, doc, args):
        """Common parsing and setting up of args"""
        desc = textwrap.dedent(doc)
        parser = argparse.ArgumentParser(
            formatter_class=argparse.RawDescriptionHelpFormatter,
            description=desc,
            epilog="Copyright (c) 2020 Perforce Software, Inc."
        )
        self.add_parse_args(parser)
        self.options = parser.parse_args(args=args)
        self.init_logger()
        self.logger.debug("Command Line Options: %s\n" % self.options)

    def add_parse_args(self, parser, default_log_file=None, default_verbosity=None):
        """Default trigger arguments - common to all triggers
        :param default_verbosity:
        :param default_log_file:
        :param parser:
        """
        if not default_log_file:
            default_log_file = DEFAULT_LOG_FILE
        if not default_verbosity:
            default_verbosity = DEFAULT_VERBOSITY
        parser.add_argument('-p', '--p4port', default=None,
                            help="Perforce server port. Default: $P4PORT")
        parser.add_argument('-u', '--p4user', default=None, help="Perforce user. Default: $P4USER")
        parser.add_argument('-L', '--log', default=default_log_file, help="Default: " + default_log_file)
        parser.add_argument('-i', '--sdp-instance', help="SDP instance")
        parser.add_argument('-t', '--test-file', help="Test file (section of log file from monitor_metrics.py)")
        parser.add_argument('-m', '--metrics-root', default=metrics_root, help="Metrics directory to use. Default: " + metrics_root)
        parser.add_argument('-o', '--metrics-file', default=metrics_file, help="Metrics file to use. Default: " + metrics_file)
        parser.add_argument('-v', '--verbosity',
                            nargs='?',
                            const="INFO",
                            default=default_verbosity,
                            choices=('DEBUG', 'WARNING', 'INFO', 'ERROR', 'FATAL'),
                            help="Output verbosity level. Default is: " + default_verbosity)

    def init_logger(self, logger_name=None):
        if not logger_name:
            logger_name = LOGGER_NAME
        self.logger = logging.getLogger(logger_name)
        self.logger.setLevel(self.options.verbosity)
        logformat = '%(levelname)s %(asctime)s %(filename)s %(lineno)d: %(message)s'
        logging.basicConfig(format=logformat, filename=self.options.log, level=self.options.verbosity)
        formatter = logging.Formatter('%(message)s')
        ch = logging.StreamHandler(sys.stderr)
        ch.setLevel(logging.INFO)
        ch.setFormatter(formatter)
        self.logger.addHandler(ch)

    def run_cmd(self, cmd, get_output=True, timeout=5, stop_on_error=True):
        "Run cmd logging input and output"
        output = ""
        try:
            self.logger.debug("Running: %s" % cmd)
            if get_output:
                p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, universal_newlines=True, shell=True)
                if python3:
                    output, _ = p.communicate(timeout=timeout)
                else:
                    output, _ = p.communicate()
                self.logger.debug("Output:\n%s" % output)
            else:
                if python3:
                    result = subprocess.check_call(cmd, stderr=subprocess.STDOUT, shell=True, timeout=timeout)
                else:
                    result = subprocess.check_call(cmd, stderr=subprocess.STDOUT, shell=True)
                self.logger.debug('Result: %d' % result)
        except subprocess.TimeoutExpired:
            self.logger.debug("Timeout Expired")
            return ""
        except subprocess.CalledProcessError as e:
            self.logger.debug("Output: %s" % e.output)
            if stop_on_error:
                msg = 'Failed cmd: %d %s' % (e.returncode, str(e))
                self.logger.debug(msg)
        except Exception as e:
            self.logger.debug("Output: %s" % output)
            if stop_on_error:
                msg = 'Failed cmd: %s' % str(e)
                self.logger.debug(msg)
        return output

    # Monitor data format:
    # 562 I perforce 00:01:01 monitor
    # 2502 I fred 00:01:01 sync //...
    # 2503 I susan 00:01:01 sync //...
    def parseMonitorData(self, mondata):
        reProc = re.compile(r"(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s*(.*)$")
        pids = {}
        for line in mondata.split("\n"):
            m = reProc.search(line)
            if m:
                pid = m.group(1)
                # runstate = m.group(2)
                user = m.group(3)
                elapsed = m.group(4)
                cmd = m.group(5)
                args = None
                if len(m.groups()) == 6:
                    args = m.group(6)
                pids[pid] = MonitorPid(pid, user, cmd, args, elapsed)
        return pids

    # Old versions of lslocks can't return json so we parse text
    # For now assume no spaces in file paths or this won't work!
    # COMMAND           PID   TYPE SIZE MODE  M START END PATH                       BLOCKER
    # (unknown)          -1 OFDLCK   0B WRITE 0     0   0 /etc/hosts
    # (unknown)          -1 OFDLCK   0B READ  0     0   0
    # p4d               107  FLOCK  16K READ* 0     0   0 /path/db.config            105
    # p4d               105  FLOCK  16K WRITE 0     0   0 /path/db.config
    # p4d               105  FLOCK  16K WRITE 0     0   0 /path/db.configh
    def parseTextLockInfo(self, lockdata):
        jlock = {'locks': []}
        for line in lockdata.split("\n"):
            parts = line.split()
            if len(parts) < 9:
                if line != "":
                    self.logger.warning("Warning (not enough fields) - failed to parse: %s" % line)
                continue
            if parts[0] == "COMMAND" or parts[3] == "START":
                continue
            lockinfo = {"command": parts[0], "pid": parts[1],
                        "type": parts[2], "size": parts[3],
                        "mode": parts[4], "m": parts[5],
                        "start": parts[6], "end": parts[7],
                        "path": parts[8], "blocker": None}
            if len(parts) == 10:
                lockinfo["blocker"] = parts[9]
            jlock['locks'].append(lockinfo)
        self.logger.debug("parsed TextLockInfo: %s" % str(jlock))
        return json.dumps(jlock)

    def dbFileInPath(self, path):
        "Returns name of db file or empty string"
        parts = path.split("/")
        if not parts:
            return ""
        p = parts[-1]
        if p.startswith("db.") or p == "rdb.lbr" or p.startswith("storage"):
            return p
        for p in ["/clients/", "/clientEntity/", "/meta/"]:
            if p in path:
                return p.replace("/", "") + "Lock"
        return ""

    # lslocks output in JSON format:
    # {"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B",
    #   "mode": "READ", "m": "0", "start": "0", "end": "0",
    #   "path": "/p4/1/root/server.locks/clientEntity/10,d/robomerge-main-ts",
    #   "blocker": null}
    def findLocks(self, lockdata, mondata):
        "Finds appropriate locks by parsing data"
        pids = self.parseMonitorData(mondata)
        metrics = MonitorMetrics()
        metrics.monitorCommands = pids
        if lockdata in ["", "{}"]:
            self.logger.debug("Empty json for lockdata")
            return metrics
        try:
            jlock = json.loads(lockdata)
        except Exception as e:
            self.logger.warning("Warning - failed to load json: %s\n%s", str(e), lockdata)
            jlock = []
        if 'locks' not in jlock:
            return metrics
        for j in jlock['locks']:
            if "p4d" not in j["command"] or "path" not in j:
                continue
            if j["path"] and "clientEntity" in j["path"]:
                if j["mode"] == "READ":
                    metrics.clientEntityReadLocks += 1
                elif j["mode"] == "WRITE":
                    metrics.clientEntityWriteLocks += 1
            cmd = user = ""
            pid = str(j["pid"])
            # mode = j["mode"]
            path = j["path"]
            if pid in pids:
                mp = pids[pid]
                user = mp.user
                cmd = mp.cmd
            if path and "server.locks/meta" in path:
                if j["mode"] == "READ":
                    metrics.metaReadLocks += 1
                elif j["mode"] == "WRITE":
                    metrics.metaWriteLocks += 1
            dbPath = "unknown"
            if path:
                dbPath = self.dbFileInPath(path)
            if dbPath:
                if j["mode"] == "READ":
                    metrics.dbReadLocks += 1
                if j["mode"] == "WRITE":
                    metrics.dbWriteLocks += 1
            if j["blocker"]:
                buser, bcmd, bargs, belapsed = "unknown", "unknown", "unknown", "unknown"
                bpid = str(j["blocker"])
                if bpid in pids:
                    mp = pids[bpid]
                    buser = mp.user
                    bcmd = mp.cmd
                    bargs = mp.args
                    belapsed = mp.elapsed
                msg = "pid %s, user %s, cmd %s, table %s, blocked by pid %s, user %s, cmd %s, args %s" % (
                    pid, user, cmd, dbPath, bpid, buser, bcmd, bargs)
                if bpid not in metrics.blockingCommands:
                    metrics.blockingCommands[bpid] = Blocker(bpid, buser, bcmd, belapsed, dbPath)
                if pid not in metrics.blockingCommands[bpid].blockedPids:
                    metrics.blockedCommands += 1
                    metrics.blockingCommands[bpid].blockedPids.append(pid)
                    metrics.msgs.append(msg)
                else:
                    self.logger.debug(f"warning: duplicate record for pid {pid} blocked by {bpid}")
        return metrics

    def metricsHeader(self, name, help, type):
        lines = []
        lines.append("# HELP %s %s" % (name, help))
        lines.append("# TYPE %s %s" % (name, type))
        return lines

    def formatLabels(self, labels):
        if not labels:
            return ""
        result = ",".join([x for x in labels if x])
        if result:
            return "{%s}" % result
        return ""

    def formatMetrics(self, metrics):
        lines = []
        labels = [self.serverid_label, self.sdpinst_label]
        name = "p4_locks_db_read"
        lines.extend(self.metricsHeader(name, "Database read locks", "gauge"))
        lines.append("%s%s %s" % (name, self.formatLabels(labels), metrics.dbReadLocks))
        name = "p4_locks_db_write"
        lines.extend(self.metricsHeader(name, "Database write locks", "gauge"))
        lines.append("%s%s %s" % (name, self.formatLabels(labels), metrics.dbWriteLocks))
        name = "p4_locks_cliententity_read"
        lines.extend(self.metricsHeader(name, "clientEntity read locks", "gauge"))
        lines.append("%s%s %s" % (name, self.formatLabels(labels), metrics.clientEntityReadLocks))
        name = "p4_locks_cliententity_write"
        lines.extend(self.metricsHeader(name, "clientEntity write locks", "gauge"))
        lines.append("%s%s %s" % (name, self.formatLabels(labels), metrics.clientEntityWriteLocks))
        name = "p4_locks_meta_read"
        lines.extend(self.metricsHeader(name, "meta db read locks", "gauge"))
        lines.append("%s%s %s" % (name, self.formatLabels(labels), metrics.metaReadLocks))
        name = "p4_locks_meta_write"
        lines.extend(self.metricsHeader(name, "meta db write locks", "gauge"))
        lines.append("%s%s %s" % (name, self.formatLabels(labels), metrics.metaWriteLocks))
        name = "p4_locks_cmds_blocked"
        lines.extend(self.metricsHeader(name, "cmds blocked by locks", "gauge"))
        lines.append("%s%s %s" % (name, self.formatLabels(labels), metrics.blockedCommands))
        return lines

    def writeMetrics(self, lines):
        fname = os.path.join(self.options.metrics_root, self.options.metrics_file)
        self.logger.debug("Writing to metrics file: %s", fname)
        self.logger.debug("Metrics: %s\n", "\n".join(lines))
        tmpfname = fname + ".tmp"
        with open(tmpfname, "w") as f:
            f.write("\n".join(lines))
            f.write("\n")
        os.chmod(tmpfname, 0o644)
        os.rename(tmpfname, fname)

    def formatLog(self, metrics):
        prefix = self.now.strftime("%Y-%m-%d %H:%M:%S")
        lines = []
        if not metrics.msgs:
            lines.append("%s no blocked commands" % prefix)
        else:
            for m in metrics.msgs:
                lines.append("%s %s" % (prefix, m))
        return lines

    def writeLog(self, lines):
        with open(self.options.log, "a") as f:
            f.write("\n".join(lines))
            f.write("\n")

    def getLslocksVer(self, ver):
        # lslocks from util-linux 2.23.2
        try:
            return ver.split()[-1]
        except Exception:
            return "1.0"

    def findBlockers(self, metrics):
        lblockers = [metrics.blockingCommands[x] for x in metrics.blockingCommands]
        lblockers.sort(key=lambda x: x.elapsed)  # Newest first
        blines = []
        if lblockers:
            blines.append("Blocking commands by oldest, with count")
        # Check if blocked files have children, grand-children etc who are blocked!
        self.blocking_tree = build_blocking_tree(self.logger, metrics.blockingCommands)
        blockingCounts = count_blocking(self.blocking_tree)
        verbose_tree = tree_with_metadata(self.blocking_tree, metrics.blockingCommands, metrics.monitorCommands, blockingCounts)
        self.logger.debug("Blocking tree:\npid, [table,] cmd, args\n" + json.dumps(verbose_tree, indent=4))
        lblockers.sort(key=lambda x: x.elapsed, reverse=True)  # Oldest first
        for b in lblockers:
            if not b.pid in blockingCounts:
                continue
            blocking_str = f"{'/'.join(map(str, blockingCounts[b.pid]))}"
            bcount = sum(blockingCounts[b.pid])
            blines.append("blocking cmd: elapsed %s, pid %s, user %s, cmd %s, blocking directly/indirectly: %s, total %d" % (
                b.elapsed, b.pid, b.user, b.cmd, blocking_str, bcount))
        blines.append("blocking totals: %d" % (metrics.blockedCommands))
        return blines

    def parseTestFile(self):
        # Parses test file and outputs result
        # DEBUG 2024-04-03 23:57:02,118 monitor_metrics.py 137: Running: sudo lslocks -o +BLOCKER -J
        # DEBUG 2024-04-03 23:57:02,211 monitor_metrics.py 144: Output:
        # {
        # "locks": [
        #     {"command":"snapd", "pid":1249, "type":"FLOCK", "size":null, "mode":"WRITE", "m":false, "start":0, "end":0, "path":"/var/lib/snapd/state.lock", "blocker":null},
        # }
        #
        # DEBUG 2024-04-03 23:57:02,211 monitor_metrics.py 137: Running: /p4/1/bin/p4_1 -u p4sdp -p ssl:1667 -F "%id% %runstate% %user% %elapsed% %function% %args%" monitor show -al
        # DEBUG 2024-04-03 23:57:02,313 monitor_metrics.py 144: Output:
        # 2030 B svc_master-1666 05:24:42 ldapsync -g -i 1800
        # 162476 I svc_p4d_fs_brk 00:00:01 IDLE none
        locklines = []
        monlines = []
        timestamp = ""
        isJSON = True
        with open(self.options.test_file, "r") as f:
            stage = 0   # 1 = processing locks, 2 = processing monitor data
            for line in f:
                line = line.rstrip()
                if stage == 0 and line.startswith("{"):
                    stage = 1
                    locklines.append(line)
                    continue
                if stage == 0 and line.startswith("COMMAND"): # Non-JSON
                    stage = 1
                    isJSON = False
                    locklines.append(line)
                    continue
                if stage == 1:
                    locklines.append(line)
                    if isJSON and line == "}":
                        stage = 2
                    if not isJSON and "parsed TextLockInfo:" in line:
                        ind = line.index("{")
                        locklines = line[ind:].replace("'", '"')
                        locklines = locklines.replace(" None", " null")
                        stage = 2
                    continue
                if stage == 2 and line.endswith("Output:"):
                    stage = 3
                    timestamp = line[6:25] + " "
                    continue
                if stage == 3:
                    if line == "":
                        if isJSON:
                            metrics = self.findLocks("\n".join(locklines), "\n".join(monlines))
                        else:
                            metrics = self.findLocks(locklines, "\n".join(monlines))
                        self.writeLog(self.formatLog(metrics))
                        blines = self.findBlockers(metrics)
                        self.writeLog([timestamp + x for x in blines])
                        self.writeMetrics(self.formatMetrics(metrics))
                        locklines = []
                        monlines = []
                        stage = 0
                    else:
                        monlines.append(line)
        if monlines or locklines:
            if isJSON:
                metrics = self.findLocks("\n".join(locklines), "\n".join(monlines))
            else:
                metrics = self.findLocks(locklines, "\n".join(monlines))
            self.writeLog(self.formatLog(metrics))
            self.findBlockers(metrics)
            self.writeMetrics(self.formatMetrics(metrics))

    def run(self):
        """Runs script"""
        if self.options.test_file:
            self.parseTestFile()
            return
        p4cmd = "%s -u %s -p %s" % (os.environ["P4BIN"], os.environ["P4USER"], os.environ["P4PORT"])
        verdata = self.run_cmd("lslocks -V")
        locksver = self.getLslocksVer(verdata)
        lockcmd = "sudo lslocks -o +BLOCKER"    # Try sudo
        # If lslocks can't return JSON we parse it into JSON ourselves
        if locksver > "2.26":
            lockcmd += " -J"
            lockdata = self.run_cmd(lockcmd)
            if not lockdata:
                lockcmd = lockcmd.replace("sudo ", "")
                lockdata = self.run_cmd(lockcmd)
        else:
            lockdata = self.run_cmd(lockcmd)
            if not lockdata:
                lockcmd = lockcmd.replace("sudo ", "")
                lockdata = self.run_cmd(lockcmd)
            lockdata = self.parseTextLockInfo(lockdata)
        mondata = self.run_cmd('{0} -F "%id% %runstate% %user% %elapsed% %function% %args%" monitor show -al'.format(p4cmd))
        metrics = self.findLocks(lockdata, mondata)
        self.writeLog(self.formatLog(metrics))
        timestamp = self.now.strftime("%Y-%m-%d %H:%M:%S ")
        blines = self.findBlockers(metrics)
        self.writeLog([timestamp + x for x in blines])
        self.writeMetrics(self.formatMetrics(metrics))


if __name__ == '__main__':
    """ Main Program"""
    obj = P4Monitor(*sys.argv[1:])
    obj.run()
