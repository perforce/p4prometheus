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
    This script monitors locks and other Perforce server metrics for use with Prometheus.

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

LOGGER_NAME = 'monitor_metrics'
logger = logging.getLogger(LOGGER_NAME)

metrics_root = "/p4/metrics"
metrics_file = "locks.prom"

script_name = os.path.basename(os.path.splitext(__file__)[0])
LOGDIR = os.getenv('LOGS', '/p4/1/logs')

DEFAULT_LOG_FILE = "log-%s.log" % script_name
if os.path.exists(LOGDIR):
    DEFAULT_LOG_FILE = os.path.join(LOGDIR, "%s.log" % script_name)
DEFAULT_VERBOSITY = 'DEBUG'
LOGGER_NAME = 'monitor_metrics'

class MonitorMetrics:
    """Metric counts"""

    def __init__(self):
        super().__init__()
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

class P4Monitor(object):
    """See module doc string for details"""

    def __init__(self, *args, **kwargs):
        self.parse_args(__doc__, args)
        self.now = datetime.datetime.now()
        self.sdpinst_label = ""
        self.serverid_label = ""
        if self.options.sdp_instance:
            self.sdpinst_label = ',sdpinst="%s"' % self.options.sdp_instance
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
        parser.add_argument('--no-sdp', action='store_true', default=False, help="Whether this is SDP instance or not")
        parser.add_argument('-i', '--sdp-instance', help="SDP instance")
        parser.add_argument('-m', '--metrics-root', default=metrics_root, help="Metrics directory to use. Default: " + metrics_root)
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

    def run_cmd(self, cmd, get_output=True, timeout=35, stop_on_error=True):
        "Run cmd logging input and output"
        output = ""
        try:
            self.logger.debug("Running: %s" % cmd)
            if get_output:
                p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, universal_newlines=True, shell=True)
                output, err = p.communicate(timeout=timeout)
                rc = p.returncode
                self.logger.debug("Output:\n%s" % output)
            else:
                result = subprocess.check_call(cmd, stderr=subprocess.STDOUT, shell=True, timeout=timeout)
                self.logger.debug('Result: %d' % result)
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

    def parseMonitorData(self, mondata):
        reProc = re.compile("(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s*(.*)$")
        pids = {}
        for line in mondata.split("\n"):
            m = reProc.search(line)
            if m:
                pid = m.group(1)
                runstate = m.group(2)
                user = m.group(3)
                elapsed = m.group(4)
                cmd = m.group(5)
                args = None
                if len(m.groups()) == 6:
                    args = m.group(6)
                pids[pid] = (user, cmd, args)
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
                    self.logger.warning("Failed to parse: %s" % line)
                continue
            if parts[0] == "COMMAND":
                continue
            lockinfo = {"command": parts[0], "pid": parts[1],
                    "type": parts[2], "size": parts[3],
                    "mode": parts[4], "m": parts[5],
                    "start": parts[6], "end": parts[7],
                    "path": parts[8], "blocker": None}
            if len(parts) == 10:
                lockinfo["blocker"] = parts[9]
            jlock['locks'].append(lockinfo)
        return jlock
            
    # lslocks output in JSON format:
    # {"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B",
    #   "mode": "READ", "m": "0", "start": "0", "end": "0",
    #   "path": "/p4/1/root/server.locks/clientEntity/10,d/robomerge-main-ts",
    #   "blocker": null}
    def findLocks(self, lockdata, mondata):
        "Finds appropriate locks by parsing data"
        pids = self.parseMonitorData(mondata)
        metrics = MonitorMetrics()
        try:
            jlock = json.loads(lockdata)
        except Exception as e:
            self.logger.warning("Failed to load json: %s", str(e))
            jlock = []
        locks = []
        if 'locks' not in jlock:
            return metrics
        for j in jlock['locks']:
            if "p4d" not in j["command"]:
                continue
            if "clientEntity" in j["path"]:
                if j["mode"] == "READ":
                    metrics.clientEntityReadLocks += 1
                elif j["mode"] == "WRITE":
                    metrics.clientEntityWriteLocks += 1
            cmd = user = args = ""
            pid = j["pid"]
            mode = j["mode"]
            path = j["path"]
            if j["pid"] in pids:
                user, cmd, args = pids[j["pid"]]
            if "server.locks/meta" in j["path"]:
                if j["mode"] == "READ":
                    metrics.metaReadLocks += 1
                elif j["mode"] == "WRITE":
                    metrics.metaWriteLocks += 1
            if "/db." in j["path"]:
                if j["mode"] == "READ":
                    metrics.dbReadLocks += 1
                if j["mode"] == "WRITE":
                    metrics.dbWriteLocks += 1
            if j["blocker"]:
                metrics.blockedCommands += 1
                buser, bcmd, bargs = "unknown", "unknown", "unknown"
                bpid = j["blocker"]
                if bpid in pids:
                    buser, bcmd, bargs = pids[bpid]
                msg = "pid %s, user %s, cmd %s, table %s, blocked by pid %s, user %s, cmd %s, args %s" % (
                    pid, user, cmd, path, bpid, buser, bcmd, bargs)
                metrics.msgs.append(msg)
        return metrics

    def metricsHeader(self, name, help, type):
        lines = []
        lines.append("# HELP %s %s" % (name, help))
        lines.append("# TYPE %s %s" % (name, type))
        return lines

    def formatMetrics(self, metrics):
        lines = []
        name = "p4_locks_db_read"
        lines.extend(self.metricsHeader(name, "Database read locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.dbReadLocks))
        name = "p4_locks_db_write"
        lines.extend(self.metricsHeader(name, "Database write locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.dbWriteLocks))
        name = "p4_locks_cliententity_read"
        lines.extend(self.metricsHeader(name, "clientEntity read locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.clientEntityReadLocks))
        name = "p4_locks_cliententity_write"
        lines.extend(self.metricsHeader(name, "clientEntity write locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.clientEntityWriteLocks))
        name = "p4_locks_meta_read"
        lines.extend(self.metricsHeader(name, "meta db read locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.metaReadLocks))
        name = "p4_locks_meta_write"
        lines.extend(self.metricsHeader(name, "meta db write locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.metaWriteLocks))
        name = "p4_locks_replica_read"
        lines.extend(self.metricsHeader(name, "replica read locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.replicaReadLocks))
        name = "p4_locks_replica_write"
        lines.extend(self.metricsHeader(name, "replica write locks", "gauge"))
        lines.append("%s{%s%s} %s" % (name, self.serverid_label, self.sdpinst_label, metrics.replicaWriteLocks))
        return lines

    def writeMetrics(self, lines):
        fname = os.path.join(self.options.metrics_root, metrics_file)
        self.logger.debug("Writing to metrics file: %s", fname)
        self.logger.debug("Metrics: %s\n", "\n".join(lines))
        tmpfname = fname + ".tmp"
        with open(tmpfname, "w") as f:
            f.write("\n".join(lines))
            f.write("\n")
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

    def getLslocksVer(self, msg):
        # lslocks from util-linux 2.23.2
        try:
            return msg.split(" ")[-1]
        except:
            return "1.0"

    def run(self):
        """Runs script"""
        p4cmd = "%s -u %s -p %s" % (os.environ["P4BIN"], os.environ["P4USER"], os.environ["P4PORT"])
        locksver = self.getLsLocksVer(self.run_cmd("lslocks -V"))
        lockcmd = "lslocks -o +BLOCKER"
        if locksver <= "2.26":
            lockcmd += " +J"
            lockdata = self.run_cmd("lslocks -o +BLOCKER")
        else:
            lockdata = self.run_cmd("lslocks -o +BLOCKER")
            lockdata = self.parseTextLockInfo(lockdata)
        mondata = self.run_cmd('{0} -F "%id% %runstate% %user% %elapsed% %function% %args%" monitor show -al'.format(p4cmd))
        metrics = self.findLocks(lockdata, mondata)
        self.writeLog(self.formatLog(metrics))
        self.writeMetrics(self.formatMetrics(metrics))

if __name__ == '__main__':
    """ Main Program"""
    obj = P4Monitor(*sys.argv[1:])
    obj.run()
