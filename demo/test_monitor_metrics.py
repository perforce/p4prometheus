# -*- encoding: UTF8 -*-
# Test harness for monitor_metrics.py

from __future__ import print_function

import sys
import unittest
import os
import re

import P4
curr_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(curr_dir))

from monitor_metrics import P4Monitor, MonitorMetrics

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
        self.assertEqual(0, m.clientEntityLocks)
        self.assertEqual(0, m.metaLocks)
        self.assertEqual(0, m.blockedCommands)
        self.assertEqual(0, len(m.msgs))

        m = obj.findLocks(lockdata, mondata)
        self.assertEqual(1, m.dbReadLocks)
        self.assertEqual(0, m.dbWriteLocks)
        self.assertEqual(1, m.clientEntityLocks)
        self.assertEqual(1, m.metaLocks)
        self.assertEqual(0, m.blockedCommands)
        self.assertEqual(0, len(m.msgs))

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
        self.assertEqual(0, m.clientEntityLocks)
        self.assertEqual(0, m.metaLocks)
        self.assertEqual(2, m.blockedCommands)
        self.assertEqual(2, len(m.msgs))
        self.assertEqual("pid 2502, user fred, cmd sync, table /p4/1/root/db.have, blocked by pid 166, user jim, cmd sync, args -f //...", m.msgs[0])
        self.assertEqual("pid 2503, user susan, cmd sync, table /p4/1/root/db.have, blocked by pid 166, user jim, cmd sync, args -f //...", m.msgs[1])


if __name__ == '__main__':
    unittest.main()
