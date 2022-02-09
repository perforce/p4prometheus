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
import yaml
import grafanalib.core as G
from grafanalib._gen import write_dashboard

DEFAULT_TITLE = "P4Prometheus Metrics"

# The following counters not included (for now)
# p4_cmd_ip_counter
# p4_cmd_ip_cumulative_seconds
# p4_cmd_running
# p4_cmd_user_counter
# p4_cmd_user_cumulative_seconds
# p4_cmd_user_detail_counter
# p4_cmd_user_detail_cumulative_seconds


metrics = yaml.load("""
- section: Monitor Tracking
- row: 1
- title: Monitor Processes (by cmd)
  target:
  - expr: p4_monitor_by_cmd{sdpinst="$sdpinst",serverid="$serverid"}
    legend: "{{cmd}}"
  - expr: sum(p4_monitor_by_cmd{sdpinst="$sdpinst",serverid="$serverid"})
    legend: all
- title: Monitor Processes (by user)
  target:
  - expr: p4_monitor_by_user{sdpinst="$sdpinst",serverid="$serverid"}
    legend: "{{user}}"
  - expr: sum(p4_monitor_by_user{sdpinst="$sdpinst",serverid="$serverid"})
    legend: all

- row: 1
- title: p4d process count
  target:
  - expr: p4_process_count
- title: rtv sessions active
  target:
  - expr: p4_rtv_svr_sessions_active

- row: 1
- title: $serverid Time for last SDP checkpoint
  target:
  - expr: p4_sdp_checkpoint_duration{sdpinst="$sdpinst",serverid="$serverid"}
  yformat: s
- title: Uptime
  type: gauge
  target:
  - expr: p4_server_uptime{sdpinst="$sdpinst"}
  yformat: s

- row: 1
- title: $serverid time since last SDP checkpoint
  target:
  - expr: time() - p4_sdp_checkpoint_log_time{sdpinst="$sdpinst",serverid="$serverid"}
  yformat: s

- row: 1
- title: All P4 Cmds Count (rate/10min)
  target:
  - expr: rate(p4_completed_cmds_per_day{sdpinst="$sdpinst",serverid="$serverid"}[10m])
  - expr: sum(rate(p4_cmd_counter{sdpinst="$sdpinst",serverid="$serverid"}[10m]))
- title: p4d log lines read (rate/min)
  target:
  - expr: rate(p4_prom_log_lines_read{sdpinst="$sdpinst",serverid="$serverid"}[1m])

- row: 1
- title: p4prometheus cmds pending
  target:
  - expr: p4_prom_cmds_pending{sdpinst="$sdpinst",serverid="$serverid"}
- title: p4prometheus cmds processed (rate/min)
  target:
  - expr: rate(p4_prom_cmds_processed{sdpinst="$sdpinst",serverid="$serverid"}[1m])

- row: 1
- title: p4prometheus CPU system (rate/min)
  target:
  - expr: rate(p4_prom_cpu_system{sdpinst="$sdpinst",serverid="$serverid"}[1m])
- title: p4prometheus CPU user (rate/min)
  target:
  - expr: rate(p4_prom_cpu_user{sdpinst="$sdpinst",serverid="$serverid"}[1m])

- row: 1
- title: Error Count rates by subsystem/id
  target:
  - expr: rate(p4_error_count{sdpinst="$sdpinst",serverid="$serverid",subsystem!~"[0-9].*"}[1m])
    legend: subsys {{subsystem}}, level {{level}}, id {{error_id}}

- row: 1
- title: P4 cmds by program - count (rate/10min)
  target:
  - expr: rate(p4_cmd_program_counter{sdpinst="$sdpinst",serverid="$serverid"}[10m])
    legend: "{{program}}"
- title: P4 cmds by program - duration (rate/10min)
  target:
  - expr: rate(p4_cmd_program_cumulative_seconds{sdpinst="$sdpinst",serverid="$serverid"}[10m])
    legend: "{{program}}"

- section: Replication
- row: 1
- title: Replica Journal number (from servers -J on master)
  target:
  - expr: p4_replica_curr_jnl{sdpinst="$sdpinst",serverid="$serverid"}
    legend: "{{servername}}"
- title: Replica Journal Pos (from servers -J on master)
  target:
  - expr: p4_replica_curr_pos{sdpinst="$sdpinst",serverid="$serverid"}
    legend: "{{servername}}"

- row: 1
- title: Replica Lag for sample_replica from sample_master (Update for your site)
  target:
  - expr: >-
      p4_replica_curr_pos{instance="p4poke-chi:9100",job="node_exporter",sdpinst="1",servername="sample_master"} -
      ignoring (serverid, servername)
      p4_replica_curr_pos{instance="p4poke-chi:9100",job="node_exporter",sdpinst="1",servername="sample_replica"}

- row: 1
- title: Pull queue size on replica
  target:
  - expr: p4_pull_queue{sdpinst="$sdpinst"}
- title: rtv_repl_behind_bytes sample_replica (Update for your site)
  target:
  - expr: p4_rtv_rpl_behind_bytes{instance="gemini:9100", job="node_exporter", sdpinst="1", serverid="sample_replica"}

- row: 1
- title: Pull queue errors
  target:
  - expr: p4_pull_errors{sdpinst="$sdpinst",serverid="$serverid"}

- row: 1
- title: Cmd counts per replica IP rate/min
  target:
  - expr: rate(p4_cmd_replica_counter{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "{{replica}}"
- title: Cmd counts per replica IP duration/min
  target:
  - expr: rate(p4_cmd_replica_cumulative_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "{{replica}}"

- row: 1
- title: Files synced (add/update/delete) /min
  target:
  - expr: rate(p4_sync_files_added{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "added"
  - expr: rate(p4_sync_files_updated{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "updated"
  - expr: rate(p4_sync_files_deleted{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "deleted"
- title: Bytes synced (add/update) /min
  target:
  - expr: rate(p4_sync_bytes_added{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "added"
    yformat: decbytes
  - expr: rate(p4_sync_bytes_updated{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "updated"
    yformat: decbytes

- section: Cmd Count and Duration
- row: 1
- title: Cmds duration (rate/min)
  target:
  - expr: rate(p4_cmd_cumulative_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "{{cmd}}"
- title: p4 cmds top 10 (rate/min)
  target:
  - expr: sum without (instance, job)(rate(p4_cmd_counter{sdpinst="$sdpinst",serverid="$serverid"}[1m]))
    legend: "{{cmd}}"

- row: 1
- title: Cmds CPU System (rate/min)
  target:
  - expr: rate(p4_cmd_cpu_system_cumulative_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "{{cmd}}"
- title: Cmds CPU User (rate/min)
  target:
  - expr: rate(p4_cmd_cpu_user_cumulative_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "{{cmd}}"

- row: 1
- title: Cmd Error Counter (rate/min)
  target:
  - expr: rate(p4_cmd_error_counter{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "{{cmd}}"

- title: Trigger duration (rate/min)
  target:
  - expr: rate(p4_total_trigger_lapse_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m])
    legend: "{{trigger}}"

- section: Table Locking
- row: 1
- title: P4 Read Locks
  target:
  - expr: p4_locks_db_read{sdpinst="$sdpinst",serverid="$serverid"}
    legend: db
  - expr: p4_locks_cliententity_read{sdpinst="$sdpinst",serverid="$serverid"}
    legend: cliententity
  - expr: p4_locks_meta_read{sdpinst="$sdpinst",serverid="$serverid"}
    legend: meta
- title: P4 Write Locks
  target:
  - expr: p4_locks_db_write{sdpinst="$sdpinst",serverid="$serverid"}
    legend: db
  - expr: p4_locks_cliententity_write{sdpinst="$sdpinst",serverid="$serverid"}
    legend: cliententity
  - expr: p4_locks_meta_write{sdpinst="$sdpinst",serverid="$serverid"}
    legend: meta

- row: 1
- title: p4 read locks held per table (rate/min)
  target:
  - expr: sum without (instance, job) (rate(p4_total_read_held_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))
    legend: "{{table}}"
- title: p4 read locks waiting (rate/min)
  target:
  - expr: sum without (instance, job) (rate(p4_total_read_wait_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))
    legend: "{{table}}"

- row: 1
- title: p4 write locks held per table (rate/min)
  target:
  - expr: sum without (instance, job) (rate(p4_total_write_held_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))
    legend: "{{table}}"
- title: p4 write locks wait per table (rate/min)
  target:
  - expr: sum without (instance, job) (rate(p4_total_write_wait_seconds{sdpinst="$sdpinst",serverid="$serverid"}[1m]))
    legend: "{{table}}"

- row: 1
- title: RTV processes waiting for locks
  target:
  - expr: p4_rtv_db_lockwait{sdpinst="$sdpinst",serverid="$serverid"}
- title: RTV DB I/O record count
  target:
  - expr: p4_rtv_db_io_records{sdpinst="$sdpinst",serverid="$serverid"}

- row: 1
- title: RTV is checkpoint active
  target:
  - expr: p4_rtv_db_ckp_active{sdpinst="$sdpinst",serverid="$serverid"}
- title: RTV checkpoint records processed
  target:
  - expr: p4_rtv_db_ckp_records{sdpinst="$sdpinst",serverid="$serverid"}
- row: 1
- title: RTV replica byte lag
  target:
  - expr: p4_rtv_rpl_behind_bytes{sdpinst="$sdpinst",serverid="$serverid"}
- title: RTV replica journal lag
  target:
  - expr: p4_rtv_rpl_behind_journals{sdpinst="$sdpinst",serverid="$serverid"}

- row: 1
- title: RTV active sessions
  target:
  - expr: p4_rtv_svr_sessions_active{sdpinst="$sdpinst",serverid="$serverid"}
- title: RTV total sessions
  target:
  - expr: p4_rtv_svr_sessions_total{sdpinst="$sdpinst",serverid="$serverid"}

- row: 1
- title: Processes waiting on read locks
  target:
  - expr: p4_locks_db_read{sdpinst="$sdpinst",serverid="$serverid"}
- title: Processes waiting on write locks
  target:
  - expr: p4_locks_db_write{sdpinst="$sdpinst",serverid="$serverid"}

- row: 1
- title: Processes waiting on cliententity read locks
  target:
  - expr: p4_locks_cliententity_read{sdpinst="$sdpinst",serverid="$serverid"}
- title: Processes waiting on cliententity write locks
  target:
  - expr: p4_locks_cliententity_write{sdpinst="$sdpinst",serverid="$serverid"}

- row: 1
- title: Processes waiting on meta_read locks
  target:
  - expr: p4_locks_meta_read{sdpinst="$sdpinst",serverid="$serverid"}
- title: Processes waiting on meta_write
  target:
  - expr: p4_locks_meta_write{sdpinst="$sdpinst",serverid="$serverid"}

- row: 1
- title: Processes blocked count
  target:
  - expr: p4_locks_cmds_blocked{sdpinst="$sdpinst",serverid="$serverid"}
""", Loader=yaml.FullLoader)


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
        parser.add_argument('--customer', action='store_true', help="Specify that customer variable is defined and included")
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
        if self.options.customer:
            templateList.append(G.Template(
                    default="1",
                    dataSource="default",
                    name="customer",
                    label="Customer",
                    query="label_values(customer)"))
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
            if 'type' in metric and metric['type'] == 'gauge':
                pass
                # text = G.Text(title=metric['title'],
                #                 dataSource='default')
                # dashboard.rows[-1].panels.append(G.Text)
            else:
                yAxis = G.single_y_axis(format="short")
                if 'yformat' in metric:
                    yAxis = G.single_y_axis(format=metric['yformat'])
                graph = G.Graph(title=metric['title'],
                                dataSource='default',
                                maxDataPoints=1000,
                                legend=G.Legend(show=True, alignAsTable=True,
                                                min=True, max=True, avg=True, current=True, total=True,
                                                sort='max', sortDesc=True),
                                yAxes=yAxis)
                refId = 'A'
                for targ in metric['target']:
                    texp = targ['expr']
                    legend = "instance {{instance}}, serverid {{serverid}}"
                    if 'legend' in targ:
                        legend += ' %s' % targ['legend']
                    # Remove SDP
                    if not self.options.use_sdp:
                        texp = texp.replace('sdpinst="$sdpinst",', '')
                    if self.options.customer:
                        texp = texp.replace('{', '{customer="$customer",')
                    graph.targets.append(G.Target(expr=texp,
                                                  legendFormat=legend,
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
