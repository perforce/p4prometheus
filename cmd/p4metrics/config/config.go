package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v2"
)

// MonitorGroup defines a group of commands to monitor together
type MonitorGroup struct {
	Commands   string         `yaml:"commands"` // Regex pattern for commands
	ReCommands *regexp.Regexp `yaml:"-"`        // Compiled regex for commands - not set from YAML
	Label      string         `yaml:"label"`    // Group label name
}

// MemLimitGroup defines memory limits for a group of users
type MemLimitGroup struct {
	Description                    string         `yaml:"description"`                    // Name for this group - used for logging/debugging
	Users                          string         `yaml:"users"`                          // Go regex pattern matching user names
	ReUsers                        *regexp.Regexp `yaml:"-"`                              // Compiled regex for users - not set from YAML
	CmdMaxPercentage               string         `yaml:"cmd_max_percentage"`             // 0-99 (with optional % suffix), 0/blank means no limit
	CmdMaxPercentageInt            int            `yaml:"-"`                              // Parsed integer value
	CmdMaxValue                    string         `yaml:"cmd_max_value"`                  // e.g. 10M, 1.5G; blank/0 means no limit
	CmdMaxValueInt                 int64          `yaml:"-"`                              // Parsed bytes value
	UserCumulativeMaxPercentage    string         `yaml:"user_cumulative_max_percentage"` // 0-99 (with optional % suffix), 0/blank means no limit
	UserCumulativeMaxPercentageInt int            `yaml:"-"`                              // Parsed integer value
	UserCumulativeMaxValue         string         `yaml:"user_cumulative_max_value"`      // e.g. 10M, 1.5G; blank/0 means no limit
	UserCumulativeMaxValueInt      int64          `yaml:"-"`                              // Parsed bytes value
}

// MemLimits defines memory limit monitoring/enforcement configuration
type MemLimits struct {
	CandidateCmds   string          `yaml:"candidate_cmds"` // Go regex pattern matching command names to consider
	ReCandidateCmds *regexp.Regexp  `yaml:"-"`              // Compiled regex - not set from YAML
	Enabled         bool            `yaml:"enabled"`        // Whether to evaluate and report memory limits
	EnforceKills    bool            `yaml:"enforce_kills"`  // Whether to actually terminate processes (requires enabled)
	Groups          []MemLimitGroup `yaml:"groups"`         // Ordered list of user groups with limits
}

// Config for p4metrics - see SampleConfig for details
type Config struct {
	MetricsRoot          string        `yaml:"metrics_root"`
	SDPInstance          string        `yaml:"sdp_instance"` // If this is set then it defines the other variables such as P4Port
	P4Port               string        `yaml:"p4port"`       // P4PORT value (if not set in env or as parameter)
	P4User               string        `yaml:"p4user"`       // ditto
	P4Config             string        `yaml:"p4config"`     // P4CONFIG file - useful if non-SDP
	P4Bin                string        `yaml:"p4bin"`        // Only useful if non SDP - path to "p4" binary if not in $PATH
	P4DBin               string        `yaml:"p4dbin"`       // Only useful if non SDP - path to "p4d" binary if not in $PATH
	UpdateInterval       time.Duration `yaml:"update_interval"`
	LongUpdateInterval   time.Duration `yaml:"long_update_interval"`
	MonitorSwarm         bool          `yaml:"monitor_swarm"`
	SwarmURL             string        `yaml:"swarm_url"`           // Swarm URL - if the value returned by p4 property -l does not work (VPN etc)
	SwarmSecure          bool          `yaml:"swarm_secure"`        // Whether to validate the Swarm HTTPS certificate
	CmdsByUser           bool          `yaml:"cmds_by_user"`        // Whether to output metric p4_monitor_by_user
	MemoryByUser         bool          `yaml:"memory_by_user"`      // Whether to output metric p4_active_memory_by_user
	MaxJournalSize       string        `yaml:"max_journal_size"`    // Maximum size of journal file to monitor, e.g. 100M, 0 means no limit
	MaxJournalPercent    string        `yaml:"max_journal_percent"` // Maximum size of journal as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
	MaxLogSize           string        `yaml:"max_log_size"`        // Maximum size of journal file to monitor, e.g. 100M, 0 means no limit
	MaxLogPercent        string        `yaml:"max_log_percent"`     // Maximum size of log as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
	MaxJournalSizeInt    int64
	MaxJournalPercentInt int
	MaxLogSizeInt        int64
	MaxLogPercentInt     int
	MonitorIgnore        string         `yaml:"monitor_ignore"` // Monitor commmands to ignore - e.g. long running background tasks - values are a Go regex pattern - e.g. "admin resource-monitor|ldapsync"
	MonitorIgnoreRe      *regexp.Regexp `yaml:"-"`              // Compiled regex for monitor_ignore - not set from YAML
	MonitorGroups        []MonitorGroup `yaml:"monitor_groups"` // Array of command groups - each with a regex pattern to match commands and a label value to use for those commands (see SampleConfig for details)
	MemLimits            *MemLimits     `yaml:"memlimits"`      // Optional memory limit monitoring/enforcement configuration
}

