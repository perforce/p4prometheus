# p4logtail

This utility is part of p4prometheus and will continuously tail a p4d log file, writing completed commands
in JSON format to an output file.

Uses [go-libp4dlog](https://github.com/rcowham/go-libp4dlog) for actual log file parsing.

It is intended to provide a simple way to export commands to Elastic Search and similar tools.

## Overview

This can be run as a spawned process or packaged up via `systemd`

## Configuration

The config file is by default `p4logtail.yaml`:

```yaml
# p4_log: the p4d log file, e.g. SDP would be /p4/1/logs/log
# This file can be rotated without issue
p4_log:     /p4/1/logs/log
# json_log: the output JSON file containing one line per completed command.
# Can also be rotated together with the p4_log
json_log:   /p4/1/logs/log.json
```

## Running the process

```
$ ./p4logtail -h
usage: p4logtail [<flags>]


Flags:
  -h, --help                     Show context-sensitive help (also try --help-long and --help-man).
  -c, --config="p4logtail.yaml"  Config file for p4logtail.
      --p4log=""                 P4LOG file to process (overrides value in config file if specified)
      --jsonlog=""               Name of ouput file in JSON format (overrides value in config file if specified)
      --debug                    Enable debugging.
      --version                  Show application version.
```

