# p4metrics

This is a version of monitor_metrics.sh in Go as part of p4prometheus
It is intended to be more reliable and cross platform than the original.
It should be run permanently as a systemd service on Linux, as it tails
the errors.csv file

Works fine on Windows!

This replaces the (now deprecated) [monitor_metrics.sh](../../scripts/monitor_metrics.sh) script - which was hard to use on Windows, and lacked some functionality.

# Usage

```
./p4metrics -h
usage: p4metrics [<flags>]

Flags:
  -h, --help                     Show context-sensitive help (also try --help-long and --help-man).
  -c, --config="p4metrics.yaml"  Config file for p4prometheus.
      --sdp.instance=""          SDP Instance, typically 1 or alphanumeric.
      --p4port=""                P4PORT to use (if sdp.instance is not set).
      --p4user=""                P4USER to use (if sdp.instance is not set).
      --p4config=""              P4CONFIG file to use (if sdp.instance is not set and no value in config file).
      --debug                    Enable debugging.
  -n, --dry.run                  Don't write metrics - but show the results - useful for debugging with --debug.
  -C, --sample.config            Output a sample config file and exit. Useful for getting started to create p4metrics.yaml. E.g. p4metrics --sample.config > p4metrics.yaml
      --version                  Show application version.
```

Debugging (in bash):

```bash
nohup ./p4metrics --config p4metrics.yaml --debug --dry.run > out.txt &
# Examine the context - shows metrics being written and commands being run without actually updating any *.prom files
less out.txt
kill %1     # Kill the running task when happy
```

# Config file p4metrics.yaml

Run `p4metrics --sample.config > p4metrics.yaml` to create an example.

Note that [the installer](../../scripts/install_p4prom.sh) will create a default version of this file - suitable for SDP and non-SDP

# Design

The basics are:

* Run as systemd service (as constantly tailing the structured error log (errors.csv)
* Poll p4d, swarm etc as per the dealy in the config file
* Handle p4d service being down (so don't die but wait for next cycle!)
