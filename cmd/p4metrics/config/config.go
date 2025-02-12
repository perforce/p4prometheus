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
	ServerID           string        `yaml:"server_id"`
	ServerIDPath       string        `yaml:"server_id_path"`
	SDPInstance        string        `yaml:"sdp_instance"`
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
		return fmt.Errorf("invalid metrics_output: please specify name of p4metrics metric file to write, e.g. /hxlogs/metrics/p4_metrics.prom")
	}
	// if !strings.HasSuffix(c.MetricsRoot, ".prom") {
	// 	return fmt.Errorf("invalid metrics_output: P4metrics metric file must end in '.prom'")
	// }
	return nil
}
