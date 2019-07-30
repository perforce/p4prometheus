package config

import (
	"fmt"
	"io/ioutil"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

// Config for p4prometheus
type Config struct {
	LogPath       string `yaml:"log_path"`
	MetricsOutput string `yaml:"metrics_output"`
	ServerID      string `yaml:"server_id"`
	SDPInstance   string `yaml:"sdp_instance"`
}

// Unmarshal the config
func Unmarshal(config []byte) (*Config, error) {
	cfg := &Config{}
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
	return nil
}
