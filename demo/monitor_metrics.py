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
import json

LOGGER_NAME = 'monitor_metrics'
logger = logging.getLogger(LOGGER_NAME)

metrics_root = "/hxlogs/metrics"
default_logfile = "locks.log"

script_name = os.path.basename(os.path.splitext(__file__)[0])
LOGDIR = os.getenv('LOGS', '/p4/1/logs')

DEFAULT_LOG_FILE = "log-%s.log" % script_name
if os.path.exists(LOGDIR):
    DEFAULT_LOG_FILE = os.path.join(LOGDIR, "%s.log" % script_name)
DEFAULT_VERBOSITY = 'DEBUG'
LOGGER_NAME = 'monitor_metrics'

class MonitorMetrics:
    """Metric counts"""
    dbReadLocks = 0
    dbWriteLocks = 0
    clientEntityLocks = 0
    metaLocks = 0
    blockedCommands = 0

class P4Monitor(object):
    """See module doc string for details"""

    def __init__(self, *args, **kwargs):
        self.parse_args(__doc__, args)

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

    # {"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", 
    # "mode": "READ", "m": "0", "start": "0", "end": "0", 
    # "path": "/p4/1/root/server.locks/clientEntity/10,d/robomerge-main-ts", 
    # "blocker": null}
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
                metrics.clientEntityLocks += 1
            if "server.locks/meta" in j["path"]:
                metrics.metaLocks += 1
            if "/db." in j["path"]:
                if j["mode"] == "READ":
                    metrics.dbReadLocks += 1
                if j["mode"] == "WRITE":
                    metrics.dbWriteLocks += 1
            if j["blocker"]:
                metrics.blockedCommands += 1
                if j["blocker"] in pids:
                    user, cmd, blocker, buser, bcmd = ""
                    if j["pid"] in pids:
                        user, cmd, blocker = pids[j["pid"]]
                    msg = "Cmd %s pid %s blocked by cmd %s pid %s user %s" % (
                    )
        return metrics

    def run(self):
        """Runs script"""
        p4cmd = "%s -u %s -p %s" % (os.environ["P4BIN"], os.environ["P4USER"], os.environ["P4PORT"])
        lockdata = self.run_cmd("lslocks -o +BLOCKER -J")
        mondata = self.run_cmd('%s -F "%id% %runstate% %user% %elapsed% %function% %args%" monitor show -al' % p4cmd)
        self.findLocks(lockdata, mondata)
        self.logLocks()
        self.logMetrics()

if __name__ == '__main__':
    """ Main Program"""
    obj = P4Monitor(*sys.argv[1:])
    obj.run()

