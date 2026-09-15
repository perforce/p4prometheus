# P4Prometheus Installation

The recommended installation and upgrade path uses the supplied scripts. The canonical guide is [P4Prometheus Installation](doc/P4Prometheus_Installation.adoc).

It covers:

- Automated installation for monitoring servers, Helix Core servers, and other monitored hosts.
- Upgrades, offline installations, persistent state, dedicated data volumes, retention, and additional scrape targets.
- Verification and troubleshooting.
- Component-by-component manual installation for environments where scripts cannot be used.

For the available script options, run the applicable installer with `-h`:

```bash
./scripts/install_prom_graf.sh -h
./scripts/install_p4prom.sh -h
./scripts/install_node.sh -h
```
