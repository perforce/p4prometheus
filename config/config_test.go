package config

import (
	"runtime"
	"testing"
	"time"
)

const defaultConfig = `
log_path:					/p4/1/logs/log
metrics_output:				/hxlogs/metrics/cmds.prom
server_id:					myserverid
sdp_instance: 				1
update_interval: 			15s
output_cmds_by_user: 		true
output_cmds_by_user_regex: 	".*"
output_cmds_by_ip: 			true
case_sensitive_server: 		true
`

const nonSDPConfig1 = `
log_path:			/p4/1/logs/log
metrics_output:		/hxlogs/metrics/cmds.prom
server_id:			myserverid
update_interval: 	1m
output_cmds_by_user: false
case_sensitive_server: true
`

const nonSDPConfig2 = `
log_path:			/p4/1/logs/log
metrics_output:		/hxlogs/metrics/cmds.prom
server_id:			myserverid
sdp_instance:
update_interval: 	20s
output_cmds_by_user: true
case_sensitive_server: false
`

const nonSDPConfig3 = `
log_path:			/p4/1/logs/log
metrics_output:		/hxlogs/metrics/cmds.prom
server_id:			myserverid
server_id_path:		/p4/root/server.id
sdp_instance:
update_interval: 	20s
output_cmds_by_user: true
case_sensitive_server: false
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
	checkValue(t, "LogPath", cfg.LogPath, "/p4/1/logs/log")
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/cmds.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "1")
	checkValueDuration(t, "UpdateInterval", cfg.UpdateInterval, 15*time.Second)
	checkValueBool(t, "OutputCmdsByUser", cfg.OutputCmdsByUser, true)
	checkValue(t, "OutputCmdsByUserRegex", cfg.OutputCmdsByUserRegex, ".*")
	checkValueBool(t, "OutputCmdsByIp", cfg.OutputCmdsByIP, true)
}

func TestNoSDP(t *testing.T) {
	cfg := loadOrFail(t, nonSDPConfig1)
	checkValue(t, "LogPath", cfg.LogPath, "/p4/1/logs/log")
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/cmds.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "ServerIdPath", cfg.ServerIDPath, "")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "")
	checkValueDuration(t, "UpdateInterval", cfg.UpdateInterval, 1*time.Minute)
	checkValueBool(t, "OutputCmdsByUser", cfg.OutputCmdsByUser, false)
	cfg = loadOrFail(t, nonSDPConfig2)
	checkValue(t, "LogPath", cfg.LogPath, "/p4/1/logs/log")
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/cmds.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "ServerIdPath", cfg.ServerIDPath, "")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "")
	checkValueDuration(t, "UpdateInterval", cfg.UpdateInterval, 20*time.Second)
	checkValueBool(t, "OutputCmdsByUser", cfg.OutputCmdsByUser, true)
	cfg = loadOrFail(t, nonSDPConfig3)
	checkValue(t, "LogPath", cfg.LogPath, "/p4/1/logs/log")
	checkValue(t, "MetricsOutput", cfg.MetricsOutput, "/hxlogs/metrics/cmds.prom")
	checkValue(t, "ServerId", cfg.ServerID, "myserverid")
	checkValue(t, "ServerIdPath", cfg.ServerIDPath, "/p4/root/server.id")
	checkValue(t, "SDPInstance", cfg.SDPInstance, "")
	checkValueDuration(t, "UpdateInterval", cfg.UpdateInterval, 20*time.Second)
	checkValueBool(t, "OutputCmdsByUser", cfg.OutputCmdsByUser, true)
}

func TestWrongValues(t *testing.T) {
	start := `log_path:			/p4/1/logs/log
metrics_output:				/hxlogs/metrics/cmds.prom
`
	ensureFail(t, start+`update_interval: 	'not duration'`, "duration")
}

func TestDefaultInterval(t *testing.T) {
	cfg := loadOrFail(t, `
log_path:			/p4/1/logs/log
metrics_output:		/hxlogs/metrics/cmds.prom
server_id:			myserverid
sdp_instance: 		1
`)
	if cfg.UpdateInterval != 15*time.Second {
		t.Errorf("Failed default interval: %v", cfg.UpdateInterval)
	}
	if !cfg.OutputCmdsByUser {
		t.Errorf("Failed default output_cmds_by_user")
	}
	if runtime.GOOS == "windows" {
		if cfg.CaseSensitiveServer {
			t.Errorf("Failed default case_sensitive_server on Windows")
		}
	} else {
		if !cfg.CaseSensitiveServer {
			t.Errorf("Failed default case_sensitive_server on Linux/Mac")
		}
	}
}

func TestRegex(t *testing.T) {
	// Invalid regex should cause error
	cfgString := `
log_path:					/p4/1/logs/log
metrics_output:				/hxlogs/metrics/cmds.prom
server_id:					myserverid
output_cmds_by_user_regex: 	"[.*"
`
	_, err := Unmarshal([]byte(cfgString))
	if err == nil {
		t.Fatalf("Expected regex error not seen")
	}
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
