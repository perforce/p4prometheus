#!/usr/bin/env python3
# # -*- encoding: UTF8 -*-
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

    def testNoLocks(self):
        """Check parsing of lockdata when no results returned"""
        lockdata = """{}"""
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
        self.assertEqual(0, m.dbReadLocks)
        self.assertEqual(0, m.dbWriteLocks)
        self.assertEqual(0, m.clientEntityReadLocks)
        self.assertEqual(0, m.clientEntityWriteLocks)
        self.assertEqual(0, m.metaReadLocks)
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
        self.assertEqual("pid 2502, user fred, cmd sync, table db.have, blocked by pid 166, user jim, cmd sync, args -f //...", m.msgs[0])
        self.assertEqual("pid 2503, user susan, cmd sync, table db.have, blocked by pid 166, user jim, cmd sync, args -f //...", m.msgs[1])

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

    def testFindBlockers2(self):
        """Check parsing of lockdata"""
        lockdata = """{
   "locks": [
      {"command":"master", "pid":2023, "type":"FLOCK", "size":"33B", "mode":"WRITE", "m":false, "start":0, "end":0, "path":"/var/spool/postfix/pid/master.pid", "blocker":null},
      {"command":"p4d_1", "pid":910, "type":"FLOCK", "size":"14.6G", "mode":"WRITE*", "m":false, "start":0, "end":0, "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":92079},
      {"command":"p4d_1", "pid":92079, "type":"FLOCK", "size":"14.6G", "mode":"WRITE", "m":false, "start":0, "end":0, "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":null},
      {"command":"p4d_1", "pid":921, "type":"FLOCK", "size":null, "mode":"READ", "m":false, "start":0, "end":0, "path":"/hxmetadata/p4/1/db1/server.locks/meta/db", "blocker":null}
   ]
}"""
        mondata = """ 2033 B svc_master-1666 633:31:21 ldapsync -g -i 1800
 7009 I svc_p4d_fs_brk 00:00:34 IDLE none
12857 I svc_p4d_edge_CL1 00:02:32 IDLE none
925 R jteam      00:00:09 transmit -b8
92061 R ecagent    00:00:07 sync //...
922 R ecagent    00:00:06 transmit -t92061 -b8 -s524288
923 R jteam      00:00:06 sync ...
92079 R jteam      00:00:06 sync ...
924 R jteam      00:00:06 sync ...
921 R jteam      00:00:04 sync ...
910 R jteam      00:00:02 transmit -t92074 -b8 -s524288
92264 I swarm      00:00:00 IDLE none
609936 I svc_p4d_ha_chi 23:30:43 IDLE none"""
        obj = P4Monitor()
        m = obj.findLocks(lockdata, mondata)
        self.assertEqual(0, m.dbReadLocks)
        self.assertEqual(1, m.dbWriteLocks)
        self.assertEqual(0, m.clientEntityReadLocks)
        self.assertEqual(0, m.clientEntityWriteLocks)
        self.assertEqual(1, m.metaReadLocks)
        self.assertEqual(0, m.metaWriteLocks)
        self.assertEqual(1, m.blockedCommands)
        self.assertEqual(1, len(m.msgs))
        self.assertEqual("pid 910, user jteam, cmd transmit, table db.sendq, blocked by pid 92079, user jteam, cmd sync, args ...", m.msgs[0])

        lines = [x for x in obj.formatMetrics(m) if not x.startswith("#")]
        exp = """p4_locks_db_read 0
                 p4_locks_db_write 1
                 p4_locks_cliententity_read 0
                 p4_locks_cliententity_write 0
                 p4_locks_meta_read 1
                 p4_locks_meta_write 0
                 p4_locks_cmds_blocked 1""".split("\n")
        exp_lines = [x.strip() for x in exp]
        exp_lines.sort()
        lines.sort()
        self.maxDiff = None
        self.assertEqual(exp_lines, lines)

    def testFindBlockers3(self):
        """Check analysis of blockers"""
        lockdata = """{
   "locks": [
      {"command":"p4d_1", "pid":910, "mode":"WRITE*", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":920},
      {"command":"p4d_1", "pid":920, "mode":"WRITE", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":921},
      {"command":"p4d_1", "pid":921, "mode":"READ", "path":"/hxmetadata/p4/1/db1/server.locks/meta/db", "blocker":900},
      {"command":"p4d_1", "pid":900, "mode":"READ", "path":"/hxmetadata/p4/1/db1/server.locks/meta/db", "blocker":null}
   ]
}"""
        mondata = """925 R jteam      00:00:09 transmit -b8
922 R ecagent    00:00:06 transmit -t92061 -b8 -s524288
923 R jteam      00:00:06 sync ...
920 R jteam      00:00:06 sync ...
924 R jteam      00:00:06 sync ...
921 R jteam      00:00:04 sync ...
900 R jteam      00:00:04 sync ...
910 R jteam      00:00:02 transmit -b8"""
        obj = P4Monitor()
        metrics = obj.findLocks(lockdata, mondata)
        self.assertEqual(3, len(metrics.msgs))
        self.assertEqual(r"pid 910, user jteam, cmd transmit, table db.sendq, blocked by pid 920, user jteam, cmd sync, args ...",
                         metrics.msgs[0])
        self.assertEqual(r"pid 920, user jteam, cmd sync, table db.sendq, blocked by pid 921, user jteam, cmd sync, args ...",
                         metrics.msgs[1])
        self.assertEqual(r"pid 921, user jteam, cmd sync, table , blocked by pid 900, user jteam, cmd sync, args ...",
                         metrics.msgs[2])
        blines = obj.findBlockers(metrics)
        print(json.dumps(obj.blocking_tree, indent=4))
        self.assertEqual(3, len(blines))
        self.assertEqual("Blocking commands by oldest, with count", blines[0])
        self.assertRegex(blines[1], ".+ pid 900, .* blocking directly/indirectly: 1/1/1, total 3", blines[1])
        self.assertEqual("blocking totals: 3", blines[2])

        lockdata = """{
   "locks": [
      {"command":"p4d_1", "pid":910, "mode":"WRITE*", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":920},
      {"command":"p4d_1", "pid":920, "mode":"WRITE", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":null},
      {"command":"p4d_1", "pid":921, "mode":"READ", "path":"/hxmetadata/p4/1/db1/server.locks/meta/db", "blocker":900},
      {"command":"p4d_1", "pid":900, "mode":"READ", "path":"/hxmetadata/p4/1/db1/server.locks/meta/db", "blocker":null}
   ]
}"""
        obj = P4Monitor()
        metrics = obj.findLocks(lockdata, mondata)
        self.assertEqual(2, len(metrics.msgs))
        blines = obj.findBlockers(metrics)
        # Pretty print the blocking tree
        print(json.dumps(obj.blocking_tree, indent=4))
        self.assertEqual(4, len(blines))
        self.assertEqual("Blocking commands by oldest, with count", blines[0])
        self.assertRegex(blines[1], ".+ pid 920, .* blocking directly/indirectly: 1, total 1")
        self.assertRegex(blines[2], ".+ pid 900, .* blocking directly/indirectly: 1, total 1")
        self.assertEqual("blocking totals: 2", blines[3])


    def testFindBlockersNoPath(self):
        """Check parsing of lockdata"""
        lockdata = """{
   "locks": [
      {"command": "crond", "pid": "1313", "type": "FLOCK", "size": "5B", "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/run/crond.pid", "blocker": null},
      {"command": "p4d_1_bin", "pid": "6142", "type": "FLOCK", "size": null, "mode": "WRITE*", "m": "0", "start": "0", "end": "0", "path": null, "blocker": "3727"},
      {"command": "p4d_1_bin", "pid": "6144", "type": "FLOCK", "size": null, "mode": "WRITE*", "m": "0", "start": "0", "end": "0", "path": null, "blocker": "3727"},
      {"command": "p4d_1_bin", "pid": "3727", "type": "FLOCK", "size": null, "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": null, "blocker": null},
      {"command": "lsmd", "pid": "913", "type": "FLOCK", "size": "0B", "mode": "WRITE", "m": "0", "start": "0", "end": "0", "path": "/run/lsm/ipc/.lsmd-ipc-lock", "blocker": null}
   ]
}"""
        # Note pseudonimised reconcile commands
        mondata = r""" 3727 R fred 00:11:09 reconcile -f -m -c default a:\Project_files\Content\__ExternalActo..._Houses\FE7X5.uasset
 4620 I swarm      00:09:00 IDLE none
 4846 I swarm      00:07:59 IDLE none
 6142 R fred 00:04:22 reconcile -f -m -c default a:\Project_files\Content\__ExternalActo..._Houses\FE7X6.uasset
 6144 R fred 00:04:24 reconcile -f -m -c default a:\Project_files\Content\__ExternalActo..._Houses\0018T.uasset
 7048 I svc_p4d_edge_uswest2 00:00:00 IDLE none
 7535 R perforce   00:00:00 monitor show -al"""
        obj = P4Monitor()
        m = obj.findLocks(lockdata, mondata)
        self.assertEqual(0, m.dbReadLocks)
        self.assertEqual(1, m.dbWriteLocks)
        self.assertEqual(0, m.clientEntityReadLocks)
        self.assertEqual(0, m.clientEntityWriteLocks)
        self.assertEqual(0, m.metaReadLocks)
        self.assertEqual(0, m.metaWriteLocks)
        self.assertEqual(2, m.blockedCommands)
        self.assertEqual(2, len(m.msgs))
        self.maxDiff = None
        self.assertEqual(r"pid 6142, user fred, cmd reconcile, table unknown, blocked by pid 3727, user fred, cmd reconcile, args -f -m -c default a:\Project_files\Content\__ExternalActo..._Houses\FE7X5.uasset",
                         m.msgs[0])
        self.assertEqual(r"pid 6144, user fred, cmd reconcile, table unknown, blocked by pid 3727, user fred, cmd reconcile, args -f -m -c default a:\Project_files\Content\__ExternalActo..._Houses\FE7X5.uasset",
                         m.msgs[1])

        lines = [x for x in obj.formatMetrics(m) if not x.startswith("#")]
        exp = """p4_locks_db_read 0
                 p4_locks_db_write 1
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
