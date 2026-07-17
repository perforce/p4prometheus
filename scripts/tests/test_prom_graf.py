# Tests for Prometheus and Grafana

def test_grafana_is_installed(host):
    grafana = host.package("grafana")
    assert grafana.is_installed


def test_core_services_running_and_enabled(host):
    for service in ["prometheus", "grafana-server", "victoria-metrics", "alertmanager", "pint", "node_exporter"]:
        s = host.service(service)
        assert s.is_running
        assert s.is_enabled


def test_http_health_endpoints(host):
    for endpoint in [
        "http://localhost:9090/-/healthy",
        "http://localhost:8428/health",
        "http://localhost:9093/-/healthy",
        "http://localhost:3000/api/health",
        "http://localhost:9100/metrics",
    ]:
        r = host.run(f"curl -fsS --max-time 5 {endpoint}")
        assert r.rc == 0


def test_core_binaries_exist_and_executable(host):
    for binary in [
        "/usr/local/bin/prometheus",
        "/usr/local/bin/promtool",
        "/usr/local/bin/victoria-metrics-prod",
        "/usr/local/bin/alertmanager",
        "/usr/local/bin/node_exporter",
        "/usr/local/bin/pint",
    ]:
        f = host.file(binary)
        assert f.exists
        assert f.mode & 0o111


def test_expected_config_files_exist(host):
    for path in [
        "/etc/prometheus/prometheus.yml",
        "/etc/prometheus/perforce_rules.yml",
        "/etc/prometheus/pint_vm.hcl",
        "/etc/alertmanager/alertmanager.yml",
        "/etc/alertmanager/templates/perforce.tmpl",
        "/etc/grafana/provisioning/datasources/p4prometheus-victoria-metrics.yaml",
        "/etc/grafana/provisioning/dashboards/p4prometheus-dashboards.yaml",
    ]:
        f = host.file(path)
        assert f.exists
        assert f.size > 0


def test_prometheus_config_has_expected_sections(host):
    prom_cfg = host.file("/etc/prometheus/prometheus.yml")
    assert prom_cfg.contains(r"rule_files:")
    assert prom_cfg.contains(r"/etc/prometheus/perforce_rules.yml")
    assert prom_cfg.contains(r"localhost:9100")
    assert prom_cfg.contains(r"myp4:9100")
    assert prom_cfg.contains(r"myreplica:9100")
    assert prom_cfg.contains(r"http://localhost:8428/api/v1/write")


def test_alertmanager_config_uses_templates(host):
    am_cfg = host.file("/etc/alertmanager/alertmanager.yml")
    assert am_cfg.contains(r"templates:")
    assert am_cfg.contains(r"templates/\*\.tmpl")


def test_pint_config_targets_victoria_metrics(host):
    pint_cfg = host.file("/etc/prometheus/pint_vm.hcl")
    assert pint_cfg.contains(r"victoria_metrics\s*=\s*true")
    assert pint_cfg.contains(r"http://localhost:8428")


def test_pint_service_execstart(host):
    unit = host.file("/etc/systemd/system/pint.service")
    assert unit.exists
    assert unit.contains(r"ExecStart=/usr/local/bin/pint watch glob /etc/prometheus/perforce_rules.yml --config /etc/prometheus/pint_vm.hcl")


def test_grafana_dashboards_downloaded(host):
    for dash_id in ["12278", "15509", "405", "1860"]:
        dash = host.file(f"/var/lib/grafana/dashboards/p4prometheus/{dash_id}.json")
        assert dash.exists
        assert dash.size > 0


# def test_pushgateway_not_installed_by_default(host):
#     push_svc = host.service("pushgateway")
#     assert not push_svc.is_running
#     assert not push_svc.is_enabled

#     prom_cfg = host.file("/etc/prometheus/prometheus.yml")
#     assert "job_name: 'pushgateway'" not in prom_cfg.content_string


def test_prometheus_and_alertmanager_configs_validate(host):
    promtool = host.run("/usr/local/bin/promtool check config /etc/prometheus/prometheus.yml")
    assert promtool.rc == 0

    amtool = host.run("/usr/local/bin/amtool check-config /etc/alertmanager/alertmanager.yml")
    assert amtool.rc == 0


def test_expected_file_ownership_and_permissions(host):
    # Prometheus-managed files
    for path in [
        "/etc/prometheus/prometheus.yml",
        "/etc/prometheus/perforce_rules.yml",
        "/etc/prometheus/pint_vm.hcl",
    ]:
        f = host.file(path)
        assert f.user == "prometheus"
        assert f.group == "prometheus"
        assert oct(f.mode & 0o777) == "0o644"

    # Alertmanager-managed files
    for path in [
        "/etc/alertmanager/alertmanager.yml",
        "/etc/alertmanager/templates/perforce.tmpl",
    ]:
        f = host.file(path)
        assert f.user == "alertmanager"
        assert f.group == "alertmanager"
        assert oct(f.mode & 0o777) == "0o644"

    # Grafana-downloaded dashboard files
    for dash_id in ["12278", "15509", "405", "1860"]:
        f = host.file(f"/var/lib/grafana/dashboards/p4prometheus/{dash_id}.json")
        assert f.user == "grafana"
        assert f.group == "grafana"
        assert oct(f.mode & 0o777) == "0o644"
