package config

import (
	"strings"
	"testing"
	"time"
)

const defaultConfig = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
update_interval:                        60s
monitor_swarm:                          false
`

const config2 = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
update_interval:                        60s
monitor_swarm:                          false
max_journal_size:                       100M
max_journal_percent:            40
max_log_size:                           10.3G
max_log_percent:                        30
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

func TestCmdsByProgConfig(t *testing.T) {
	for _, test := range []struct {
		settings string
		want     bool
	}{
		{"", true},
		{"\ncmds_by_prog: true\n", true},
		{"\ncmds_by_prog: false\n", false},
	} {
		cfg := loadOrFail(t, defaultConfig+test.settings)
		if cfg.CmdsByProg != test.want {
			t.Errorf("cmds_by_prog %q: got %v, want %v", test.settings, cfg.CmdsByProg, test.want)
		}
	}
}

func TestP4AuthConfig(t *testing.T) {
	for _, test := range []struct {
		name, settings  string
		monitor, secure bool
	}{
		{"defaults", "", true, true},
		{"disabled", "\nmonitor_p4auth: false\n", false, true},
		{"insecure", "\np4auth_secure: false\n", true, false},
		{"both disabled", "\nmonitor_p4auth: false\np4auth_secure: false\n", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := loadOrFail(t, defaultConfig+test.settings)
			if cfg.MonitorP4Auth != test.monitor || cfg.P4AuthSecure != test.secure {
				t.Errorf("got monitor_p4auth=%v p4auth_secure=%v", cfg.MonitorP4Auth, cfg.P4AuthSecure)
			}
		})
	}
}

func TestLicenseUpdatesConfig(t *testing.T) {
	cfg := loadOrFail(t, defaultConfig)
	checkValue(t, "LicenseUpdates.Mode", cfg.LicenseUpdates.Mode, "every_update")
	checkValueDuration(t, "LicenseUpdates.StartWindow", cfg.LicenseUpdates.StartWindow, 5*time.Minute)
	if cfg.LicenseUpdates.Location != time.Local {
		t.Fatal("expected host timezone by default")
	}

	cfg = loadOrFail(t, defaultConfig+`
license_updates:
  mode: scheduled
  times: ["01:00", "23:30"]
  timezone: Europe/London
  start_window: 2m
`)
	checkValue(t, "LicenseUpdates.Mode", cfg.LicenseUpdates.Mode, "scheduled")
	checkValue(t, "LicenseUpdates.Location", cfg.LicenseUpdates.Location.String(), "Europe/London")
	checkValueDuration(t, "LicenseUpdates.StartWindow", cfg.LicenseUpdates.StartWindow, 2*time.Minute)
	if len(cfg.LicenseUpdates.Times) != 2 {
		t.Fatalf("expected two times, got %v", cfg.LicenseUpdates.Times)
	}
	cfg = loadOrFail(t, defaultConfig+"\nlicense_updates:\n  mode: disabled\n")
	checkValue(t, "LicenseUpdates.Mode", cfg.LicenseUpdates.Mode, "disabled")
	loadOrFail(t, SampleConfig)
}

func TestInvalidLicenseUpdatesConfig(t *testing.T) {
	for _, test := range []struct {
		name, settings, message string
	}{
		{"mode", "mode: daily", "license_updates.mode"},
		{"missing times", "mode: scheduled", "license_updates.times"},
		{"empty times", "mode: scheduled\n  times: []", "license_updates.times"},
		{"invalid hour", `times: ["24:00"]`, "license_updates.times"},
		{"invalid minute", `times: ["01:60"]`, "license_updates.times"},
		{"short hour", `times: ["1:00"]`, "license_updates.times"},
		{"seconds", `times: ["01:00:00"]`, "license_updates.times"},
		{"duplicate", `times: ["01:00", "01:00"]`, "duplicate license_updates.times"},
		{"timezone", "timezone: Invalid/Timezone", "license_updates.timezone"},
		{"zero window", "start_window: 0s", "license_updates.start_window"},
		{"negative window", "start_window: -1m", "license_updates.start_window"},
		{"large window", "start_window: 24h", "license_updates.start_window"},
		{"invalid duration", "start_window: tomorrow", "invalid configuration"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ensureFail(t, defaultConfig+"\nlicense_updates:\n  "+test.settings+"\n", test.message)
		})
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
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
max_journal_size:                       100A
`

