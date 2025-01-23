package config

import (
	"testing"
	"time"
)

const defaultConfig = `
metrics_output:				/hxlogs/metrics/p4_metrics.prom
server_id:					myserverid
sdp_instance: 				1
update_interval: 			60s
monitor_swarm:		 		false
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

func checkValueBool(t *testing.T, fieldname string, val bool, expected bool) {
	if val != expected {
		t.Fatalf("Error parsing %s, expected %v got %v", fieldname, expected, val)
	}
}

func TestValidConfig(t *testing.T) {
	cfg := loadOrFail(t, defaultConfig)
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/p4_metrics.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "1")
	checkValueDuration(t, "UpdateInterval", cfg.UpdateInterval, 60*time.Second)
}

func ensureFail(t *testing.T, cfgString string, desc string) {
	_, err := Unmarshal([]byte(cfgString))
	if err == nil {
		t.Fatalf("Expected config err not found: %s", desc)
	}
	t.Logf("Config err: %v", err.Error())
}

func loadOrFail(t *testing.T, cfgString string) *Config {
	cfg, err := Unmarshal([]byte(cfgString))
	if err != nil {
		t.Fatalf("Failed to read config: %v", err.Error())
	}
	return cfg
}
