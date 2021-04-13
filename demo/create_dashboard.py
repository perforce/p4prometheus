#!/usr/bin/env python3
# -*- coding: utf-8 -*-

# ==============================================================================
# Copyright and license info is available in the LICENSE file included with
# the Server Deployment Package (SDP), and also available online:
# https://swarm.workshop.perforce.com/projects/perforce-software-sdp/view/main/LICENSE
# ------------------------------------------------------------------------------

"""
NAME:
    create_dashboard.py

DESCRIPTION:
    This script creates Grafana dashboards for p4prometheus monitoring metrics.

    The resulting dashboard can be easily uploaded to Grafana with associated script:

        upload_grafana_dashboard.sh

USAGE:
    ./create_dashboard.py -h

    Set environment variables for use in upload script:

    export GRAFANA_SERVER=p4monitor:3000
    export GRAFANA_API_KEY="<API key created above>"

    Create and upload the dashboard:

    ./create_dashboard.py --title "My python dashboard" > dash.json
    ./upload_grafana_dashboard.sh dash.json

"""

import textwrap
import argparse
import sys
import io
import grafanalib.core as G
from grafanalib._gen import write_dashboard

DEFAULT_TITLE = "P4prometheus Metrics"

metrics = [
    {'section': 'Monitor Tracking'},
    {'row': 1},
    {'title': 'Monitor Processes (by cmd)',
     'expr': [r'p4_monitor_by_cmd{sdpinst="$sdpinst",serverid="$serverid"}',
              r'sum(p4_monitor_by_cmd{sdpinst="$sdpinst",serverid="$serverid"})']},
    {'title': 'Monitor Processes (by user)',
     'expr': ['p4_monitor_by_user{sdpinst="$sdpinst",serverid="$serverid"}',
             'sum(p4_monitor_by_user{sdpinst="$sdpinst",serverid="$serverid"})']},
    {'row': 1},
    {'title': 'p4d process count',
     'expr': ['p4_process_count']},
    {'title': 'rtv sessions active',
     'expr': ['p4_rtv_svr_sessions_active']},
    {'row': 1},
    {'title': '$serverid Time for last checkpoint',
     'expr': ['p4_sdp_checkpoint_duration{sdpinst="$sdpinst",serverid="$serverid"}']},
    {'title': 'Uptime',
     'expr': ['p4_server_uptime{sdpinst="$sdpinst"}']},
    {'row': 1},
    {'title': '$serverid time since last checkpoint',
     'expr': ['time() - p4_sdp_checkpoint_log_time{sdpinst="$sdpinst",serverid="$serverid"}']},
     
    {'row': 1},
    {'title': 'All P4 Cmds Count (rate/10min)',
     'expr': ['rate(p4_completed_cmds_per_day{instance="p4poke-chi:9100",sdpinst="$sdpinst",serverid="$serverid"}[10m])',
              'sum(rate(p4_cmd_counter{sdpinst="$sdpinst",serverid="$serverid"}[10m]))']},
    {'title': 'p4d log lines read (rate/min)',
     'expr': ['rate(p4_prom_log_lines_read{sdpinst="$sdpinst",serverid="$serverid"}[1m])']},
    {'row': 1},
    {'title': 'Error Count rates by subsystem/id',
     'expr': ['rate(p4_error_count{subsystem!~"[0-9].*"}[1m])']},
    
    {'section': 'Replication'},
    {'row': 1},
    {'title': 'Replica Journal number',
     'expr': ['p4_replica_curr_jnl{sdpinst="$sdpinst",serverid="$serverid"}']},
    {'title': 'Replica Journal Pos',
     'expr': ['p4_replica_curr_pos{sdpinst="$sdpinst",serverid="$serverid"}']},
    {'row': 1},
    {'title': 'Replica Lag p4d_ha_chi',
     'expr': ['p4_replica_curr_pos{instance="p4poke-chi:9100",job="node_exporter",sdpinst="1",servername="master-1666"} - '
              'ignoring(serverid,servername) '
              'p4_replica_curr_pos{instance="p4poke-chi:9100",job="node_exporter",sdpinst="1",servername="p4d_ha_chi"}']},
    {'title': 'Replica Lag p4d_fs_brk',
     'expr': ['p4_replica_curr_pos{instance="p4poke-chi:9100",job="node_exporter",sdpinst="1",servername="master-1666"} - '
              'ignoring(serverid, servername) '
              'p4_replica_curr_pos{instance="p4poke-chi:9100",job="node_exporter",sdpinst="1",servername="p4d_fs_brk"}']},
    {'row': 1},
    {'title': 'Pull queue size',
     'expr': ['p4_pull_queue{sdpinst="$sdpinst"}']},
    {'title': 'rtv_repl_behind_bytes p4d_fs_brk',
     'expr': ['p4_rtv_rpl_behind_bytes{instance="gemini:9100", job="node_exporter", sdpinst="1", serverid="p4d_fs_brk"}']},
    {'row': 1},
    {'title': 'Pull queue errors',
     'expr': ['p4_pull_errors{sdpinst="$sdpinst"}']},

    {'section': 'Cmd Count and Duration'},
    {'row': 1},
    {'title': 'Cmds duration (rate/min)',
     'expr': ['rate(p4_cmd_cumulative_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m])']},
    {'title': 'p4 cmds top 10 (rate/min)',
     'expr': ['sum without (instance, job)(rate(p4_cmd_counter{sdpinst="$sdpinst",serverid="$serverid"}[1m]))']},

    {'section': 'Table Locking'},
    {'row': 1},
    {'title': 'P4 Read Locks',
     'expr': ['p4_locks_db_read{sdpinst="$sdpinst",serverid="$serverid"}',
              'p4_locks_cliententity_read{sdpinst="$sdpinst",serverid="$serverid"}',
              'p4_locks_meta_read{sdpinst="$sdpinst",serverid="$serverid"}',
              'p4_locks_replica_read{sdpinst="$sdpinst",serverid="$serverid"}']},
    {'title': 'P4 Write Locks',
     'expr': ['p4_locks_db_write{sdpinst="$sdpinst",serverid="$serverid"}',
              'p4_locks_cliententity_write{sdpinst="$sdpinst",serverid="$serverid"}',
              'p4_locks_meta_write{sdpinst="$sdpinst",serverid="$serverid"}',
              'p4_locks_replica_write{sdpinst="$sdpinst",serverid="$serverid"}']},
    {'row': 1},
    {'title': 'p4 read locks held per table (rate/min)',
     'expr': ['sum without (instance, job)(rate(p4_total_read_held_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))']},
    {'title': 'p4 read locks waiting (rate/min)',
     'expr': ['sum without (instance, job)(rate(p4_total_read_wait_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))']},
    {'row': 1},
    {'title': 'p4 write locks held per table (rate/min)',
     'expr': ['sum without (instance, job)(rate(p4_total_write_held_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))']},
    {'title': 'p4 write locks wait per table (rate/min)',
     'expr': ['sum without (instance, job)(rate(p4_total_write_wait_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))']},

    {'row': 1},
    {'title': 'RTV processes waiting for locks',
     'expr': ['p4_rtv_db_lockwait']},
    {'row': 1},
    {'title': 'RTV is checkpoint active',
     'expr': ['p4_rtv_db_ckp_active']},
    {'title': 'RTV checkpoint records processed',
     'expr': ['p4_rtv_db_ckp_records']},
    {'row': 1},
    {'title': 'RTV DB I/O record count',
     'expr': ['p4_rtv_db_io_records']},
    {'row': 1},
    {'title': 'RTV replica byte lag',
     'expr': ['p4_rtv_rpl_behind_bytes']},
    {'title': 'RTV replica journal lag',
     'expr': ['p4_rtv_rpl_behind_journals']},
    {'row': 1},
    {'title': 'RTV active sessions',
     'expr': ['p4_rtv_svr_sessions_active']},
    {'title': 'RTV total sessions',
     'expr': ['p4_rtv_svr_sessions_total']},
    {'row': 1},
    {'title': 'Processes waiting on read locks',
     'expr': ['p4_locks_db_read']},
    {'title': 'Processes waiting on write locks',
     'expr': ['p4_locks_db_write']},
    {'row': 1},
    {'title': 'Processes waiting on read locks per table',
     'expr': ['p4_locks_db_read_by_table']},
    {'title': 'Processes waiting on write locks per table',
     'expr': ['p4_locks_db_write_by_table']},
    {'row': 1},
    {'title': 'Processes waiting on cliententity read locks',
     'expr': ['p4_locks_cliententity_read']},
    {'title': 'Processes waiting on cliententity write locks',
     'expr': ['p4_locks_cliententity_write']},
    {'row': 1},
    {'title': 'p4_locks_meta_read',
     'expr': ['Processes waiting on meta_read']},
    {'title': 'p4_locks_meta_write',
     'expr': ['Processes waiting on meta_write']},
    {'row': 1},
    {'title': 'Processes blocked count',
     'expr': ['p4_locks_cmds_blocked']},
    {'title': 'Processes blocked count by cmd',
     'expr': ['p4_locks_cmds_blocking_by_cmd']},
]


