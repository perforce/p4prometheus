# -*- encoding: UTF8 -*-
# Test harness for monitor_metrics.py

from __future__ import print_function

import sys
import unittest
import os
import json

curr_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(curr_dir))

from monitor_metrics import P4Monitor

# os.environ["LOGS"] = "."
# LOGGER_NAME = "testMonitorMetrics"
# LOG_FILE = "log-testMonitorMetrics.log"


class TestMonitorMetrics(unittest.TestCase):
    # def __init__(self, methodName='runTest'):
    #     super(TestMonitorMetrics, self).__init__(LOGGER_NAME, LOG_FILE, methodName=methodName)

    def setUp(self):
        pass

    def tearDown(self):
        pass

    def testFindLocks(self):
        """Check parsing of lockdata"""
        lockdata = """{ "locks": [
                {"command": "lvmetad", "pid": "1458", "type": "POSIX", "size": "5B", "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/run/lvmetad.pid", "blocker": null},
                {"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/server.locks/clientEntity/10,d/robomerge-main-ts", "blocker": null},
                {"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/server.locks/meta/db", "blocker": null},
                {"command": "p4d"   , "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/db.have", "blocker": null}
            ]}
            """
        mondata = """     562 I perforce 00:01:01 monitor
          2502 I fred 00:01:01 sync //...
        """
        obj = P4Monitor()
        m = obj.findLocks("", "")
        self.assertEqual(0, m.dbReadLocks)
        self.assertEqual(0, m.dbWriteLocks)
        self.assertEqual(0, m.clientEntityReadLocks)
        self.assertEqual(0, m.clientEntityWriteLocks)
        self.assertEqual(0, m.metaReadLocks)
        self.assertEqual(0, m.metaWriteLocks)
        self.assertEqual(0, m.blockedCommands)
        self.assertEqual(0, len(m.msgs))

        m = obj.findLocks(lockdata, mondata)
        self.assertEqual(1, m.dbReadLocks)
        self.assertEqual(0, m.dbWriteLocks)
        self.assertEqual(1, m.clientEntityReadLocks)
        self.assertEqual(0, m.clientEntityWriteLocks)
        self.assertEqual(1, m.metaReadLocks)
        self.assertEqual(0, m.metaWriteLocks)
        self.assertEqual(0, m.blockedCommands)
        self.assertEqual(0, len(m.msgs))

    def testTextLslocksParse(self):
        """Check parsing of textual form"""
        lockdata = """COMMAND           PID   TYPE SIZE MODE  M START END PATH                       BLOCKER
(unknown)          -1 OFDLCK   0B WRITE 0     0   0 /etc/hosts
(unknown)          -1 OFDLCK   0B READ  0     0   0
p4d               107  FLOCK  16K READ* 0     0   0 /path/db.config            105
p4d               105  FLOCK  16K WRITE 0     0   0 /path/db.config
p4d               105  FLOCK  16K WRITE 0     0   0 /path/db.configh
"""
        obj = P4Monitor()
        jlock = obj.parseTextLockInfo(lockdata)
        expected = {"locks": [
                {"command": "(unknown)", "pid": "-1", "type": "OFDLCK", "size": "0B",
                    "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/etc/hosts",
                    "blocker": None},
                {"command": "p4d", "pid": "107", "type": "FLOCK", "size": "16K",
                    "mode": "READ*", "m": "0", "start": "0", "end": "0", "path": "/path/db.config",
                    "blocker": "105"},
                {"command": "p4d", "pid": "105", "type": "FLOCK", "size": "16K",
                    "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/path/db.config",
                    "blocker": None},
                {"command": "p4d", "pid": "105", "type": "FLOCK", "size": "16K",
                    "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/path/db.configh",
                    "blocker": None},
            ]}
        self.maxDiff = None
        self.assertDictEqual(expected, json.loads(jlock))

    def testFindBlockers(self):
        """Check parsing of lockdata"""
        lockdata = """{ "locks": [
                {"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/db.have", "blocker": "166"},
                {"command": "p4d", "pid": "2503", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/db.have", "blocker": "166"},
                {"command": "p4d", "pid": "2502", "type": "FLOCK", "size": "17B", "mode": "READ", "m": "0", "start": "0", "end": "0", "path": "/p4/1/root/db.have", "blocker": null}
            ]}
            """
        mondata = """     562 I perforce 00:01:01 monitor
          2502 I fred 00:01:01 sync //...
          2503 I susan 00:01:01 sync //...
          166 I jim 00:01:01 sync -f //...
        """
        obj = P4Monitor()
        m = obj.findLocks(lockdata, mondata)
        self.assertEqual(3, m.dbReadLocks)
        self.assertEqual(0, m.dbWriteLocks)
        self.assertEqual(0, m.clientEntityReadLocks)
        self.assertEqual(0, m.clientEntityWriteLocks)
        self.assertEqual(0, m.metaReadLocks)
        self.assertEqual(0, m.metaWriteLocks)
        self.assertEqual(2, m.blockedCommands)
        self.assertEqual(2, len(m.msgs))
        self.assertEqual("pid 2502, user fred, cmd sync, table /p4/1/root/db.have, blocked by pid 166, user jim, cmd sync, args -f //...", m.msgs[0])
        self.assertEqual("pid 2503, user susan, cmd sync, table /p4/1/root/db.have, blocked by pid 166, user jim, cmd sync, args -f //...", m.msgs[1])

        lines = [x for x in obj.formatMetrics(m) if not x.startswith("#")]
        exp = """p4_locks_db_read 3
                 p4_locks_db_write 0
                 p4_locks_cliententity_read 0
                 p4_locks_cliententity_write 0
                 p4_locks_meta_read 0
                 p4_locks_meta_write 0
                 p4_locks_cmds_blocked 2""".split("\n")
        exp_lines = [x.strip() for x in exp]
        exp_lines.sort()
        lines.sort()
        self.maxDiff = None
        self.assertEqual(exp_lines, lines)


if __name__ == '__main__':
    unittest.main()
