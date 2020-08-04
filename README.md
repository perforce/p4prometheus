![Support](https://img.shields.io/badge/Support-Community-yellow.svg)

# p4prometheus

Utility which integrates Perforce (Helix Core) with Prometheus. If performs real-time analysis of p4d log files feeding to a dashboard and for system alerting.

It continuously parses p4d log files and writes a summary to 
a specified Prometheus compatible metrics file which can be handled via the `node_exporter`
textfile collector module.

Uses [go-libp4dlog](https://github.com/rcowham/go-libp4dlog) for actual log file parsing.

- [p4prometheus](#p4prometheus)
  - [Support Status](#support-status)
  - [Overview](#overview)
- [Grafana Dashboards](#grafana-dashboards)
- [Detailed Installation](#detailed-installation)

## Support Status

This is currently a Community Supported Perforce tool.

## Overview

This is part of a solution consisting of the following components:

* Prometheus - time series metrics management system: https://prometheus.io/
* Grafana - The leading open source software for time series analytics - https://grafana.com/
* node_exporter - Prometheus collector for basic Linux metrics - https://github.com/prometheus/node_exporter

Two custom components:

* p4prometheus - This component.
* monitor_metrics.sh - [SDP](https://swarm.workshop.perforce.com/projects/perforce-software-sdp) compatible bash script to generate simple supplementary metrics - [monitor_metrics.sh](demo/monitor_metrics.sh)

Check out the ![Prometheus architecture](https://prometheus.io/assets/architecture.png)
The custom components referred to above are "Prometheus targets".

# Grafana Dashboards

When installed and setup, you can get dashboards such as the following:

Commands Summary:

![Commands Summary](images/p4stats_cmds_summary.png)

Rates for command durations and count:

![Commands](images/p4stats_cmds.png)

Active commands (monitor):

![Commands](images/p4stats_monitor.png)

Replication status:

![Commands](images/p4stats_replication.png)

Read/write locks held/waiting status:

![Commands](images/p4stats_table_read_locks.png)

Dashboard alerts can be defined, as well as alert rules which are actioned by [alertmanager](https://prometheus.io/docs/alerting/alertmanager/) - see installation below for link to examples.

# Detailed Installation

You need to install Prometheus and Grafana using standard methods. This is typically done on a seperate VM/machine to the Perforce server itself (for security and HA reasons).

The easiest way is to use Ansible with a Galaxy module.

Example files are to be found in the [demo folder](demo/) for this project which is an (as yet incomplete) Docker Compose demonstrator.

Note that all the components do run on Windows but you will need an appropriate Service wrapper.

See [Detailed Instatallation Instructions (INSTALL.md)](INSTALL.md) in this project.
