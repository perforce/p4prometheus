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

    It takes as input a YAML config file which specifies the dashboard. Please customize this
    config file as some values include things like server_id which are site specific.

    The resulting dashboard can be easily uploaded to Grafana by one one of these methods:

    * directly using Grafana API and specifying url/api key token

    * with associated script which takes json output file

        upload_grafana_dashboard.sh
    
    * outputting json to a file and copy and pasting it in to Grafana

USAGE:
    ./create_dashboard.py -h

    Edit config file (default 'dashboard.yaml') and customize it.
    
    Create and upload the dashboard:

    ./create_dashboard.py --title "P4Prometheus dashboard" -c dashboard.yaml --url http://p4monitor:3000 --api-key "Grafana-API-key"

    Alternatively:
    
    ./create_dashboard.py --title "P4Prometheus dashboard" -c dashboard.yaml > dash.json
    ./upload_grafana_dashboard.sh

"""

import textwrap
import argparse
import sys
import io
import yaml
import json
import grafanalib.core as G
from grafanalib._gen import write_dashboard, DashboardEncoder
# import http
import requests


DEFAULT_TITLE = "P4Prometheus Metrics"
DEFAULT_CONFIG = "dashboard.yaml"
# http.client.HTTPConnection.debuglevel = 1


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
            epilog="Copyright (c) 2021-2 Perforce Software, Inc."
        )
        self.add_parse_args(parser)
        self.options = parser.parse_args(args=args)
        self.options.use_sdp = not self.options.no_sdp

    def add_parse_args(self, parser):
        """Default arguments"""
        parser.add_argument('-t', '--title', default=DEFAULT_TITLE, help="Dashboard title. Default: " + DEFAULT_TITLE)
        parser.add_argument('-c', '--config', default=DEFAULT_CONFIG, help="Dashboard config YAML file. Default: " + DEFAULT_CONFIG)
        parser.add_argument('--customer', action='store_true', help="Specify that customer variable is defined and included")
        parser.add_argument('--no-sdp', action='store_true', default=False, help="Whether this is SDP instance or not - default is SDP")
        parser.add_argument('--filter-labels', action='store_true', default=False, help="Whether to filter labels by SDP or not")
        parser.add_argument('-a', '--api-key', help="Grafana API key token")
        parser.add_argument('--datasource', help="Grafana datasource name (otherwise uses default)")
        parser.add_argument('--list-datasources', action='store_true', default=False, 
                            help="Calls Grafana API to list datasources - output can be used with --datasource. " +
                            " This command will not upload anything.")
        parser.add_argument('-u', '--url', help="Grafana url base, e.g. http://server or https://server")

    def run(self):
        
        if self.options.list_datasources:
            if not self.options.url or not self.options.api_key:
                raise Exception("You must specify --url and --api-key when you specify --list-datasources")
            headers = {"Authorization": "Bearer " + self.options.api_key, "Content-Type": "application/json"}
            url = self.options.url + "/api/datasources"
            response = requests.get(url, headers=headers)
            for j in response.json():
                if 'name' in j and 'uid' in j:
                    print("Name: %s, uid: %s" % (j['name'], j['uid']))
            return

        try:
            with open(self.options.config) as f:
                self.config = yaml.load(f, Loader=yaml.FullLoader)
        except Exception as e:
            raise Exception('Could not read config file %s: %s' % (self.options.config, str(e)))

        dataSource = 'default'
        if self.options.datasource:
            dataSource = self.options.datasource

        templateList = []
        serverid_query = "label_values(serverid)"
        sdpinst_query = "label_values(sdpinst)"
        # Customer first variable if required
        if self.options.customer:
            serverid_query = 'label_values(p4_prom_log_lines_read{customer=~"$customer"}, serverid)'
            sdpinst_query = 'label_values(p4_prom_log_lines_read{customer=~"$customer"}, sdpinst)'
            if self.options.filter_labels:
                if self.options.use_sdp:
                    serverid_query = 'label_values(p4_prom_log_lines_read{customer=~"$customer",sdpinst!=""}, serverid)'
                    sdpinst_query = 'label_values(p4_prom_log_lines_read{customer=~"$customer",sdpinst!=""}, sdpinst)'
                else:
                    serverid_query = "label_values(p4_prom_log_lines_read{sdpinst=""}, serverid)"
                    sdpinst_query = "label_values(p4_prom_log_lines_read{sdpinst=""}, sdpinst)"
            templateList.append(G.Template(
                    default="1",
                    dataSource=dataSource,
                    name="customer",
                    label="Customer",
                    query="label_values(customer)"))
        else:
            if self.options.filter_labels:
                if self.options.use_sdp:
                    serverid_query = 'label_values(p4_prom_log_lines_read{sdpinst!=""}, serverid)'
                    sdpinst_query = 'label_values(p4_prom_log_lines_read{sdpinst!=""}, sdpinst)'
                else:
                    serverid_query = 'label_values(p4_prom_log_lines_read{sdpinst=""}, serverid)'
                    sdpinst_query = 'label_values(p4_prom_log_lines_read{sdpinst=""}, sdpinst)'
        templateList.append(G.Template(
                    default="",
                    dataSource=dataSource,
                    name="serverid",
                    label="ServerID",
                    query=serverid_query))
        if self.options.use_sdp:
            templateList.append(G.Template(
                    default="1",
                    dataSource=dataSource,
                    name="sdpinst",
                    label="SDPInstance",
                    query=sdpinst_query))

        dashboard = G.Dashboard(
            title=self.options.title,
            templating=G.Templating(list=templateList)
        )

        for metric in self.config:
            if 'section' in metric:
                dashboard.rows.append(G.Row(title=metric['section'], showTitle=True))
                continue
            if 'row' in metric:
                dashboard.rows.append(G.Row(title='', showTitle=False))
                continue
            if 'type' in metric and metric['type'] == 'gauge':
                pass
            else: # graph
                yAxis = G.single_y_axis(format="short")
                if 'yformat' in metric:
                    yAxis = G.single_y_axis(format=metric['yformat'])
                graph = G.Graph(title=metric['title'],
                                dataSource=dataSource,
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
        if self.options.url and self.options.api_key:
            headers = {"Authorization": "Bearer " + self.options.api_key, "Content-Type": "application/json"}
            url = self.options.url + "/api/dashboards/db"
            response = requests.post(
                url,
                data=json.dumps({
                    "dashboard": dashboard.to_json_data(),
                    "overwrite": True,
                    "message": "Initialise"
                }, sort_keys=True, indent=2, cls=DashboardEncoder),
                headers=headers,
            )
            print(response.text)
        else:
            print("""{"dashboard": %s}""" % s.getvalue())


if __name__ == '__main__':
    """ Main Program"""
    obj = CreateDashboard(*sys.argv[1:])
    obj.run()