// SampleConfig shows a sample config file - this can be used as a template
// for creating your own config file and is also output if you run p4metrics
// with the --sample.config flag.
const SampleConfig = `
# Sample p4metrics configuration file - normally called p4metrics.yaml
# Generated by: p4metrics --sample.config
# Edit as required - see comments below
# Blank lines and lines starting with # are comments and ignored

# ----------------------
# metrics_root: REQUIRED! Directory into which to write metrics files for processing by node_exporter.
# Ensure that node_exporter user has read access to this folder (and any parent directories)!
metrics_root: /hxlogs/metrics

# ----------------------
# sdp_instance: SDP instance - typically integer, but can be alphanumeric
# See: https://swarm.workshop.perforce.com/projects/perforce-software-sdp for more
# If this value is blank then it is assumed to be a non-SDP instance, and you will want
# to set other values with a prefix of p4 below.
sdp_instance:

# ----------------------
# p4port: The value of P4PORT to use
# IGNORED if sdp_instance is non-blank!
p4port:

# ----------------------
# p4user: The value of P4USER to use
# IGNORED if sdp_instance is non-blank!
p4user:

# ----------------------
# p4config: The value of a P4CONFIG to use
# This is very useful and should be set to an absolute path if you need values like P4TRUST/P4TICKETS etc
# IGNORED if sdp_instance is non-blank!
p4config:      

# ----------------------
# p4bin: The absolute path to the p4 binary to be used - important if not available in your PATH
# E.g. /some/path/to/p4
# IGNORED if sdp_instance is non-blank! (Will use /p4/<instance>/bin/p4_<instance>)
p4bin:      p4

# ----------------------
# p4dbin: The absolute path to the p4d binary to be used - important if not available in your PATH
# E.g. /some/path/to/p4d
# IGNORED if sdp_instance is non-blank! (Will use /p4/<instance>/bin/p4d_<instance>)
p4dbin:     p4d

# ----------------------
# update_interval: how frequently metrics should be written - defaults to 1m
# Values are as parsed by Go, e.g. 1m or 30s etc.
update_interval:    1m

# ----------------------
# cmds_by_user: true/false - Whether to output metric p4_monitor_by_user
# Normally this should be set to true as the metric is useful.
# If you have a p4d instance with hundreds/thousands of users you may find the number
# of metrics labels is too great (one per distinct user), so set this to false.
# Or set it to false if any personal information concerns. See also memory_by_user below for similar considerations.
cmds_by_user:   true

# ----------------------
# monitor_swarm: true/false - Whether to monitor status and version of swarm
# Normally this should be set to true - won't run if there is no Swarm property
monitor_swarm:   true

# ----------------------
# swarm_url: URL of the Swarm instance to monitor
# Normally this is blank, and p4metrics reads the p4 property value
# Sometimes (e.g. due to VPN setup) that value is not correct - so set this instead
# swarm_url: https://swarm.example.com
swarm_url:

# ----------------------
# swarm_secure: true/false - Whether to validate SSL for swarm
# Defaults to true, but if you have a self-signed certificate or similar set to false
swarm_secure: true

# ----------------------
# max_journal_size: Maximum size of journal file to monitor, e.g. 10G, 0 means no limit
# Units are K/M/G/T/P (powers of 1024), e.g. 10M, 1.5G etc
# If the journal file is larger than this value it will be rotated using: p4 admin journal
# This is useful to avoid sudden large journal growth causing disk space issues (often a sign of automation problems).
# Note that this is only actioned if the p4d server is a "standard" or "commit-server" (so no replicas or edge servers).
# The system will only rotate the journal if the user is a super user and the journalPrefix volume has sufficient free space.
# Leave blank or set to 0 to disable (see max_journal_percent below for alternative).
max_journal_size:

# ----------------------
# max_journal_percent: Maximum size of journal as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
# Values are integers 0-99
# Volume information is read using: p4 diskspace
# If the journal file is larger than this percentag value it will be rotated using: p4 admin journal
# This is useful to avoid sudden large journal growth causing disk space issues (often a sign of automation problems).
# Note that this is only actioned if the p4d server is a "standard" or "commit-server" (so no replicas or edge servers).
# The system will only rotate the journal if the journalPrefix volume has sufficient free space.
# Leave blank or set to 0 to disable (see max_journal_size above for alternative).
max_journal_percent:     30

# ----------------------
# max_log_size: Maximum size of P4LOG file to monitor - similar to max_journal_size above
# Units are K/M/G/T/P (powers of 1024), e.g. 10M, 1.5G etc
# If the log file is larger than this value it will be rotated and compressed (using rename + gzip)
max_log_size:

# ----------------------
# max_log_percent: Maximum size of log as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
# Values are integers 0-99
# Volume information is read using: p4 diskspace
# If the log file is larger than this percentage value it will be rotated and compressed (using rename + gzip)
max_log_percent:        30

# ----------------------
# monitor_ignore: Monitor commmands to ignore - e.g. long running background tasks
# Values are a Go regex pattern - e.g. "admin resource-monitor|ldapsync"
monitor_ignore: "admin resource-monitor|ldapsync"

# ----------------------
# monitor_groups: Optional (but recommended) grouping of commands for monitor entries (useful for spotting slow commands).
# Each entry has:
#   commands: a Go regex pattern matching command names
#   label: a name for this group of commands - used as a label value in the p4_monitor_commands metric, so should be a valid label value (see reLabelName in config.go for details)
# These values are ignored if monitor_ignore matches (first match wins), 
# and then the command is checked against the patterns in order, with the first match winning (so more specific patterns should come first).
# Note that only Running commands (state 'R') are counted for these groups, not Background ('B') or Idle ('I'), 
# as typically you want to monitor the runtime of active commands (and some IDLE commands can be long running and skew the metrics).
# Example:
# monitor_groups:
# - commands: "^rmt.*"
#   label:    rmt
# - commands: "sync|transmit"
#   label: sync_transmit
# - commands: ".*"
#   label:    other
monitor_groups:
  - commands: "^rmt.*"
    label:    rmt
  - commands: "sync|transmit"
    label:    sync_transmit
  - commands: ".*"
    label:    other

# ----------------------
# memory_by_user: true/false - Whether to output metric p4_active_memory_by_user
# Normally this should be set to true as the metric is useful.
# If you have a p4d instance with hundreds/thousands of users you may find the number
# of metrics labels is too great (one per distinct user), so set this to false.
# Or set it to false if any personal information concerns
memory_by_user:   true

# ----------------------
# memlimits: Optional (but recommended) way to define which users and commands to monitor for memory limits 
#   (useful for inadvertently high memory usage). Some users run commands on inappropriate paths such as the entire repository,
#   or a huge depot. Commands which exceed these settings have 'p4 monitor terminate' run on them, which will ask the command to terminate.
#   This is related to the MaxMemory setting for p4 groups but has some more flexibility for cumulative limits across multiple commands for a user.
# candidate_cmds: A Go regex pattern matching command names to be considered for memory monitoring - e.g. "sync|transmit|print|fstat|files|changes"
#   We default to reporting commands only.
# enabled: true/false - whether to enable this memory monitoring functionality (if false will report the metrics but not take any action, 
#   so you can monitor the metrics and adjust settings before enabling the termination functionality).
# enforce_kills: true/false - whether to actually enforce kills when limits are exceeded (if false, will only report)
# Groups:
#   Each entry has:
#     description: Name for this group of settings - used for logging and debugging, so should be unique and descriptive
#     users:       Go regex pattern matching user names
#     cmd_max_percentage:             0-99, where 0 means no limit
#     cmd_max_value:                  Units are M/G (powers of 1024), e.g. 10M, 1.5G etc, if blank or 0 then no limit
#     user_cumulative_max_percentage: For all commands for a user, 0-99, where 0 means no limit
#     user_cumulative_max_value:      Units are M/G (powers of 1024), e.g. 10M, 1.5G etc, if blank or 0 then no limit
# THE ORDER OF THE GROUPS IS IMPORTANT - the first match wins, so more specific patterns should come first (e.g. admin users should be first, 
# with no limits, and then (optionally) a group for build users with higher limits, followed by a catch-all for other users with limits).
# Note that only Running commands (state 'R') and Idle ('I') are counted for these groups, not Background ('B'), 
# since Background commands are things like replication and resource monitoring
# Example:
memlimits:
  candidate_cmds:  "annotate|changes|changelists|describe|diff|diff2|filelog|files|fstat|grep|integrated|interchanges|istat|opened|print|sync|transmit|IDLE"
  enabled:         true
  enforce_kills:   false
  groups:
  - description: "No limits for service or super users (as they hopefully know what they are doing!)"
    users: "super|perforce|p4admin|svc_.*"
    cmd_max_percentage:             
    cmd_max_value:                  
    user_cumulative_max_percentage: 
    user_cumulative_max_value:      
  - description: "Default limits for all other users"
    users: ".*"
    cmd_max_percentage:             50%
    cmd_max_value:                  
    user_cumulative_max_percentage: 70%
    user_cumulative_max_value:      

`

