package config

import (
	"testing"
)

const defaultConfig = `
log_path:		/p4/1/logs/log
metrics_output:	/hxlogs/metrics/cmds.prom
server_id:		myserverid
sdp_instance: 	1
`

const nonSDPConfig1 = `
log_path:		/p4/1/logs/log
metrics_output:	/hxlogs/metrics/cmds.prom
server_id:		myserverid
`

const nonSDPConfig2 = `
log_path:		/p4/1/logs/log
metrics_output:	/hxlogs/metrics/cmds.prom
server_id:		myserverid
sdp_instance:
`

func checkValue(t *testing.T, fieldname string, val string, expected string) {
	if val != expected {
		t.Fatalf("Error parsing %s, expected %v got %v", fieldname, expected, val)
	}
}

func TestValidConfig(t *testing.T) {
	cfg := loadOrFail(t, defaultConfig)
	checkValue(t, "LogPath", cfg.LogPath, "/p4/1/logs/log")
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/cmds.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "1")
}

func TestNoSDP(t *testing.T) {
	cfg := loadOrFail(t, nonSDPConfig1)
	checkValue(t, "LogPath", cfg.LogPath, "/p4/1/logs/log")
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/cmds.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "")
	cfg = loadOrFail(t, nonSDPConfig2)
	checkValue(t, "LogPath", cfg.LogPath, "/p4/1/logs/log")
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/cmds.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "")
}

func loadOrFail(t *testing.T, cfgString string) *Config {
	cfg, err := Unmarshal([]byte(cfgString))
	if err != nil {
		t.Fatalf("Failed to read config: %v", err.Error())
	}
	return cfg
}
