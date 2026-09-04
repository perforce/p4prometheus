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
        self.assertIn("Current Duration : 00:02:59  (:warning: ONGOING)", message)
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

    def testSlackBotPostsThreadedReply(self):
        """Bot mode posts the alert and configured reply in the alert thread."""
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
            "reply_message": "Investigating this alert.",
        })

        self.assertEqual(2, len(requests))
        self.assertEqual("xoxb-test", requests[0][0])
        self.assertEqual("C123", requests[0][1]["channel"])
        self.assertEqual("C123", requests[1][1]["channel"])
        self.assertEqual("123.456", requests[1][1]["thread_ts"])
        self.assertEqual("Investigating this alert.", requests[1][1]["text"])

        requests[:] = []
        with mock.patch("monitor_metrics.time.sleep") as sleep:
            notifier._send_slack("alert body", {
                "mode": "bot",
                "bot_token": "xoxb-test",
                "channel_id": "C123",
                "reply_message": "Investigating this alert.",
            }, test_notify=True)
            sleep.assert_called_once_with(5)

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
            "lock_start": datetime.datetime(2026, 9, 4, 7, 42, 1),
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
        self.assertIn("Lock Start Time  : 2026-09-04 07:42:01 (PDT)", reply_text)
        self.assertIn("*[1] 2001 root*", reply_text)

if __name__ == '__main__':
    unittest.main()
