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

func TestValidConfig(t *testing.T) {
	cfg := loadOrFail(t, defaultConfig)
	if cfg.LogPath != "/p4/1/logs/log" {
		t.Fatalf("Error parsing LogPath, got %v", cfg.LogPath)
	}
	if cfg.MetricsOutput != "/hxlogs/metrics/cmds.prom" {
		t.Fatalf("Error parsing MetricsOutput, got %v", cfg.MetricsOutput)
	}
	if cfg.ServerID != "myserverid" {
		t.Fatalf("Error parsing ServerID, got %v", cfg.ServerID)
	}
	if cfg.SDPInstance != "1" {
		t.Fatalf("Error parsing LogPath, got %v", cfg.SDPInstance)
	}
}

func loadOrFail(t *testing.T, cfgString string) *Config {
	cfg, err := Unmarshal([]byte(cfgString))
	if err != nil {
		t.Fatalf("Failed to read config: %v", err.Error())
	}
	return cfg
}

// func equalsIgnoreIndentation(a string, b string) bool {
// 	aLines := stripEmptyLines(strings.Split(a, "\n"))
// 	bLines := stripEmptyLines(strings.Split(b, "\n"))
// 	if len(aLines) != len(bLines) {
// 		return false
// 	}
// 	for i := range aLines {
// 		if strings.TrimSpace(aLines[i]) != strings.TrimSpace(bLines[i]) {
// 			return false
// 		}
// 	}
// 	return true
// }

// func stripEmptyLines(lines []string) []string {
// 	result := make([]string, 0, len(lines))
// 	for _, line := range lines {
// 		if line != "" {
// 			result = append(result, line)
// 		}
// 	}
// 	return result
// }
