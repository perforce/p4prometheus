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
	if !cfg.ParseJournal {
		t.Fatalf("Error parsing ParseJournal, expected true got %v", cfg.ParseJournal)
	}
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

const configParseJournalFalse = `
metrics_root:				/hxlogs/metrics
sdp_instance:				1
parse_journal:				false
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

func TestParseJournalConfig(t *testing.T) {
	cfg := loadOrFail(t, configParseJournalFalse)
	if cfg.ParseJournal {
		t.Fatalf("Error parsing ParseJournal, expected false got %v", cfg.ParseJournal)
	}
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

const configWithMemLimits = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  candidate_cmds: "sync|transmit|print"
  enabled:        true
  groups:
  - description: "No limits for admin"
    users: "super|perforce"
    cmd_max_percentage:
    cmd_max_value:
    user_cumulative_max_percentage:
    user_cumulative_max_value:
  - description: "Default limits"
    users: ".*"
    cmd_max_percentage:             30%
    cmd_max_value:                  2G
    user_cumulative_max_percentage: 50%
    user_cumulative_max_value:      4G
`

const configWithMemLimitsNoPercent = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  candidate_cmds: "sync|fstat"
  enabled:        false
  groups:
  - description: "Build users"
    users: "build.*"
    cmd_max_percentage:             40
    cmd_max_value:                  512M
    user_cumulative_max_percentage: 60
    user_cumulative_max_value:      1G
`

func TestValidMemLimits(t *testing.T) {
	cfg := loadOrFail(t, configWithMemLimits)
	if cfg.MemLimits == nil {
		t.Fatal("Expected MemLimits to be set")
	}
	ml := cfg.MemLimits
	checkValue(t, "MemLimits.CandidateCmds", ml.CandidateCmds, "sync|transmit|print")
	if !ml.Enabled {
		t.Fatal("Expected MemLimits.Enabled to be true")
	}
	if len(ml.Groups) != 2 {
		t.Fatalf("Expected 2 memlimits groups, got %d", len(ml.Groups))
	}
	g0 := ml.Groups[0]
	checkValue(t, "Groups[0].Users", g0.Users, "super|perforce")
	if g0.ReUsers == nil {
		t.Fatal("Expected Groups[0].ReUsers to be compiled")
	}
	if g0.CmdMaxPercentageInt != 0 {
		t.Fatalf("Expected Groups[0].CmdMaxPercentageInt=0, got %d", g0.CmdMaxPercentageInt)
	}
	if g0.CmdMaxValueInt != 0 {
		t.Fatalf("Expected Groups[0].CmdMaxValueInt=0, got %d", g0.CmdMaxValueInt)
	}
	g1 := ml.Groups[1]
	checkValue(t, "Groups[1].Users", g1.Users, ".*")
	if g1.CmdMaxPercentageInt != 30 {
		t.Fatalf("Expected Groups[1].CmdMaxPercentageInt=30, got %d", g1.CmdMaxPercentageInt)
	}
	checkValueInt(t, "Groups[1].CmdMaxValueInt", g1.CmdMaxValueInt, 2*1024*1024*1024)
	if g1.UserCumulativeMaxPercentageInt != 50 {
		t.Fatalf("Expected Groups[1].UserCumulativeMaxPercentageInt=50, got %d", g1.UserCumulativeMaxPercentageInt)
	}
	checkValueInt(t, "Groups[1].UserCumulativeMaxValueInt", g1.UserCumulativeMaxValueInt, 4*1024*1024*1024)
}

func TestValidMemLimitsNoPercent(t *testing.T) {
	cfg := loadOrFail(t, configWithMemLimitsNoPercent)
	if cfg.MemLimits == nil {
		t.Fatal("Expected MemLimits to be set")
	}
	ml := cfg.MemLimits
	if ml.Enabled {
		t.Fatal("Expected MemLimits.Enabled to be false")
	}
	if len(ml.Groups) != 1 {
		t.Fatalf("Expected 1 memlimits group, got %d", len(ml.Groups))
	}
	g := ml.Groups[0]
	if g.CmdMaxPercentageInt != 40 {
		t.Fatalf("Expected CmdMaxPercentageInt=40, got %d", g.CmdMaxPercentageInt)
	}
	checkValueInt(t, "CmdMaxValueInt", g.CmdMaxValueInt, 512*1024*1024)
	if g.UserCumulativeMaxPercentageInt != 60 {
		t.Fatalf("Expected UserCumulativeMaxPercentageInt=60, got %d", g.UserCumulativeMaxPercentageInt)
	}
	checkValueInt(t, "UserCumulativeMaxValueInt", g.UserCumulativeMaxValueInt, 1024*1024*1024)
}

func TestNoMemLimits(t *testing.T) {
	cfg := loadOrFail(t, defaultConfig)
	if cfg.MemLimits != nil {
		t.Fatal("Expected MemLimits to be nil when not configured")
	}
}

const configMemLimitsInvalidCandidateCmds = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  candidate_cmds: "sync|[invalid"
  enabled: false
  groups:
  - description: "test"
    users: ".*"
`

const configMemLimitsEmptyUsers = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  candidate_cmds: "sync"
  enabled: false
  groups:
  - description: "test"
    users: ""
`

const configMemLimitsInvalidUsersRegex = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  candidate_cmds: "sync"
  enabled: false
  groups:
  - description: "test"
    users: "[invalid"
`

const configMemLimitsInvalidCmdMaxPercent = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  enabled: false
  groups:
  - description: "test"
    users: ".*"
    cmd_max_percentage: 150%
`

const configMemLimitsInvalidCmdMaxValue = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  enabled: false
  groups:
  - description: "test"
    users: ".*"
    cmd_max_value: 10Z
`

const configMemLimitsInvalidCumulativePercent = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  enabled: false
  groups:
  - description: "test"
    users: ".*"
    user_cumulative_max_percentage: notanumber
`

const configMemLimitsInvalidCumulativeValue = `
metrics_root:   /hxlogs/metrics
sdp_instance:   1
memlimits:
  enabled: false
  groups:
  - description: "test"
    users: ".*"
    user_cumulative_max_value: 5Q
`

func TestInvalidMemLimits(t *testing.T) {
	ensureFail(t, configMemLimitsInvalidCandidateCmds, "invalid regex in candidate_cmds")
	ensureFail(t, configMemLimitsEmptyUsers, "empty users field")
	ensureFail(t, configMemLimitsInvalidUsersRegex, "invalid regex in users")
	ensureFail(t, configMemLimitsInvalidCmdMaxPercent, "cmd_max_percentage out of range")
	ensureFail(t, configMemLimitsInvalidCmdMaxValue, "invalid cmd_max_value unit")
	ensureFail(t, configMemLimitsInvalidCumulativePercent, "invalid user_cumulative_max_percentage")
	ensureFail(t, configMemLimitsInvalidCumulativeValue, "invalid user_cumulative_max_value unit")
}
