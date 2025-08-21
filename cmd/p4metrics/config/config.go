package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v2"
)

// Config for p4metrics
type Config struct {
	MetricsRoot        string        `yaml:"metrics_root"`
	SDPInstance        string        `yaml:"sdp_instance"` // If this is set then it defines the other variables such as P4Port
	P4Port             string        `yaml:"p4port"`       // P4PORT value (if not set in env or as parameter)
	P4User             string        `yaml:"p4user"`       // ditto
	P4Config           string        `yaml:"p4config"`     // P4CONFIG file - useful if non-SDP
	P4Bin              string        `yaml:"p4bin"`        // Only useful if non SDP - path to "p4" binary if not in $PATH
	UpdateInterval     time.Duration `yaml:"update_interval"`
	LongUpdateInterval time.Duration `yaml:"long_update_interval"`
	MonitorSwarm       bool          `yaml:"monitor_swarm"`
	SwarmURL           string        `yaml:"swarm_url"`    // Swarm URL - if the value returned by p4 property -l does not work (VPN etc)
	SwarmSecure        bool          `yaml:"swarm_secure"` // Wehther to validate the Swarm HTTPS certificate
	CmdsByUser         bool          `yaml:"cmds_by_user"`
	MaxJournalSize     string        `yaml:"max_journal_size"`    // Maximum size of journal file to monitor, e.g. 100M, 0 means no limit
	MaxJournalPercent  string        `yaml:"max_journal_percent"` // Maximum size of journal as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
	MaxLogSize         string        `yaml:"max_log_size"`        // Maximum size of journal file to monitor, e.g. 100M, 0 means no limit
	MaxLogPercent      string        `yaml:"max_log_percent"`     // Maximum size of log as percentage of total P4LOGS disk space, e.g. 40, 0 means no limit
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
	return cfg, nil
}

// LoadConfigString - loads a string
func LoadConfigString(content []byte) (*Config, error) {
	cfg, err := Unmarshal([]byte(content))
	return cfg, err
}

func (c *Config) validate() error {
	if c.MetricsRoot == "" {
		return fmt.Errorf("invalid metrics_root: please specify directory to which p4metrics *.prom files should be written, e.g. /hxlogs/metrics")
	}
	if c.MaxJournalSize != "" && c.MaxJournalSize != "0" {
		if _, err := ConvertToBytes(c.MaxJournalSize); err != nil {
			return fmt.Errorf("invalid max_journal_size: %q please specify valid size, e.g. 10.5M (options: K/M/G/T/P), 0 means no limit: %v", c.MaxJournalSize, err)
		}
	}
	if c.MaxLogSize != "" && c.MaxLogSize != "0" {
		if _, err := ConvertToBytes(c.MaxLogSize); err != nil {
			return fmt.Errorf("invalid max_log_size: %q please specify valid size, e.g. 10.5M (options: K/M/G/T/P), 0 means no limit: %v", c.MaxLogSize, err)
		}
	}
	if c.MaxJournalPercent != "" && c.MaxJournalPercent != "0" {
		if _, err := ConvertToBytes(c.MaxJournalPercent); err != nil {
			return fmt.Errorf("invalid max_journal_percent: %q please specify valid percent as integer 0-99, 0 means no limit: %v", c.MaxJournalPercent, err)
		}
		if val, _ := ConvertToBytes(c.MaxJournalPercent); val < 0 || val > 99 {
			return fmt.Errorf("invalid max_journal_percent: %q please specify valid percent in range 0-99", c.MaxJournalPercent)
		}
	}
	if c.MaxLogPercent != "" && c.MaxLogPercent != "0" {
		if _, err := ConvertToBytes(c.MaxLogPercent); err != nil {
			return fmt.Errorf("invalid max_log_percent: %q please specify valid percent as integer 0-99, 0 means no limit: %v", c.MaxLogPercent, err)
		}
		if val, _ := ConvertToBytes(c.MaxLogPercent); val < 0 || val > 99 {
			return fmt.Errorf("invalid max_log_percent: %q please specify valid percent in range 0-99", c.MaxLogPercent)
		}
	}
	return nil
}
