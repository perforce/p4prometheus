# Tests for Prometheus and Grafana

from time import sleep

def test_grafana_is_installed(host):
    grafana = host.package("grafana")
    assert grafana.is_installed


def test_services_running_and_enabled(host):
    for service in ["prometheus", "grafana-server", "victoria-metrics", "alertmanager"]:
        s = host.service(service)
        assert s.is_running
        assert s.is_enabled
