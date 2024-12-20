# Example Configuration for Prometheus and Alertmanager

This directory contains some sample files which you may wish to build on, including:

* Sample [perforce_rules.yml](prometheus/perforce_rules.yml) which is referenced from [prometheus.yml](prometheus/prometheus.yml)
* Sample [alertmanager.yml](alertmanager/alertmanager.yml)
* Sample [alertmanager templates for use with Slack messages](alertmanager/templates/perforce.tmpl)

The above includes a link to a runbook, and direct to grafana relevant dashboards.

Note that in the alertmanager example there are a couple of `test*.json` files which can be used to test template formatting
(which is a bit fiddly/finicky to say the least!).

E.g.

    make test

Will give you formatted output according to the templates in the `templates/` dir.
