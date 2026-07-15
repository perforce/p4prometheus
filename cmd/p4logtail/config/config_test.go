package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validConfig = `
p4_log: /p4/1/logs/log
json_log: /p4/1/logs/p4logtail.json
`

func TestValidConfigLoaders(t *testing.T) {
	tests := []struct {
		name   string
		loader func(t *testing.T) (*Config, error)
	}{
		{
			name: "Unmarshal",
			loader: func(t *testing.T) (*Config, error) {
				return Unmarshal([]byte(validConfig))
			},
		},
		{
			name: "LoadConfigString",
			loader: func(t *testing.T) (*Config, error) {
				return LoadConfigString([]byte(validConfig))
			},
		},
		{
			name: "LoadConfigFile",
			loader: func(t *testing.T) (*Config, error) {
				tmpDir := t.TempDir()
				cfgPath := filepath.Join(tmpDir, "p4logtail.yaml")
				if err := os.WriteFile(cfgPath, []byte(validConfig), 0o600); err != nil {
					t.Fatalf("failed to write temp config: %v", err)
				}
				return LoadConfigFile(cfgPath)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := tc.loader(t)
			if err != nil {
				t.Fatalf("%s returned unexpected error: %v", tc.name, err)
			}
			if cfg.P4Log != "/p4/1/logs/log" {
				t.Fatalf("unexpected P4Log: got %q", cfg.P4Log)
			}
			if cfg.JSONLog != "/p4/1/logs/p4logtail.json" {
				t.Fatalf("unexpected JSONLog: got %q", cfg.JSONLog)
			}
		})
	}
}

func TestUnmarshalInvalidYAML(t *testing.T) {
	_, err := Unmarshal([]byte("p4_log: [unterminated"))
	if err == nil {
		t.Fatalf("expected Unmarshal to fail for invalid YAML")
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("expected invalid configuration error, got: %v", err)
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	_, err := LoadConfigFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		if !strings.Contains(err.Error(), "failed to load") {
			t.Fatalf("expected failed to load error, got: %v", err)
		}
		return
	}
	t.Fatalf("expected LoadConfigFile to fail for missing file")
}