// parsePercentage parses a percentage string (e.g. "30" or "30%") into an integer 0-99.
// Returns 0 for blank or "0" inputs (meaning no limit).
func parsePercentage(val string) (int, error) {
	if val == "" || val == "0" {
		return 0, nil
	}
	s := strings.TrimSuffix(strings.TrimSpace(val), "%")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid percentage value %q: must be an integer 0-99", val)
	}
	if n < 0 || n > 99 {
		return 0, fmt.Errorf("invalid percentage value %q: must be in range 0-99", val)
	}
	return n, nil
}

func ConvertToBytes(size string) (int64, error) {
	if len(size) == 0 {
		return 0, nil
	}
	// Find the numeric part and unit
	numStr := size
	var unit string
	for i, char := range size {
		if !strings.ContainsRune("0123456789.", char) {
			numStr = size[:i]
			unit = strings.ToUpper(size[i:])
			break
		}
	}
	// Parse the numeric part
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number format: %v", err)
	}
	// Convert based on unit
	var multiplier uint64
	switch unit {
	case "B", "":
		multiplier = 1
	case "K":
		multiplier = 1024
	case "M":
		multiplier = 1024 * 1024
	case "G":
		multiplier = 1024 * 1024 * 1024
	case "T":
		multiplier = 1024 * 1024 * 1024 * 1024
	case "P":
		multiplier = 1024 * 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unsupported unit: %s", unit)
	}
	return int64(num * float64(multiplier)), nil
}

