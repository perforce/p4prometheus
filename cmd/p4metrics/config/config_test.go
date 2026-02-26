package config

import (
	"testing"
	"time"
)

const defaultConfig = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
update_interval: 			60s
monitor_swarm:		 		false
`

const config2 = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
update_interval: 			60s
monitor_swarm:		 		false
max_journal_size:			100M
max_journal_percent:		40
max_log_size:				10.3G
max_log_percent:			30
`

func checkValue(t *testing.T, fieldname string, val string, expected string) {
	if val != expected {
		t.Fatalf("Error parsing %s, expected %v got %v", fieldname, expected, val)
	}
}

func checkValueDuration(t *testing.T, fieldname string, val time.Duration, expected time.Duration) {
	if val != expected {
		t.Fatalf("Error parsing %s, expected %v got %v", fieldname, expected, val)
	}
}

func checkValueInt(t *testing.T, fieldname string, val int64, expected int64) {
	if val != expected {
		t.Fatalf("Error parsing %s, expected %v got %v", fieldname, expected, val)
	}
}

func TestValidConfig(t *testing.T) {
	cfg := loadOrFail(t, defaultConfig)
	checkValue(t, "MetricsRoot", cfg.MetricsRoot, "/hxlogs/metrics")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "1")
	checkValueDuration(t, "UpdateInterval", cfg.UpdateInterval, 60*time.Second)
}

func TestValidConfig2(t *testing.T) {
	cfg := loadOrFail(t, config2)
	checkValue(t, "MaxJournalSize", cfg.MaxJournalSize, "100M")
	checkValue(t, "MaxLogSize", cfg.MaxLogSize, "10.3G")
	checkValue(t, "MaxJournalPercent", cfg.MaxJournalPercent, "40")
	checkValue(t, "MaxLogPercent", cfg.MaxLogPercent, "30")
}

const config3 = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
max_journal_size:			100A
`

const config4 = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
max_journal_percent:		101
`

const config5 = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
max_log_size:				10.3Z
`

const config6 = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
max_log_percent:			30$
`

func TestInvalidConfig(t *testing.T) {
	ensureFail(t, config3, "invalid max_journal_size")
	ensureFail(t, config4, "invalid max_journal_percent")
	ensureFail(t, config5, "invalid max_log_size")
	ensureFail(t, config6, "invalid max_log_percent")
}

func ensureFail(t *testing.T, cfgString string, desc string) {
	_, err := Unmarshal([]byte(cfgString))
	if err == nil {
		t.Fatalf("Expected config err not found: %s", desc)
	}
	// t.Logf("Config  found: %v", err.Error())
}

func loadOrFail(t *testing.T, cfgString string) *Config {
	cfg, err := Unmarshal([]byte(cfgString))
	if err != nil {
		t.Fatalf("Failed to read config: %v", err.Error())
	}
	return cfg
}

const configWithMonitorGroups = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
monitor_groups:
- commands: "sync|transmit"
  label: sync_transmit
- commands: "shelve|unshelve"
  label: shelf_ops
`

const configWithInvalidGroupRegex = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
monitor_groups:
- commands: "sync|[invalid"
  label: sync_ops
`

const configWithInvalidGroupName = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
monitor_groups:
- commands: "sync|transmit"
  label: "sync transmit"
`

const configWithEmptyCommands = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
monitor_groups:
- commands: ""
  label: sync_ops
`

const configWithEmptyGroup = `
metrics_root:				/hxlogs/metrics
sdp_instance: 				1
monitor_groups:
- commands: "sync"
  label: ""
`

func TestValidMonitorGroups(t *testing.T) {
	cfg := loadOrFail(t, configWithMonitorGroups)
	if len(cfg.MonitorGroups) != 2 {
		t.Fatalf("Expected 2 monitor groups, got %d", len(cfg.MonitorGroups))
	}
	checkValue(t, "MonitorGroups[0].Commands", cfg.MonitorGroups[0].Commands, "sync|transmit")
	checkValue(t, "MonitorGroups[0].Label", cfg.MonitorGroups[0].Label, "sync_transmit")
	checkValue(t, "MonitorGroups[1].Commands", cfg.MonitorGroups[1].Commands, "shelve|unshelve")
	checkValue(t, "MonitorGroups[1].Label", cfg.MonitorGroups[1].Label, "shelf_ops")
}

func TestInvalidMonitorGroups(t *testing.T) {
	ensureFail(t, configWithInvalidGroupRegex, "invalid regex in commands")
	ensureFail(t, configWithInvalidGroupName, "invalid characters in group name")
	ensureFail(t, configWithEmptyCommands, "empty commands field")
	ensureFail(t, configWithEmptyGroup, "empty group field")
}
