#!/bin/bash
# Uses VictoriaMetrics rule replay/backfilling feature.

#/etc/prometheus/temp/vmalert-prod -rule=/etc/prometheus/perforce_rules.yml \
/usr/local/bin/vmalert-prod -rule=/etc/prometheus/perforce_rules.yml \
	-datasource.url=http://localhost:8428 \
	-remoteWrite.url=http://localhost:8428 \
	-replay.timeFrom=2024-12-17T00:00:00Z \
	-replay.timeTo=2024-12-17T23:00:00Z

echo "Now search in Explore for metric ALERTS and pick an alert name..."
echo " See: https://docs.victoriametrics.com/vmalert.html#rules-backfilling"