// Unmarshal the config
func Unmarshal(config []byte) (*Config, error) {
	// Default values specified here
	cfg := &Config{
		UpdateInterval: 60 * time.Second,
		MonitorSwarm:   false,
		SwarmSecure:    true}
	err := yaml.Unmarshal(config, cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration: %v. make sure to use 'single quotes' around strings with special characters (like match patterns or label templates), and make sure to use '-' only for lists (metrics) but not for maps (labels)", err.Error())
	}
	err = cfg.validate()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadConfigFile - loads p4prometheus config file
func LoadConfigFile(filename string) (*Config, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load %v: %v", filename, err.Error())
	}
	cfg, err := LoadConfigString(content)
	if err != nil {
		return nil, fmt.Errorf("failed to load %v: %v", filename, err.Error())
	}
	// Set defaults
	if cfg.P4Bin == "" {
		cfg.P4Bin = "p4"
	}
	if cfg.P4DBin == "" {
		cfg.P4DBin = "p4d"
	}
	return cfg, nil
}

// LoadConfigString - loads a string
func LoadConfigString(content []byte) (*Config, error) {
	cfg, err := Unmarshal([]byte(content))
	return cfg, err
}

// reLabelName - any chars in label values not matching this will be converted to underscores.
// We exclude chars such as: <space>;!="^'
// Allowed values must be valid for node_exporter and also the graphite text protocol for labels/tags
// https://graphite.readthedocs.io/en/latest/tags.html
// In addition any backslashes must be double quoted for node_exporter.
var reLabelName = regexp.MustCompile(`[\t =/+:;!@{}&%<>*\\.,\(\)\[\]-]`)

