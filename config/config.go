package config

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"runtime"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v2"
)

// Config for p4prometheus - see SampleConfig for details
type Config struct {
	LogPath               string        `yaml:"log_path"`
	MetricsOutput         string        `yaml:"metrics_output"`
	ServerID              string        `yaml:"server_id"`
	ServerIDPath          string        `yaml:"server_id_path"`
	SDPInstance           string        `yaml:"sdp_instance"`
	UpdateInterval        time.Duration `yaml:"update_interval"`
	OutputCmdsByUser      bool          `yaml:"output_cmds_by_user"`
	OutputCmdsByUserRegex string        `yaml:"output_cmds_by_user_regex"`
	OutputCmdsByIP        bool          `yaml:"output_cmds_by_ip"`
	CaseSensitiveServer   bool          `yaml:"case_senstive_server"`
}

// SampleConfig shows a sample config file - this can be used as a template
// for creating your own config file and is also output if you run p4prometheus
// with the --sample.config flag.
const SampleConfig = `
# ----------------------
# log_path: Path to p4d server log - REQUIRED!
# On Windows this might be something like: D:/Perforce/logs/p4d.log (note forward slashes preferred))
log_path:       /p4/1/logs/log

# ----------------------
# metrics_output: Name of output file to write for processing by node_exporter - REQUIRED!
# Ensure that node_exporter user has read access to this folder.
metrics_output: /hxlogs/metrics/cmds.prom

# ----------------------
# sdp_instance: SDP instance - typically integer, but can be alphanumeric.
# See: https://swarm.workshop.perforce.com/projects/perforce-software-sdp for more
# If this value is blank then it is assumed to be a non-SDP instance (and other fields
# such as server_id or server_id_path must be set)
sdp_instance:   1

# ----------------------
# server_id: Optional - serverid for metrics - typically read from /p4/<sdp_instance>/root/server.id for 
# SDP installations - please specify a value if *non-SDP* install, or set server_id_path
server_id:      

# ----------------------
# server_id_path: Optional - path to server.id file for metrics - only required for non-SDP installations.
# Set either this field, or server_id instead.
server_id_path:      

# ----------------------
# output_cmds_by_user: true/false - Whether to output metrics p4_cmd_user_counter/p4_cmd_user_cumulative_seconds
# Normally this should be set to true as the metrics are useful.
# If you have a p4d instance with thousands of users you may find the number
# of metrics labels is too great (one per distinct user), so set this to false.
output_cmds_by_user: true

# ----------------------
# case_sensitive_server: true/false - if output_cmds_by_user=true then if this value is set to false
# all userids will be written in lowercase - otherwise as they occur in the log file
# If not present, this value will default to true on Windows and false otherwise.
case_sensitive_server: true

# ----------------------
# output_cmds_by_ip: true/false - Whether to output metrics p4_cmd_ip_counter/p4_cmd_ip_cumulative_seconds
# Like output_cmds_by_user this can be an issue for larger sites so defaults to false.
output_cmds_by_ip: false

# ----------------------
# output_cmds_by_user_regex: Specifies a Go regex for users for whom to output
# metrics p4_cmd_user_detail_counter (tracks cmd counts per user/per cmd) and
# p4_cmd_user_detail_cumulative_seconds
# 
# This can be set to values such as: "" (no users), ".*" (all users), or "swarm|jenkins"
# for just those 2 users. The latter is likely to be appropriate in many sites (keep an eye
# on automation users only, without generating thousands of labels for all users)
output_cmds_by_user_regex: ""

# ----------------------
# fail_on_missing_logfile: Due to timing log file might not be there - just wait.
fail_on_missing_logfile: false

`

// Unmarshal the config
func Unmarshal(config []byte) (*Config, error) {
	// Default values specified here
	caseSensitive := true
	if runtime.GOOS == "windows" {
		caseSensitive = false
	}
	cfg := &Config{
		UpdateInterval:      15 * time.Second,
		OutputCmdsByUser:    true,
		CaseSensitiveServer: caseSensitive}
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
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("Failed to load %v: %v", filename, err.Error())
	}
	cfg, err := LoadConfigString(content)
	if err != nil {
		return nil, fmt.Errorf("Failed to load %v: %v", filename, err.Error())
	}
	return cfg, nil
}

// LoadConfigString - loads a string
func LoadConfigString(content []byte) (*Config, error) {
	cfg, err := Unmarshal([]byte(content))
	return cfg, err
}

func (c *Config) validate() error {
	if c.LogPath == "" {
		return fmt.Errorf("Invalid log_path: please specify name of p4d server log")
	}
	if c.MetricsOutput == "" {
		return fmt.Errorf("Invalid metrics_output: please specify name of Prometheus metric file to write, e.g. /hxlogs/metrics/p4_cmds.prom")
	}
	if !strings.HasSuffix(c.MetricsOutput, ".prom") {
		return fmt.Errorf("Invalid metrics_output: Prometheus metric file must end in '.prom'")
	}
	// Validate regex
	if c.OutputCmdsByUserRegex != "" {
		if _, err := regexp.Compile(c.OutputCmdsByUserRegex); err != nil {
			return fmt.Errorf("Failed to parse '%s' as a regex", c.OutputCmdsByUserRegex)
		}
	}
	return nil
}
