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
