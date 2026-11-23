package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestInstallerP4MetricsConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("installer requires bash")
	}
	common, err := filepath.Abs("../../../scripts/p4prom_common.sh")
	if err != nil {
		t.Fatal(err)
	}
	sample := loadOrFail(t, SampleConfig)
	for _, test := range []struct {
		name, existing string
	}{
		{name: "new install"},
		{name: "upgrade", existing: defaultConfig},
		{name: "commented example", existing: defaultConfig + "\n# license_updates:\n#   mode: every_update\n"},
		{name: "preserve custom schedule", existing: defaultConfig + `
license_updates:
  mode: scheduled
  times: ["22:30", "04:00"]
  timezone: Europe/London
  start_window: 2m
memlimits:
  enabled: true
  enforce_kills: false
  groups:
  - description: Custom group
    users: ".*"
    cmd_max_percentage: 25%
    user_cumulative_max_percentage: 35%
`},
		{name: "preserve disabled", existing: defaultConfig + "\nlicense_updates:\n  mode: disabled\n"},
		{name: "preserve every update", existing: defaultConfig + "\nlicense_updates:\n  mode: every_update\n"},
		{name: "add limits with existing memory reporting", existing: defaultConfig + "\nmemory_by_user: false\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			filename := filepath.Join(dir, "p4metrics.yaml")
			if test.existing != "" {
				if err := os.WriteFile(filename, []byte(test.existing), 0600); err != nil {
					t.Fatal(err)
				}
			}
			generate := func() []byte {
				t.Helper()
				cmd := exec.Command("bash", "-c", `
set -e
source "$1"
chown() { :; }
p4metrics_config_file="$2"
metrics_root="$3"
SDP_INSTANCE=1
p4port=
p4user=
OSUSER=test
OSGROUP=test
write_or_update_p4metrics_config_file
`, "bash", common, filename, dir)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("installer failed: %v\n%s", err, output)
				}
				content, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				return content
			}
			first := generate()
			cfg := loadOrFail(t, string(first))
			wantLicense := sample.LicenseUpdates
			wantMemlimits := sample.MemLimits
			if test.existing != "" {
				existing := loadOrFail(t, test.existing)
				if strings.Contains(test.existing, "\nlicense_updates:") {
					wantLicense = existing.LicenseUpdates
				}
				if existing.MemLimits != nil {
					wantMemlimits = existing.MemLimits
				}
			}
			if !reflect.DeepEqual(cfg.LicenseUpdates, wantLicense) {
				t.Errorf("license settings: got %+v, want %+v", cfg.LicenseUpdates, wantLicense)
			}
			if !reflect.DeepEqual(cfg.MemLimits, wantMemlimits) {
				t.Errorf("memory settings: got %+v, want %+v", cfg.MemLimits, wantMemlimits)
			}
			if !reflect.DeepEqual(cfg.Notifications, sample.Notifications) {
				t.Errorf("notification settings differ from sample")
			}
			if strings.Contains(test.existing, "\nmemory_by_user: false") && cfg.MemoryByUser {
				t.Error("existing memory_by_user setting was overwritten")
			}
			if second := generate(); string(second) != string(first) {
				t.Fatal("repeated installer run modified existing configuration")
			}
		})
	}
}
