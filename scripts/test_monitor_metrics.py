#!/usr/bin/env python3
# # -*- encoding: UTF8 -*-
# Test harness for monitor_metrics.py

from __future__ import print_function

import sys
import unittest
import os
import json
import datetime
import logging
import tempfile
from unittest import mock

curr_dir = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(curr_dir))

from monitor_metrics import P4Monitor, Notifier, build_slack_tree_sections

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
        self.assertEqual(3, m.dbReadLocks)
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
        self.assertEqual(1, m.dbReadLocks)
        self.assertEqual(1, m.dbWriteLocks)
        self.assertEqual(0, m.clientEntityReadLocks)
        self.assertEqual(0, m.clientEntityWriteLocks)
        self.assertEqual(1, m.metaReadLocks)
        self.assertEqual(0, m.metaWriteLocks)
        self.assertEqual(1, m.blockedCommands)
        self.assertEqual(1, len(m.msgs))
        self.assertEqual("pid 910, user jteam, cmd transmit, table db.sendq, blocked by pid 92079, user jteam, cmd sync, args ...", m.msgs[0])

        lines = [x for x in obj.formatMetrics(m) if not x.startswith("#")]
        exp = """p4_locks_db_read 1
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

    def testTruncatesLongSendqTableNames(self):
        """Long db.sendq names should be shortened in messages and Slack sections."""
        long_name = "db.sendq.jenkins-swarm-jenkins-docker-68-docker-development-modules-development_Changes.0"
        self.assertEqual("db.sendq.jenkins-sw...", P4Monitor.truncate_table_name(long_name))
        self.assertEqual("db.have", P4Monitor.truncate_table_name("db.have"))
        self.assertEqual(
            "this_table_name_is_very_long_b...",
            P4Monitor.truncate_table_name("this_table_name_is_very_long_but_not_db_sendq")
        )

        obj = P4Monitor()
        value = obj.dbFileInPath("/hxmetadata/p4/1/db1/" + long_name)
        self.assertEqual("db.sendq.jenkins-sw...", value)

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
        self.assertEqual(r"pid 921, user jteam, cmd sync, table metaLock, blocked by pid 900, user jteam, cmd sync, args ...",
                         metrics.msgs[2])
        blines, _, _ = obj.findBlockers(metrics)
        print(json.dumps(obj.blocking_tree, indent=4))
        self.assertEqual(3, len(blines))
        self.assertEqual("Blocking commands by oldest, with count", blines[0])
        self.assertRegex(blines[1], ".+ pid 900, .* blocking directly/indirectly: 1/1/1, total 3")
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
        blines, _, _ = obj.findBlockers(metrics)
        # Pretty print the blocking tree
        print(json.dumps(obj.blocking_tree, indent=4))
        self.assertEqual(4, len(blines))
        self.assertEqual("Blocking commands by oldest, with count", blines[0])
        self.assertRegex(blines[1], ".+ pid 920, .* blocking directly/indirectly: 1, total 1")
        self.assertRegex(blines[2], ".+ pid 900, .* blocking directly/indirectly: 1, total 1")
        self.assertEqual("blocking totals: 2", blines[3])

    def testRecursiveBlockers(self):
        """Check analysis of blockers when A is blocked by B is blocked by A!"""
        lockdata = """{
   "locks": [
      {"command":"p4d_1", "pid":910, "mode":"WRITE*", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":920},
      {"command":"p4d_1", "pid":900, "mode":"WRITE*", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":920},
      {"command":"p4d_1", "pid":920, "mode":"WRITE", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":910}
   ]
}"""
        mondata = """925 R jteam      00:00:09 transmit -b8
920 R jteam      00:00:06 sync ...
900 R jteam      00:00:04 sync ...
910 R jteam      00:00:02 transmit -b8"""
        obj = P4Monitor()
        metrics = obj.findLocks(lockdata, mondata)
        self.assertEqual(3, len(metrics.msgs))
        blines, _, _ = obj.findBlockers(metrics)
        print(json.dumps(obj.blocking_tree, indent=4))
        self.assertEqual(3, len(blines))
        self.assertEqual("Blocking commands by oldest, with count", blines[0])
        self.assertRegex(blines[1], ".+ pid 920, .* blocking directly/indirectly: 2/1, total 3")
        self.assertEqual("blocking totals: 3", blines[2])

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

    def testExtractServerInfoLines(self):
        """Extract ServerID and Server services from p4 info output."""
        obj = P4Monitor()
        infodata = """User name: perforce
Client name: my-client
ServerID: p4d_edge_CL1
Server services: edge-server
Server root: /p4/1/root
"""
        self.assertEqual(
            ["ServerID: p4d_edge_CL1", "Server services: edge-server"],
            obj.extract_server_info_lines(infodata)
        )

    def testNotificationIncludesServerInfoFirst(self):
        """Notification text should start with server identity lines."""
        notifier = Notifier({}, logging.getLogger("test_monitor_metrics"))
        message = notifier._format_chat_message(
            blocked_count=2,
            blocking_tree={"123": {}},
            max_lines=0,
            teams_style=False,
            include_intro=False,
            server_info_lines=["ServerID: p4d_edge_CL1", "Server services: edge-server"],
        )
        lines = message.splitlines()
        self.assertEqual("ServerID: p4d_edge_CL1", lines[0])
        self.assertEqual("Server services: edge-server", lines[1])
        self.assertIn("Blocking threshold exceeded - total commands showing as blocked: 2", message)

    def testSlackDetailedTreeSections(self):
        """The 'detailed' Slack style renders each root blocker as a header plus a box-drawing tree."""
        lockdata = """{
   "locks": [
      {"command":"p4d_1", "pid":910, "mode":"WRITE*", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":920},
      {"command":"p4d_1", "pid":920, "mode":"WRITE", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":null}
   ]
}"""
        mondata = """920 R teddkim    00:02:59 change -i
910 R teddkim    00:02:51 fstat -Olhp //PUBG/Solar_D..."""
        obj = P4Monitor()
        metrics = obj.findLocks(lockdata, mondata)
        blines, verbose_tree, tree_context = obj.findBlockers(metrics)
        sections = tree_context["sections"]
        self.assertEqual(1, len(sections))
        header, code_lines = sections[0]
        self.assertIn("920 teddkim", header)
        self.assertIn("blocks direct/indirect 1: total 1", header)
        self.assertEqual("cmd: change -i", code_lines[0])
        self.assertEqual("└─ 910 teddkim, elapsed 00:02:51, fstat -Olhp //PUBG/Solar_D...", code_lines[1])
        self.assertEqual(tree_context["duration"], "00:02:59")

        notifier = Notifier({}, logging.getLogger("test_monitor_metrics"))
        message = notifier._format_slack_detailed_message(
            1, tree_context,
            server_info_lines=["ServerID: p4d_edge_idc", "Server services: edge-server"])
        self.assertIn("ServerID        : p4d_edge_idc", message)
        self.assertIn("Longest Blocker Elapsed : 00:02:59  (:warning: ONGOING)", message)
        self.assertIn("Blocking threshold exceeded \u2014 total blocked commands: 1", message)
        self.assertIn("*Blocking Tree (full, untruncated)*", message)
        self.assertIn("```\ncmd: change -i", message)

    def testSlackDetailedChunksSplitLargeTreeAcrossBlocks(self):
        """A blocking tree too big for one Slack block should be split across several
        chunks (each within the block text limit) instead of being truncated."""
        notifier = Notifier({}, logging.getLogger("test_monitor_metrics"))
        big_code_lines = ["├─ {} someuser, elapsed 00:00:{:02d}, fstat -Olhp //some/long/path/to/a/file{}...".format(
            1000 + i, i % 60, i) for i in range(200)]
        tree_context = {
            "sections": [("910 someuser \u2014 clientEntityLock (blocks direct/indirect 200: total 200), elapsed 00:10:00",
                         big_code_lines)],
            "duration": "00:10:00",
            "lock_start": None,
            "detected_at": None,
            "tzname": "",
        }
        limit = 500  # small limit to force splitting without needing huge fixtures
        chunks = notifier._format_slack_detailed_chunks(1, tree_context, limit=limit)
        self.assertGreater(len(chunks), 2, "expected the large tree to be split into multiple chunks")
        for chunk in chunks:
            self.assertLessEqual(len(chunk), limit + 100)
        self.assertNotIn("truncated for Slack length limit", "\n".join(chunks))
        # All 200 lines must still be present somewhere across the chunks.
        joined = "\n".join(chunks)
        for i in (0, 100, 199):
            self.assertIn("someuser, elapsed 00:00:{:02d}, fstat -Olhp //some/long/path/to/a/file{}...".format(
                i % 60, i), joined)

    def testSlackBotPostsAlertOnly(self):
        """Bot mode posts the alert and saves its timestamp for a later reply."""
        notifier = Notifier({}, logging.getLogger("test_monitor_metrics"))
        requests = []

        def fake_api_request(token, payload):
            requests.append((token, payload))
            return {"ok": True, "ts": "123.456"} if len(requests) == 1 else {"ok": True}

        notifier._slack_api_request = fake_api_request
        notifier._send_slack("alert body", {
            "mode": "bot",
            "bot_token": "xoxb-test",
            "channel_id": "C123",
        })

        self.assertEqual(1, len(requests))
        self.assertEqual("xoxb-test", requests[0][0])
        self.assertEqual("C123", requests[0][1]["channel"])
    # When a later run finds fewer blocked commands, bot mode replies in the
    # original alert thread with the updated blocking tree.

    def testParseTestFileIgnoresExtraOutputBlocks(self):
        """parseTestFile() must skip unrelated Running:/Output: blocks (e.g. "info -s")
        and self-produced debug JSON dumps (e.g. "Blocking tree:"), and only treat the
        block following a "monitor show" command as monitor data."""
        log_text = """DEBUG 2026-01-01 00:00:00,000 monitor_metrics.py 1: Running: sudo lslocks -o +BLOCKER -J
DEBUG 2026-01-01 00:00:00,001 monitor_metrics.py 2: Output:
{
   "locks": [
      {"command":"p4d_1", "pid":910, "mode":"WRITE*", "path":"/hxmetadata/p4/1/db1/db.sendq", "blocker":920}
   ]
}

DEBUG 2026-01-01 00:00:00,002 monitor_metrics.py 3: Running: /p4/1/bin/p4_1 -u p4admin -p ssl:1666 info -s
DEBUG 2026-01-01 00:00:00,003 monitor_metrics.py 4: Output:
ServerID: p4d_edge_idc
Server services: edge-server

DEBUG 2026-01-01 00:00:00,004 monitor_metrics.py 5: Blocking tree:
pid, user [table,] cmd, args
{
    "920": {
        "910": {}
    }
}
DEBUG 2026-01-01 00:00:00,005 monitor_metrics.py 6: Running: /p4/1/bin/p4_1 -u p4admin -p ssl:1666 -F "%id% %runstate% %user% %elapsed% %function% %args%" monitor show -al
DEBUG 2026-01-01 00:00:00,006 monitor_metrics.py 7: Output:
920 R teddkim    00:02:59 change -i
910 R teddkim    00:02:51 fstat -Olhp //PUBG/Solar_D...

"""
        with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".log") as tmp:
            tmp.write(log_text)
            test_file = tmp.name
        self.addCleanup(lambda: os.path.exists(test_file) and os.remove(test_file))

        obj = P4Monitor()
        obj.options.test_file = test_file
        calls = []
        obj.process_entry = lambda locklines, monlines, timestamp, isJSON: calls.append(
            (locklines, monlines, timestamp, isJSON))

        obj.parseTestFile()

        self.assertEqual(1, len(calls), "expected exactly one parsed entry")
        locklines, monlines, timestamp, isJSON = calls[0]
        self.assertTrue(isJSON)
        self.assertIn('"pid":910', "\n".join(locklines))
        self.assertEqual(["920 R teddkim    00:02:59 change -i",
                          "910 R teddkim    00:02:51 fstat -Olhp //PUBG/Solar_D..."], monlines)

    def testNotifierSkipsDuplicateNotification(self):
        """Notifier should not resend exactly the same notification content."""
        with tempfile.NamedTemporaryFile(delete=False) as tmp:
            state_file = tmp.name
        self.addCleanup(lambda: os.path.exists(state_file) and os.remove(state_file))

        cfg = {
            "min_blocked_commands": 1,
            "cooldown_seconds": 0,
            "state_file": state_file,
            "script": {"enabled": True, "command": "dummy"},
        }
        notifier = Notifier(cfg, logging.getLogger("test_monitor_metrics"))
        sent_payloads = []

        def fake_send_script(payload, channel_cfg):
            sent_payloads.append((payload, channel_cfg))

        notifier._send_script = fake_send_script

        kwargs = {
            "blocked_count": 2,
            "blines": ["blocking totals: 2"],
            "detail_msgs": ["detail1"],
            "blocking_tree": {"123 user cmd": {}},
            "server_info_lines": ["ServerID: p4d_edge_CL1", "Server services: edge-server"],
        }

        notifier.maybe_notify(**kwargs)
        notifier.maybe_notify(**kwargs)
        self.assertEqual(1, len(sent_payloads))

        # Change payload: should send again.
        kwargs["blocked_count"] = 3
        kwargs["blines"] = ["blocking totals: 3"]
        notifier.maybe_notify(**kwargs)
        self.assertEqual(2, len(sent_payloads))

    def testSlackBotNotificationDecisionMatrix(self):
        """Bot alerts honor threshold/cooldown and reply only to later reductions."""
        cases = [
            ("initial below threshold", None, "", 4, 1000, 0, None, False),
            ("initial reaches threshold", None, "", 5, 1000, 1, "parent", False),
            ("equal during cooldown", 5, "123.456", 5, 1100, 0, None, False),
            ("higher during cooldown", 5, "123.456", 7, 1100, 0, None, False),
            ("equal after cooldown", 5, "123.456", 5, 1301, 1, "parent", False),
            ("higher after cooldown", 5, "123.456", 7, 1301, 1, "parent", False),
            ("lower replies during cooldown", 5, "123.456", 3, 1100, 1, "reply", False),
            ("lower without thread", 5, "", 3, 1100, 0, None, False),
            ("failed lower reply retains thread", 5, "123.456", 3, 1100, 1, "reply", True),
        ]
        for (name, previous_count, previous_ts, blocked_count, now, request_count,
             request_type, fail_reply) in cases:
            with self.subTest(name=name), tempfile.NamedTemporaryFile(delete=False) as tmp:
                state_file = tmp.name
            self.addCleanup(lambda path=state_file: os.path.exists(path) and os.remove(path))
            notifier = Notifier({
                "min_blocked_commands": 5,
                "cooldown_seconds": 300,
                "state_file": state_file,
                "slack": {
                    "enabled": True,
                    "mode": "bot",
                    "bot_token": "xoxb-test",
                    "channel_id": "C123",
                },
            }, logging.getLogger("test_monitor_metrics"))
            if previous_count is not None:
                notifier._save_state(1000, "previous", previous_count, previous_ts)

            requests = []

            def fake_api_request(token, payload):
                requests.append(payload)
                if "thread_ts" in payload and fail_reply:
                    return None
                return {"ok": True, "ts": "987.654"} if "thread_ts" not in payload else {"ok": True}

            notifier._slack_api_request = fake_api_request
            with mock.patch("monitor_metrics.time.time", return_value=now):
                notifier.maybe_notify(
                    blocked_count, ["blocking totals: {}".format(blocked_count)], [], {"2001": {}})

            self.assertEqual(request_count, len(requests))
            if request_type == "parent":
                self.assertNotIn("thread_ts", requests[0])
            elif request_type == "reply":
                self.assertEqual(previous_ts, requests[0]["thread_ts"])
            if fail_reply:
                with open(state_file, "r") as state_handle:
                    self.assertEqual(previous_ts, json.load(state_handle)["last_slack_ts"])

    def testNonBotNotificationDecisionMatrix(self):
        """Non-bot channels apply threshold, cooldown, and duplicate suppression."""
        cases = [
            ("below threshold", None, 4, 1000, "payload", 0),
            ("active cooldown", 1000, 5, 1100, "payload", 0),
            ("expired cooldown changed payload", 1000, 6, 1301, "changed", 1),
            ("expired cooldown duplicate payload", 1000, 5, 1301, "payload", 0),
        ]
        for name, previous_time, blocked_count, now, signature, expected_sends in cases:
            with self.subTest(name=name), tempfile.NamedTemporaryFile(delete=False) as tmp:
                state_file = tmp.name
            self.addCleanup(lambda path=state_file: os.path.exists(path) and os.remove(path))
            notifier = Notifier({
                "min_blocked_commands": 5,
                "cooldown_seconds": 300,
                "state_file": state_file,
                "script": {"enabled": True, "command": "dummy"},
            }, logging.getLogger("test_monitor_metrics"))
            if previous_time is not None:
                previous_signature = notifier._payload_signature(
                    5, ["blocking totals: 5"], [], {"2001": {}}, None)
                if signature == "changed":
                    previous_signature = "previous"
                notifier._save_state(previous_time, previous_signature, 5)

            sent_payloads = []
            notifier._send_script = lambda payload, cfg: sent_payloads.append(payload)
            blines = ["blocking totals: {}".format(blocked_count)]
            detail_msgs = [] if signature == "payload" else ["changed"]
            with mock.patch("monitor_metrics.time.time", return_value=now):
                notifier.maybe_notify(blocked_count, blines, detail_msgs, {"2001": {}})
            self.assertEqual(expected_sends, len(sent_payloads))

    def testBlockerObservationTimesPersistAcrossRuns(self):
        """Active root blockers retain first-seen time while stale entries are pruned."""
        with tempfile.NamedTemporaryFile(delete=False) as tmp:
            state_file = tmp.name
        self.addCleanup(lambda: os.path.exists(state_file) and os.remove(state_file))
        config = {
            "min_blocked_commands": 5,
            "state_file": state_file,
        }
        first_context = {
            "root_blocker_keys": ["100|db.have", "200|db.user"],
            "sections": [],
            "detected_at": datetime.datetime(2026, 9, 10, 17, 9),
            "tzname": "KST",
        }
        with mock.patch("monitor_metrics.time.time", return_value=1000):
            Notifier(config, logging.getLogger("test_monitor_metrics")).maybe_notify(
                2, [], [], {}, tree_context=first_context)

        second_context = {
            "root_blocker_keys": ["100|db.have"],
            "sections": [],
            "detected_at": datetime.datetime(2026, 9, 10, 17, 10),
            "tzname": "KST",
        }
        with mock.patch("monitor_metrics.time.time", return_value=1060):
            notifier = Notifier(config, logging.getLogger("test_monitor_metrics"))
            notifier.maybe_notify(1, [], [], {}, tree_context=second_context)

        self.assertEqual(datetime.datetime.fromtimestamp(1000), second_context["first_observed_at"])
        self.assertEqual(60, second_context["observed_duration_seconds"])
        rendered = "\n".join(notifier._format_slack_detailed_chunks(1, second_context))
        self.assertIn("First Blocking Observed : {} (KST)".format(
            datetime.datetime.fromtimestamp(1000).strftime("%Y-%m-%d %H:%M:%S")), rendered)
        self.assertIn("Observed Blocking For   : 00:01:00", rendered)
        with open(state_file, "r") as state_handle:
            self.assertEqual({"100|db.have": 1000}, json.load(state_handle)["blocker_first_seen"])

    def testSlackBotRepliesWhenBlocksAreReduced(self):
        """A later lower block count replies in the original Slack alert thread."""
        with tempfile.NamedTemporaryFile(delete=False) as tmp:
            state_file = tmp.name
        self.addCleanup(lambda: os.path.exists(state_file) and os.remove(state_file))

        notifier = Notifier({
            "min_blocked_commands": 5,
            "cooldown_seconds": 0,
            "state_file": state_file,
            "slack": {
                "enabled": True,
                "mode": "bot",
                "bot_token": "xoxb-test",
                "channel_id": "C123",
            },
        }, logging.getLogger("test_monitor_metrics"))
        requests = []

        def fake_api_request(token, payload):
            requests.append(payload)
            return {"ok": True, "ts": "123.456"} if len(requests) == 1 else {"ok": True}

        notifier._slack_api_request = fake_api_request
        notifier.maybe_notify(5, ["blocking totals: 5"], [], {"2001": {"2002": {}}})
        notifier.maybe_notify(3, ["blocking totals: 3"], [], {"2001": {"2002": {}, "2003": {}}})

        self.assertEqual(2, len(requests))
        self.assertEqual("123.456", requests[1]["thread_ts"])
        self.assertIn("Blocks reduced", requests[1]["text"])
        self.assertIn('"2003"', requests[1]["text"])
        with open(state_file, "r") as state_handle:
            self.assertEqual("", json.load(state_handle)["last_slack_ts"])

        notifier.maybe_notify(6, ["blocking totals: 6"], [], {"2001": {"2002": {}}})
        self.assertEqual(3, len(requests))
        self.assertNotIn("thread_ts", requests[2])

        notifier.maybe_notify(6, ["blocking totals: 6"], [], {"2001": {"2002": {}}})
        self.assertEqual(4, len(requests))
        self.assertNotIn("thread_ts", requests[3])

    def testSlackDetailedReductionReplyIncludesDetectionTimeAndTree(self):
        """Detailed Slack replies retain the alert tree and identify the reply time."""
        with tempfile.NamedTemporaryFile(delete=False) as tmp:
            state_file = tmp.name
        self.addCleanup(lambda: os.path.exists(state_file) and os.remove(state_file))

        notifier = Notifier({
            "min_blocked_commands": 5,
            "cooldown_seconds": 0,
            "state_file": state_file,
            "slack": {
                "enabled": True,
                "mode": "bot",
                "style": "detailed",
                "bot_token": "xoxb-test",
                "channel_id": "C123",
            },
        }, logging.getLogger("test_monitor_metrics"))
        requests = []

        def fake_api_request(token, payload):
            requests.append(payload)
            return {"ok": True, "ts": "123.456"} if len(requests) == 1 else {"ok": True}

        notifier._slack_api_request = fake_api_request
        original_context = {
            "sections": [("2001 root", ["cmd: job -i"])],
            "duration": "00:00:05",
            "detected_at": datetime.datetime(2026, 9, 4, 7, 42, 6),
            "tzname": "PDT",
        }
        reply_context = dict(original_context, detected_at=datetime.datetime(2026, 9, 4, 7, 42, 10))
        notifier.maybe_notify(5, ["blocking totals: 5"], [], {"2001": {}}, tree_context=original_context)
        notifier.maybe_notify(3, ["blocking totals: 3"], [], {"2001": {}}, tree_context=reply_context)

        self.assertEqual(2, len(requests))
        self.assertEqual("123.456", requests[1]["thread_ts"])
        self.assertEqual("Blocks reduced", requests[1]["text"])
        reply_text = "\n".join(block["text"]["text"] for block in requests[1]["blocks"]
                       if block["type"] == "section")
        self.assertIn("Blocks reduced", reply_text)
        self.assertIn("Reply Detected At : 2026-09-04 07:42:10 (PDT)", reply_text)
        self.assertIn("Longest Blocker Elapsed : 00:00:05  (:warning: ONGOING)", reply_text)
        self.assertIn("*[1] 2001 root*", reply_text)

if __name__ == '__main__':
    unittest.main()