class CreateDashboard():
    """See module doc string for details"""

    def __init__(self, *args, **kwargs):
        self.parse_args(__doc__, args)

    def parse_args(self, doc, args):
        """Common parsing and setting up of args"""
        desc = textwrap.dedent(doc)
        parser = argparse.ArgumentParser(
            formatter_class=argparse.RawDescriptionHelpFormatter,
            description=desc,
            epilog="Copyright (c) 2021 Perforce Software, Inc."
        )
        self.add_parse_args(parser)
        self.options = parser.parse_args(args=args)
        self.options.use_sdp = not self.options.no_sdp

    def add_parse_args(self, parser):
        """Default trigger arguments - common to all triggers"""
        parser.add_argument('-t', '--title', default=DEFAULT_TITLE, help="Dashboard title. Default: " + DEFAULT_TITLE)
        parser.add_argument('--no-sdp', action='store_true', default=False, help="Whether this is SDP instance or not - default is SDP")

    def run(self):
        templateList = []
        if self.options.use_sdp:
            templateList.append(G.Template(
                    default="1",
                    dataSource="default",
                    name="sdpinst",
                    label="SDPInstance",
                    query="label_values(sdpinst)"))
        templateList.append(G.Template(
                    default="",
                    dataSource="default",
                    name="serverid",
                    label="ServerID",
                    query="label_values(serverid)"))

        dashboard = G.Dashboard(
            title=self.options.title,
            templating=G.Templating(list=templateList)
        )

        for metric in metrics:
            if 'section' in metric:
                dashboard.rows.append(G.Row(title=metric['section'], showTitle=True))
                continue
            if 'row' in metric:
                dashboard.rows.append(G.Row(title='', showTitle=False))
                continue
            graph = G.Graph(title=metric['title'],
                            dataSource='default',
                            maxDataPoints=1000,
                            legend=G.Legend(show=True, alignAsTable=True,
                                        min=True, max=True, avg=True, current=True, total=True,
                                        sort='max', sortDesc=True),
                            yAxes=G.single_y_axis(),
                        )
            refId = 'A'
            for texp in metric['expr']:
                # Remove SDP
                if not self.options.use_sdp:
                    texp = texp.replace('sdpinst="$sdpinst",', '')
                graph.targets.append(G.Target(expr=texp,
                                              legendFormat="instance {{instance}}, serverid {{serverid}}",
                                              refId=refId))
                refId = chr(ord(refId) + 1)
            dashboard.rows[-1].panels.append(graph)

        # Auto-number panels - returns new dashboard
        dashboard = dashboard.auto_panel_ids()

        s = io.StringIO()
        write_dashboard(dashboard, s)
        print("""{
        "dashboard": %s
        }
        """ % s.getvalue())

if __name__ == '__main__':
    """ Main Program"""
    obj = CreateDashboard(*sys.argv[1:])
    obj.run()
