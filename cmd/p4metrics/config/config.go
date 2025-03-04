package config

import (
	"fmt"
	"os"
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
	CmdsByUser         bool          `yaml:"cmds_by_user"`
}

// Unmarshal the config
func Unmarshal(config []byte) (*Config, error) {
	// Default values specified here
	cfg := &Config{
		UpdateInterval: 60 * time.Second,
		MonitorSwarm:   false}
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
	return nil
}