func (c *Config) validate() error {
	if c.MetricsRoot == "" {
		return fmt.Errorf("invalid metrics_root: please specify directory to which p4metrics *.prom files should be written, e.g. /hxlogs/metrics")
	}
	var err error
	if c.MaxJournalSize != "" && c.MaxJournalSize != "0" {
		if c.MaxJournalSizeInt, err = ConvertToBytes(c.MaxJournalSize); err != nil {
			return fmt.Errorf("invalid max_journal_size: %q please specify valid size, e.g. 10.5M (options: K/M/G/T/P), 0 means no limit: %v", c.MaxJournalSize, err)
		}
	}
	if c.MaxLogSize != "" && c.MaxLogSize != "0" {
		if c.MaxLogSizeInt, err = ConvertToBytes(c.MaxLogSize); err != nil {
			return fmt.Errorf("invalid max_log_size: %q please specify valid size, e.g. 10.5M (options: K/M/G/T/P), 0 means no limit: %v", c.MaxLogSize, err)
		}
	}
	if c.MaxJournalPercent != "" && c.MaxJournalPercent != "0" {
		var val int64
		if val, err = ConvertToBytes(c.MaxJournalPercent); err != nil {
			return fmt.Errorf("invalid max_journal_percent: %q please specify valid percent as integer 0-99, 0 means no limit: %v", c.MaxJournalPercent, err)
		}
		if val < 0 || val > 99 {
			return fmt.Errorf("invalid max_journal_percent: %q please specify valid percent in range 0-99", c.MaxJournalPercent)
		}
		c.MaxJournalPercentInt = int(val)
	}
	if c.MaxLogPercent != "" && c.MaxLogPercent != "0" {
		var val int64
		if val, err = ConvertToBytes(c.MaxLogPercent); err != nil {
			return fmt.Errorf("invalid max_log_percent: %q please specify valid percent as integer 0-99, 0 means no limit: %v", c.MaxLogPercent, err)
		}
		if val < 0 || val > 99 {
			return fmt.Errorf("invalid max_log_percent: %q please specify valid percent in range 0-99", c.MaxLogPercent)
		}
		c.MaxLogPercentInt = int(val)
	}
	// Validate and compile monitor_ignore regex
	if c.MonitorIgnore != "" {
		re, err := regexp.Compile(c.MonitorIgnore)
		if err != nil {
			return fmt.Errorf("failed to parse monitor_ignore '%s' as a regex: %v", c.MonitorIgnore, err)
		}
		c.MonitorIgnoreRe = re
	}
	// Validate monitor_groups regex patterns and label values
	for i, mg := range c.MonitorGroups {
		if mg.Commands == "" {
			return fmt.Errorf("monitor_groups[%d]: commands cannot be empty", i)
		}
		if mg.Label == "" {
			return fmt.Errorf("monitor_groups[%d]: label cannot be empty", i)
		}
		if reLabelName.MatchString(mg.Label) {
			return fmt.Errorf("monitor_groups[%d]: label '%s' contains invalid characters for a label value", i, mg.Label)
		}
		re, err := regexp.Compile(mg.Commands)
		if err != nil {
			return fmt.Errorf("monitor_groups[%d]: failed to parse '%s' as a regex: %v", i, mg.Commands, err)
		}
		c.MonitorGroups[i].ReCommands = re
	}
	// Validate memlimits
	if c.MemLimits != nil {
		ml := c.MemLimits
		if ml.CandidateCmds != "" {
			re, err := regexp.Compile(ml.CandidateCmds)
			if err != nil {
				return fmt.Errorf("memlimits.candidate_cmds: failed to parse '%s' as a regex: %v", ml.CandidateCmds, err)
			}
			ml.ReCandidateCmds = re
		}
		for i, g := range ml.Groups {
			if g.Users == "" {
				return fmt.Errorf("memlimits.groups[%d]: users cannot be empty", i)
			}
			re, err := regexp.Compile(g.Users)
			if err != nil {
				return fmt.Errorf("memlimits.groups[%d]: failed to parse users '%s' as a regex: %v", i, g.Users, err)
			}
			ml.Groups[i].ReUsers = re
			if ml.Groups[i].CmdMaxPercentageInt, err = parsePercentage(g.CmdMaxPercentage); err != nil {
				return fmt.Errorf("memlimits.groups[%d]: invalid cmd_max_percentage: %v", i, err)
			}
			if g.CmdMaxValue != "" && g.CmdMaxValue != "0" {
				if ml.Groups[i].CmdMaxValueInt, err = ConvertToBytes(g.CmdMaxValue); err != nil {
					return fmt.Errorf("memlimits.groups[%d]: invalid cmd_max_value %q: %v", i, g.CmdMaxValue, err)
				}
			}
			if ml.Groups[i].UserCumulativeMaxPercentageInt, err = parsePercentage(g.UserCumulativeMaxPercentage); err != nil {
				return fmt.Errorf("memlimits.groups[%d]: invalid user_cumulative_max_percentage: %v", i, err)
			}
			if g.UserCumulativeMaxValue != "" && g.UserCumulativeMaxValue != "0" {
				if ml.Groups[i].UserCumulativeMaxValueInt, err = ConvertToBytes(g.UserCumulativeMaxValue); err != nil {
					return fmt.Errorf("memlimits.groups[%d]: invalid user_cumulative_max_value %q: %v", i, g.UserCumulativeMaxValue, err)
				}
			}
		}
	}
	return nil
}
