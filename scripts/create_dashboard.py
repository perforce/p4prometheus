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

    * directly using Grafana API and specifying url/api key token (or putting in environment)

    * with associated script which takes json output file

        upload_grafana_dashboard.sh
    
    * outputting json to a file and copy and pasting it in to Grafana

USAGE:
        ./create_dashboard.py -h

    Edit config file (default 'dashboard.yaml') and customize it.
    
    Create and upload the dashboard:

        ./create_dashboard.py --title "P4Prometheus dashboard" -c dashboard.yaml --url http://p4monitor:3000 --api-key "Grafana-API-key"

    Using environment variables: 
    
        export GRAFANA_SERVER=https://p4monitor:3000
        export GRAFANA_API_KEY="oe...=="

        ./create_dashboard.py --title "P4Prometheus dashboard" -c dashboard.yaml

    Alternatively:
    
        ./create_dashboard.py --title "P4Prometheus dashboard" -c dashboard.yaml > dash.json
        ./upload_grafana_dashboard.sh

"""

import textwrap
import argparse
import os
import sys
import warnings
import yaml
import json
import grafanalib.core as G
# import http
import requests


DEFAULT_TITLE = "P4Prometheus Metrics"
DEFAULT_CONFIG = "dashboard.yaml"
DEFAULT_HTTP_TIMEOUT = 15
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
            epilog="Copyright (c) 2021-6 Perforce Software, Inc."
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
        parser.add_argument('-a', '--api-key', dest='api_key',
                            help="Grafana service account token (or legacy API key) - defaults to $GRAFANA_API_TOKEN or $GRAFANA_API_KEY")
        parser.add_argument('--api-token', dest='api_key',
                            help="Alias for --api-key")
        parser.add_argument('--datasource', help="Grafana datasource name (otherwise uses default)")
        parser.add_argument('--list-datasources', action='store_true', default=False, 
                            help="Calls Grafana API to list datasources - output can be used with --datasource. " +
                            " This command will not upload anything.")
        parser.add_argument('-u', '--url', help="Grafana url base, e.g. http://server or https://server - default $GRAFANA_SERVER")
        parser.add_argument('--ca-cert', dest='ca_cert',
                    help="Path to CA bundle for Grafana HTTPS verification (defaults to $GRAFANA_CA_CERT)")
        parser.add_argument('--insecure-skip-verify', action='store_true', default=False,
                    help="Disable TLS certificate verification for Grafana HTTPS requests (defaults to $GRAFANA_INSECURE_SKIP_VERIFY)")

    def _bearer_headers(self):
        return {"Authorization": "Bearer " + self.options.api_key, "Content-Type": "application/json"}

    def _tls_verify_setting(self):
        """Return requests verify setting: True, False, or CA bundle path."""
        if self.options.insecure_skip_verify:
            return False
        if self.options.ca_cert:
            return self.options.ca_cert
        return True

    def _request_json(self, method, url, **kwargs):
        """Issue HTTP request with timeout and raise useful errors for Grafana API failures."""
        kwargs.setdefault("timeout", DEFAULT_HTTP_TIMEOUT)
        kwargs.setdefault("verify", self._tls_verify_setting())
        response = requests.request(method, url, **kwargs)
        try:
            response.raise_for_status()
        except requests.HTTPError as e:
            body = response.text.strip()
            if len(body) > 500:
                body = body[:500] + "..."
            raise Exception("Grafana API request failed (%s %s): HTTP %s: %s" %
                            (method, url, response.status_code, body)) from e
        return response

    def _modernize_panel(self, panel):
        """Convert legacy panel JSON to Grafana 13-friendly panel definitions."""
        # grafanalib may still hand us Row/Panel objects in some paths.
        # Convert where possible, and ignore anything we cannot treat as a dict.
        if panel is None:
            return panel
        if not isinstance(panel, dict):
            if hasattr(panel, "to_json_data"):
                panel = panel.to_json_data()
            else:
                return panel

        ptype = panel.get("type")

        if ptype == "graph":
            panel["type"] = "timeseries"
            panel.setdefault("options", {})
            panel["options"].setdefault("legend", {
                "displayMode": "table",
                "placement": "bottom",
                "showLegend": True,
            })
            panel["options"].setdefault("tooltip", {
                "mode": "multi",
                "sort": "none",
            })
            panel.setdefault("fieldConfig", {
                "defaults": {
                    "unit": "short",
                    "mappings": [],
                    "thresholds": {
                        "mode": "absolute",
                        "steps": [{"color": "green", "value": None}],
                    },
                    "custom": {
                        "drawStyle": "line",
                        "lineInterpolation": "linear",
                        "barAlignment": 0,
                        "lineWidth": 1,
                        "fillOpacity": 10,
                        "gradientMode": "none",
                        "spanNulls": False,
                        "showPoints": "never",
                        "pointSize": 5,
                        "stacking": {"mode": "none", "group": "A"},
                        "axisPlacement": "auto",
                        "axisLabel": "",
                        "axisColorMode": "text",
                        "axisBorderShow": False,
                        "scaleDistribution": {"type": "linear"},
                        "axisCenteredZero": False,
                        "hideFrom": {
                            "tooltip": False,
                            "viz": False,
                            "legend": False,
                        },
                    },
                },
                "overrides": [],
            })

            # Remove legacy graph fields to avoid old migration paths.
            for k in [
                "aliasColors", "bars", "dashLength", "dashes", "fill", "fillGradient",
                "hiddenSeries", "legend", "lines", "linewidth", "nullPointMode",
                "percentage", "pointradius", "points", "renderer", "seriesOverrides",
                "spaceLength", "stack", "steppedLine", "thresholds", "timeRegions",
                "tooltip", "xaxis", "yaxes", "yaxis",
            ]:
                panel.pop(k, None)

        elif ptype == "singlestat":
            panel["type"] = "stat"
            panel.setdefault("options", {
                "reduceOptions": {
                    "values": False,
                    "calcs": ["lastNotNull"],
                    "fields": "",
                },
                "orientation": "auto",
                "textMode": "auto",
                "colorMode": "value",
                "graphMode": "none",
                "justifyMode": "auto",
            })
            panel.setdefault("fieldConfig", {
                "defaults": {
                    "unit": "short",
                    "mappings": [],
                    "thresholds": {
                        "mode": "absolute",
                        "steps": [
                            {"color": "green", "value": None},
                            {"color": "red", "value": 80},
                        ],
                    },
                },
                "overrides": [],
            })

            for k in [
                "colorBackground", "colorValue", "colors", "gauge", "mappingType",
                "rangeMaps", "sparkline", "valueFontSize", "valueMaps", "valueName",
            ]:
                panel.pop(k, None)

        for child in panel.get("panels", []):
            self._modernize_panel(child)
        return panel

    def _modernize_dashboard_json(self, dashboard_data):
        """Walk dashboard JSON and modernize legacy panels recursively."""
        for panel in dashboard_data.get("panels", []):
            self._modernize_panel(panel)
        for row in dashboard_data.get("rows", []):
            row_data = self._modernize_panel(row)
            if isinstance(row_data, dict):
                row_panels = row_data.get("panels", [])
            elif isinstance(row, dict):
                row_panels = row.get("panels", [])
            else:
                row_panels = []
            for panel in row_panels:
                self._modernize_panel(panel)
        return dashboard_data

    def _normalize_json_types(self, value):
        """Recursively convert grafanalib objects to plain JSON-serializable types."""
        if value is None or isinstance(value, (str, int, float, bool)):
            return value
        if isinstance(value, dict):
            normalized = {}
            for k, v in value.items():
                normalized[k] = self._normalize_json_types(v)
            return normalized
        if isinstance(value, list):
            return [self._normalize_json_types(v) for v in value]
        if isinstance(value, tuple):
            return [self._normalize_json_types(v) for v in value]
        if hasattr(value, "to_json_data"):
            return self._normalize_json_types(value.to_json_data())
        if hasattr(value, "__dict__"):
            return self._normalize_json_types(value.__dict__)
        return str(value)

    def run(self):
        
        if not self.options.url:
            self.options.url = os.getenv('GRAFANA_SERVER')
        if not self.options.api_key:
            self.options.api_key = os.getenv('GRAFANA_API_TOKEN') or os.getenv('GRAFANA_API_KEY')
        if not self.options.ca_cert:
            self.options.ca_cert = os.getenv('GRAFANA_CA_CERT')
        if not self.options.insecure_skip_verify:
            insecure_env = (os.getenv('GRAFANA_INSECURE_SKIP_VERIFY') or '').strip().lower()
            self.options.insecure_skip_verify = insecure_env in ('1', 'true', 'yes', 'on')

        if self.options.insecure_skip_verify:
            warnings.filterwarnings('ignore', message='Unverified HTTPS request')

        if self.options.list_datasources:
            if not self.options.url or not self.options.api_key:
                raise Exception("You must specify --url and --api-key when you specify --list-datasources")
            headers = self._bearer_headers()
            url = self.options.url + "/api/datasources"
            response = self._request_json("GET", url, headers=headers)
            data = response.json()
            if not isinstance(data, list):
                raise Exception("Unexpected response from Grafana datasources API: %s" % type(data).__name__)
            for j in data:
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

        # Default is 2 panels per row - so half width
        # We set grid x and y
        panelsInRow = x = y = 0
        for metric in self.config:
            if 'section' in metric:
                dashboard.rows.append(G.Row(title=metric['section'], showTitle=True))
                panelsInRow = x = y = 0
                continue
            if 'row' in metric:
                dashboard.rows.append(G.Row(title='', showTitle=False))
                panelsInRow = x = y = 0
                continue
            if 'type' in metric and metric['type'] == 'gauge':
                pass
            else: # graph
                panelsInRow += 1
                if panelsInRow > 2:
                    y += 30
                if panelsInRow % 2 == 0:
                    x = 12
                yAxis = G.single_y_axis(format="short")
                if 'yformat' in metric:
                    yAxis = G.single_y_axis(format=metric['yformat'])
                graph = G.Graph(title=metric['title'],
                                dataSource=dataSource,
                                maxDataPoints=1000,
                                legend=G.Legend(show=True, alignAsTable=True,
                                                min=True, max=True, avg=True, current=True, total=True,
                                                sort='max', sortDesc=True),
                                yAxes=yAxis,
                                gridPos=G.GridPos(h=0, w=12, x=x, y=y)) # Half width panels
                refId = 'A'
                for targ in metric['target']:
                    texp = targ['expr']
                    legend = ""
                    if self.options.customer:
                        legend = "{{customer}}, "
                    legend += "instance {{instance}}, serverid {{serverid}}"
                    if 'legend' in targ:
                        legend += ' %s' % targ['legend']
                    if not self.options.use_sdp: # Remove the SDP tag
                        texp = texp.replace('sdpinst="$sdpinst",', '')
                    if self.options.customer: # Add customer tag
                        texp = texp.replace('{', '{customer="$customer",')
                    else:
                        texp = texp.replace('on (customer, ', 'on (') # Remove customer from any on expressions
                    graph.targets.append(G.Target(expr=texp,
                                                  legendFormat=legend,
                                                  refId=refId))
                    refId = chr(ord(refId) + 1)
                dashboard.rows[-1].panels.append(graph)

        # Auto-number panels - returns new dashboard
        dashboard = dashboard.auto_panel_ids()

        dashboard_data = self._modernize_dashboard_json(dashboard.to_json_data())
        dashboard_data = self._normalize_json_types(dashboard_data)
        dashboard_json = json.dumps(dashboard_data, sort_keys=True, indent=2)
        if self.options.url and self.options.api_key:
            headers = self._bearer_headers()
            url = self.options.url + "/api/dashboards/db"
            response = self._request_json("POST", url,
                                          data=json.dumps({
                                              "dashboard": dashboard_data,
                                              "overwrite": True,
                                              "message": "Initialise"
                                          }, sort_keys=True, indent=2),
                                          headers=headers)
            print(response.text)
        else:
            print("""{"dashboard": %s}""" % dashboard_json)


if __name__ == '__main__':
    """ Main Program"""
    obj = CreateDashboard(*sys.argv[1:])
    obj.run()