const configParseJournalFalse = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
parse_journal:                          false
`

const config4 = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
max_journal_percent:            101
`

const config5 = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
max_log_size:                           10.3Z
`

const config6 = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
max_log_percent:                        30$
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

const configWithSlackNotifications = `
metrics_root: /hxlogs/metrics
sdp_instance: 1
notifications:
  slack:
    enabled: true
    mode: "bot"
    bot_token: "xoxb-test-token"
    channel_id: "C1234567890"
`

func TestValidSlackNotificationsConfig(t *testing.T) {
	cfg := loadOrFail(t, configWithSlackNotifications)
	if !cfg.Notifications.Slack.Enabled {
		t.Fatalf("Expected notifications.slack.enabled true")
	}
	if cfg.Notifications.Slack.Mode != "bot" {
		t.Fatalf("Expected notifications.slack.mode=bot, got %q", cfg.Notifications.Slack.Mode)
	}
	if cfg.Notifications.Slack.BotToken != "xoxb-test-token" {
		t.Fatalf("Expected bot_token parsed correctly")
	}
	if cfg.Notifications.Slack.ChannelID != "C1234567890" {
		t.Fatalf("Expected channel_id parsed correctly")
	}
}

func TestNotificationCooldownConfig(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings string
		want     int
	}{
		{"omitted section", "", 300},
		{"existing slack settings", "\nnotifications:\n  slack:\n    enabled: false\n", 300},
		{"custom cooldown", "\nnotifications:\n  cooldown_seconds: 900\n", 900},
		{"disabled cooldown", "\nnotifications:\n  cooldown_seconds: 0\n", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := loadOrFail(t, defaultConfig+test.settings)
			if cfg.Notifications.CooldownSeconds != test.want {
				t.Fatalf("got cooldown %d, want %d", cfg.Notifications.CooldownSeconds, test.want)
			}
		})
	}
	ensureFail(t, defaultConfig+"\nnotifications:\n  cooldown_seconds: -1\n", "notifications.cooldown_seconds")
}

const configWithSlackNotificationEnvironment = `
metrics_root: /hxlogs/metrics
sdp_instance: 1
notifications:
  slack:
    enabled: true
    mode: "bot"
    bot_token_env: "P4METRICS_TEST_SLACK_TOKEN"
    channel_id: "C1234567890"
`

func TestSlackNotificationEnvironmentConfig(t *testing.T) {
	t.Setenv("P4METRICS_TEST_SLACK_TOKEN", "xoxb-environment-token")
	cfg := loadOrFail(t, configWithSlackNotificationEnvironment)
	if cfg.Notifications.Slack.BotToken != "xoxb-environment-token" {
		t.Fatalf("Expected bot_token_env to resolve bot token")
	}
}

func TestSlackNotificationMissingEnvironmentConfig(t *testing.T) {
	_, err := Unmarshal([]byte(configWithSlackNotificationEnvironment))
	if err == nil || !strings.Contains(err.Error(), "P4METRICS_TEST_SLACK_TOKEN") {
		t.Fatalf("Expected missing bot_token_env error, got %v", err)
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
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
monitor_groups:
- commands: "sync|transmit"
  label: sync_transmit
- commands: "shelve|unshelve"
  label: shelf_ops
`

const configWithInvalidGroupRegex = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
monitor_groups:
- commands: "sync|[invalid"
  label: sync_ops
`

const configWithInvalidGroupName = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
monitor_groups:
- commands: "sync|transmit"
  label: "sync transmit"
`

const configWithEmptyCommands = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
monitor_groups:
- commands: ""
  label: sync_ops
`

const configWithEmptyGroup = `
metrics_root:                           /hxlogs/metrics
sdp_instance:                           1
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
