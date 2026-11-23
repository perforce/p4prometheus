package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/perforce/p4prometheus/cmd/p4metrics/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLicenseScheduleSlots(t *testing.T) {
	for _, test := range []struct {
		name, now, zone string
		times           []string
		want            []string
	}{
		{"before", "2026-10-29T00:59:59Z", "UTC", []string{"01:00"}, nil},
		{"exact", "2026-10-29T01:00:00Z", "UTC", []string{"01:00"}, []string{"2026-10-29 01:00"}},
		{"inside window", "2026-10-29T01:04:59Z", "UTC", []string{"01:00"}, []string{"2026-10-29 01:00"}},
		{"window boundary", "2026-10-29T01:05:00Z", "UTC", []string{"01:00"}, []string{"2026-10-29 01:00"}},
		{"too late", "2026-10-29T01:05:00.001Z", "UTC", []string{"01:00"}, nil},
		{"no catch-up", "2026-10-29T12:00:00Z", "UTC", []string{"01:00"}, nil},
		{"multiple", "2026-10-29T23:31:00Z", "UTC", []string{"01:00", "23:30"}, []string{"2026-10-29 23:30"}},
		{"overlapping windows", "2026-10-29T01:04:00Z", "UTC", []string{"01:00", "01:02"}, []string{"2026-10-29 01:00", "2026-10-29 01:02"}},
		{"midnight", "2026-10-30T00:01:00Z", "UTC", []string{"23:59"}, []string{"2026-10-29 23:59"}},
		{"timezone", "2026-07-29T00:01:00Z", "Europe/London", []string{"01:00"}, []string{"2026-07-29 01:00"}},
		{"DST missing", "2026-03-29T01:31:00Z", "Europe/London", []string{"01:30"}, nil},
		{"DST first", "2026-10-25T00:31:00Z", "Europe/London", []string{"01:30"}, []string{"2026-10-25 01:30"}},
		{"DST repeated", "2026-10-25T01:31:00Z", "Europe/London", []string{"01:30"}, []string{"2026-10-25 01:30"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339Nano, test.now)
			require.NoError(t, err)
			loc, err := time.LoadLocation(test.zone)
			require.NoError(t, err)
			slots := licenseScheduleSlots(now, config.LicenseUpdates{
				Times: test.times, Location: loc, StartWindow: 5 * time.Minute,
			})
			assert.Len(t, slots, len(test.want))
			for _, key := range test.want {
				assert.Contains(t, slots, key)
			}
		})
	}
}

// Invoked as a subprocess to exercise the real command execution path.
func TestLicenseCommandHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i
			break
		}
	}
	if index == -1 {
		return
	}
	args := os.Args[index+1:]
	if len(args) < 4 {
		os.Exit(2)
	}
	mode, marker := args[0], args[1]
	command := strings.Join(args[2:], " ")
	log, err := os.OpenFile(marker+".commands", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(3)
	}
	if _, err := fmt.Fprintln(log, command); err != nil {
		os.Exit(3)
	}
	if err := log.Close(); err != nil {
		os.Exit(3)
	}
	response := func(suffix, defaultValue string) {
		data, err := os.ReadFile(marker + suffix)
		if os.IsNotExist(err) {
			fmt.Println(defaultValue)
		} else if err != nil {
			os.Exit(3)
		} else if string(data) == "ERROR" {
			fmt.Fprintln(os.Stderr, "test preflight command failed")
			os.Exit(1)
		} else {
			fmt.Print(string(data))
		}
	}
	switch command {
	case "counter depotStats":
		response(".counter", "0")
		os.Exit(0)
	case "monitor show -l":
		response(".monitor", "")
		os.Exit(0)
	case "info -s":
		fmt.Println("ServerID: test")
		os.Exit(0)
	case "license -u":
	default:
		os.Exit(2)
	}
	if err := os.WriteFile(marker, []byte("started"), 0600); err != nil {
		os.Exit(3)
	}
	if mode == "blocked" {
		for {
			if _, err := os.Stat(marker + ".release"); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if mode == "failure" {
		fmt.Fprintln(os.Stderr, "Perforce password (P4PASSWD) invalid or unset")
		os.Exit(1)
	}
	if mode == "empty" {
		os.Exit(0)
	}
	fmt.Println("... userCount 42")
	fmt.Println("... userLimit 100")
	fmt.Println("... licenseExpires 1893456000")
	os.Exit(0)
}

func newLicenseTestMonitor(t *testing.T, mode, helper string) (*P4MonitorMetrics, string) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.LoadConfigString([]byte(fmt.Sprintf(`
metrics_root: %q
license_updates:
  mode: %s
  times: ["01:00", "01:02"]
  timezone: UTC
`, dir, mode)))
	require.NoError(t, err)
	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)
	marker := filepath.Join(dir, "command-started")
	p4m.initialised = true
	p4m.serverID = "test"
	p4m.p4Cmd = fmt.Sprintf("%q -test.run=^TestLicenseCommandHelper$ -- %s %q", os.Args[0], helper, marker)
	t.Cleanup(p4m.stopLicenseUpdates)
	return p4m, marker
}

func receiveLicenseResult(t *testing.T, p4m *P4MonitorMetrics) licenseUpdateResult {
	t.Helper()
	select {
	case result := <-p4m.licenseUpdates.results:
		return result
	case <-time.After(15 * time.Second):
		t.Fatal("license command did not finish")
		return licenseUpdateResult{}
	}
}

func TestScheduledLicenseUpdateAsyncAndPersistent(t *testing.T) {
	p4m, marker := newLicenseTestMonitor(t, "scheduled", "blocked")
	at := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)

	p4m.startLicenseUpdate(at.Add(-time.Minute))
	assert.Nil(t, p4m.licenseUpdates.cancel)
	p4m.startLicenseUpdate(at)
	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 15*time.Second, 5*time.Millisecond)

	p4m.monitorUptime()
	assert.Equal(t, "p4_uptime", p4m.metricsFilePrefix)
	uptime, err := os.ReadFile(p4m.metricsFilename("p4_uptime"))
	require.NoError(t, err)
	assert.Contains(t, string(uptime), "p4_server_uptime")
	// No overlap or late retry for the second slot, even if the first is busy.
	p4m.startLicenseUpdate(at.Add(2 * time.Minute))
	require.Len(t, p4m.licenseUpdates.state.Attempts, 2)
	require.NoError(t, os.WriteFile(marker+".release", nil, 0600))
	result := receiveLicenseResult(t, p4m)
	require.NoError(t, result.err)
	p4m.finishLicenseUpdate(result)
	assert.Equal(t, "42", p4m.p4license["userCount"])
	assert.Nil(t, p4m.licenseUpdates.cancel)
	p4m.startLicenseUpdate(at.Add(3 * time.Minute))
	assert.Nil(t, p4m.licenseUpdates.cancel)

	content, err := os.ReadFile(p4m.metricsFilename("p4_license"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "p4_licensed_user_count{serverid=\"test\"} 42")
	assert.Contains(t, string(content), fmt.Sprintf("p4_license_last_success_timestamp_seconds{serverid=\"test\"} %d", result.completed.Unix()))

	// A new process restores figures and does not repeat either attempted slot.
	env := map[string]string{}
	restarted := newP4MonitorMetrics(p4m.config, &env, tlogger)
	restarted.p4Cmd, restarted.serverID, restarted.initialised = p4m.p4Cmd, "test", true
	t.Cleanup(restarted.stopLicenseUpdates)
	restarted.startLicenseUpdate(at.Add(4 * time.Minute))
	assert.Nil(t, restarted.licenseUpdates.cancel)
	assert.Equal(t, p4m.p4license, restarted.p4license)
	assert.True(t, restarted.licenseUpdates.state.LastSuccess.Equal(result.completed))

	restarted.startLicenseUpdate(at.Add(24 * time.Hour))
	require.NotNil(t, restarted.licenseUpdates.cancel)
	restarted.finishLicenseUpdate(receiveLicenseResult(t, restarted))
}

func TestLicenseUpdateFailureRetainsCacheAndDoesNotRetry(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "scheduled", "failure")
	at := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	require.True(t, p4m.loadLicenseState())
	p4m.p4license = map[string]string{"userCount": "12", "licenseExpires": "1893456000"}
	p4m.licenseUpdates.state.Values = p4m.p4license
	p4m.licenseUpdates.state.LastSuccess = at.Add(-time.Hour)
	p4m.startLicenseUpdate(at)
	result := receiveLicenseResult(t, p4m)
	require.Error(t, result.err)
	p4m.finishLicenseUpdate(result)
	assert.True(t, p4m.loginError)
	assert.Equal(t, "12", p4m.p4license["userCount"])
	assert.Equal(t, at.Add(-time.Hour), p4m.licenseUpdates.state.LastSuccess)
	p4m.startLicenseUpdate(at.Add(time.Minute))
	assert.Nil(t, p4m.licenseUpdates.cancel)
	content, err := os.ReadFile(p4m.metricsFilename("p4_license"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "p4_licensed_user_count{serverid=\"test\"} 12")
}

func TestEveryUpdateAndDisabledLicenseUpdates(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "every_update", "success")
	p4m.monitorLicense()
	p4m.finishLicenseUpdate(receiveLicenseResult(t, p4m))
	p4m.monitorLicense()
	require.NotNil(t, p4m.licenseUpdates.cancel)
	p4m.finishLicenseUpdate(receiveLicenseResult(t, p4m))
	p4m.config.LicenseUpdates.Mode = "disabled"
	p4m.monitorLicense()
	assert.Nil(t, p4m.licenseUpdates.cancel)
	_, err := os.Stat(p4m.metricsFilename("p4_license"))
	assert.True(t, os.IsNotExist(err))
}

func TestLicenseScheduledStartupAndReloadDoNotForceExpiredRefresh(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "scheduled", "success")
	// A future minute cannot have occurred within the five-minute window.
	p4m.config.LicenseUpdates.Times = []string{time.Now().UTC().Add(time.Hour).Format("15:04")}
	p4m.monitorLicense()
	result := receiveLicenseResult(t, p4m)
	require.True(t, result.skipped)
	p4m.finishLicenseUpdate(result)
	assert.Nil(t, p4m.licenseUpdates.cancel)
	cfg := *p4m.config
	p4m.config = &cfg
	p4m.monitorLicense()
	result = receiveLicenseResult(t, p4m)
	require.True(t, result.skipped)
	p4m.finishLicenseUpdate(result)
	assert.Nil(t, p4m.licenseUpdates.cancel)
	content, err := os.ReadFile(p4m.metricsFilename("p4_license"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "p4_license_last_success_timestamp_seconds{serverid=\"test\"} 0")
}

func TestLicenseCountdownFromCachedExpiry(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "scheduled", "success")
	at := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	p4m.licenseUpdates.state.LastSuccess = at
	for _, test := range []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"license expiry", map[string]string{"licenseExpires": fmt.Sprint(at.Add(time.Hour).Unix()), "licenseTimeRemaining": "3600"}, "3540"},
		{"support expiry", map[string]string{"supportExpires": fmt.Sprint(at.Add(time.Hour).Unix())}, "3540"},
		{"remaining only", map[string]string{"licenseTimeRemaining": "3600"}, "3540"},
		{"expired", map[string]string{"licenseExpires": fmt.Sprint(at.Unix())}, "-60"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p4m.p4license = test.values
			p4m.writeLicenseMetrics(at.Add(time.Minute))
			content, err := os.ReadFile(p4m.metricsFilename("p4_license"))
			require.NoError(t, err)
			assert.Contains(t, string(content), "p4_license_time_remaining{serverid=\"test\"} "+test.want)
		})
	}
}

func TestLicenseStateErrorsPreventScheduledCalls(t *testing.T) {
	for _, content := range []string{"not json", ""} {
		t.Run(fmt.Sprintf("state %q", content), func(t *testing.T) {
			p4m, marker := newLicenseTestMonitor(t, "scheduled", "success")
			require.NoError(t, os.WriteFile(p4m.outputFilename("p4_license", ".json"), []byte(content), 0600))
			p4m.startLicenseUpdate(time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC))
			assert.Nil(t, p4m.licenseUpdates.cancel)
			_, err := os.Stat(marker)
			assert.True(t, os.IsNotExist(err))
		})
	}
	p4m, _ := newLicenseTestMonitor(t, "scheduled", "success")
	require.NoError(t, os.Mkdir(p4m.outputFilename("p4_license", ".json")+".tmp", 0700))
	p4m.startLicenseUpdate(time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC))
	assert.Nil(t, p4m.licenseUpdates.cancel)
}

func TestLicenseDSTRepeatedSlotIsNotRetried(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "scheduled", "success")
	loc, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	p4m.config.LicenseUpdates.Location = loc
	p4m.config.LicenseUpdates.Times = []string{"01:30"}
	at := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	p4m.startLicenseUpdate(at)
	p4m.finishLicenseUpdate(receiveLicenseResult(t, p4m))
	p4m.startLicenseUpdate(at.Add(time.Hour))
	assert.Nil(t, p4m.licenseUpdates.cancel)
}

func TestLicenseResultDiscardedAfterServerChange(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "every_update", "success")
	p4m.startLicenseUpdate(time.Now())
	result := receiveLicenseResult(t, p4m)
	p4m.p4Cmd += " -p different-server:1666"
	p4m.finishLicenseUpdate(result)
	assert.Empty(t, p4m.p4license)
	assert.True(t, p4m.licenseUpdates.state.LastSuccess.IsZero())
}

func TestLicenseDryRunDoesNotWriteState(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "scheduled", "success")
	p4m.dryrun = true
	p4m.startLicenseUpdate(time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC))
	p4m.finishLicenseUpdate(receiveLicenseResult(t, p4m))
	files, err := os.ReadDir(p4m.config.MetricsRoot)
	require.NoError(t, err)
	for _, file := range files {
		assert.False(t, strings.HasSuffix(file.Name(), ".json") || strings.HasSuffix(file.Name(), ".prom"))
	}
}

func TestLicenseUpdateCancellation(t *testing.T) {
	p4m, marker := newLicenseTestMonitor(t, "every_update", "blocked")
	p4m.startLicenseUpdate(time.Now())
	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 15*time.Second, 5*time.Millisecond)
	p4m.stopLicenseUpdates()
	result := receiveLicenseResult(t, p4m)
	assert.Error(t, result.err)
	select {
	case <-p4m.licenseUpdates.done:
	default:
		t.Fatal("command worker was not stopped")
	}
}

func TestLicenseEmptyResponseDoesNotMarkSuccess(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "every_update", "empty")
	p4m.startLicenseUpdate(time.Now())
	p4m.finishLicenseUpdate(receiveLicenseResult(t, p4m))
	assert.True(t, p4m.licenseUpdates.state.LastSuccess.IsZero())
}

func TestLicenseDisabledWhileRunning(t *testing.T) {
	p4m, _ := newLicenseTestMonitor(t, "every_update", "success")
	p4m.startLicenseUpdate(time.Now())
	p4m.config.LicenseUpdates.Mode = "disabled"
	p4m.finishLicenseUpdate(receiveLicenseResult(t, p4m))
	assert.Equal(t, "42", p4m.p4license["userCount"])
	_, err := os.Stat(p4m.metricsFilename("p4_license"))
	assert.True(t, os.IsNotExist(err))
}

func TestDepotStatsCalculated(t *testing.T) {
	for _, test := range []struct {
		name, value string
		want        int64
		invalid     bool
	}{
		{"example", "revs(53995281R/29063043F); changes(1135714T); users(1100); lastCalculated(1793349845);#encrypted", 1793349845, false},
		{"field only", "lastCalculated(1793349845);#encrypted", 1793349845, false},
		{"missing counter", "0", 0, false},
		{"empty counter", "", 0, false},
		{"never calculated", "lastCalculated(0);", 0, false},
		{"missing field", "users(1100);#encrypted", 0, true},
		{"wrong field", "notlastCalculated(1793349845);", 0, true},
		{"not numeric", "lastCalculated(invalid);", 0, true},
		{"negative", "lastCalculated(-1);", 0, true},
		{"overflow", "lastCalculated(999999999999999999999);", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseDepotStatsCalculated([]string{test.value})
			if test.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if test.want == 0 {
				assert.True(t, got.IsZero())
			} else {
				assert.Equal(t, test.want, got.Unix())
			}
		})
	}
}

func TestLicenseStatsAgeBoundary(t *testing.T) {
	now := time.Now()
	assert.True(t, licenseStatsFresh(now, now.Add(-24*time.Hour+time.Nanosecond)))
	assert.False(t, licenseStatsFresh(now, now.Add(-24*time.Hour)))
	assert.False(t, licenseStatsFresh(now, now.Add(-24*time.Hour-time.Nanosecond)))
	assert.False(t, licenseStatsFresh(now, time.Time{}))
	assert.False(t, licenseStatsFresh(now, now.Add(time.Second)))
}

func TestCalculateStatsActive(t *testing.T) {
	for _, test := range []struct {
		line string
		want bool
	}{
		{"123 R server-host 00:01:00 calculateStats", true},
		{"123 B server-host 00:01:00 calculateStats", true},
		{"123 I server-host 00:01:00 calculateStats", false},
		{"123 T server-host 00:01:00 calculateStats", false},
		{"123 R user 00:00:00 monitor show calculateStats", false},
		{"123 R user 00:00:00 calculateStatsOther", false},
		{"invalid R host 00:01:00 calculateStats", false},
		{"calculateStats", false},
	} {
		assert.Equal(t, test.want, calculateStatsActive([]string{test.line}), test.line)
	}
}

func TestLicenseDepotStatsGate(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	fresh := fmt.Sprintf("users(1100); lastCalculated(%d);#encrypted", at.Add(-time.Hour).Unix())
	expired := fmt.Sprintf("lastCalculated(%d);", at.Add(-25*time.Hour).Unix())
	active := "123 R server-host 00:01:00 calculateStats\n"
	for _, test := range []struct {
		name, counter, monitor string
		inWindow, wantCall     bool
		wantError, wantWarning bool
	}{
		{name: "fresh outside window", counter: fresh, wantCall: true},
		{name: "expired outside window", counter: expired},
		{name: "exactly 24 hours outside window", counter: fmt.Sprintf("lastCalculated(%d);", at.Add(-24*time.Hour).Unix())},
		{name: "expired inside window", counter: expired, inWindow: true, wantCall: true},
		{name: "missing outside window", counter: "0"},
		{name: "missing inside window", counter: "0", inWindow: true, wantCall: true},
		{name: "expired with active calculation", counter: expired, monitor: active, wantCall: true},
		{name: "missing with active calculation", counter: "0", monitor: active, wantCall: true},
		{name: "idle calculation is not active", counter: expired, monitor: "123 I host 00:00:00 calculateStats"},
		{name: "malformed outside window", counter: "bad counter", wantWarning: true},
		{name: "malformed inside window", counter: "bad counter", inWindow: true, wantCall: true, wantWarning: true},
		{name: "future timestamp", counter: fmt.Sprintf("lastCalculated(%d);", at.Add(time.Hour).Unix()), wantWarning: true},
		{name: "counter failure", counter: "ERROR", wantError: true},
		{name: "counter failure inside window", counter: "ERROR", inWindow: true, wantError: true},
		{name: "monitor failure", counter: expired, monitor: "ERROR", wantError: true},
		{name: "fresh does not need monitor", counter: fresh, monitor: "ERROR", wantCall: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p4m, marker := newLicenseTestMonitor(t, "scheduled", "success")
			if test.inWindow {
				p4m.config.LicenseUpdates.Times = []string{"12:00"}
			}
			require.NoError(t, os.WriteFile(marker+".counter", []byte(test.counter), 0600))
			require.NoError(t, os.WriteFile(marker+".monitor", []byte(test.monitor), 0600))
			p4m.pollLicenseUpdate(at)
			result := receiveLicenseResult(t, p4m)
			assert.Equal(t, test.wantError, result.err != nil)
			assert.Equal(t, test.wantWarning, len(result.warnings) > 0)
			assert.Equal(t, !test.wantCall && !test.wantError, result.skipped)
			p4m.finishLicenseUpdate(result)
			commands, err := os.ReadFile(marker + ".commands")
			require.NoError(t, err)
			assert.Contains(t, string(commands), "counter depotStats\n")
			assert.Equal(t, test.wantCall, strings.Contains(string(commands), "license -u\n"))
			if test.wantCall {
				assert.Equal(t, "42", p4m.p4license["userCount"])
			} else {
				assert.Empty(t, p4m.p4license)
			}
		})
	}
}

func TestLicensePreflightCannotStartExpiredScanAfterWindow(t *testing.T) {
	p4m, marker := newLicenseTestMonitor(t, "scheduled", "success")
	p4m.config.LicenseUpdates.StartWindow = time.Nanosecond
	p4m.startLicenseUpdate(time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC))
	result := receiveLicenseResult(t, p4m)
	require.True(t, result.skipped)
	p4m.finishLicenseUpdate(result)
	_, err := os.Stat(marker)
	assert.True(t, os.IsNotExist(err))
}

func TestLicensePollingFollowsServerStatsState(t *testing.T) {
	p4m, marker := newLicenseTestMonitor(t, "scheduled", "success")
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	require.True(t, p4m.loadLicenseState())
	p4m.p4license = map[string]string{"userCount": "12"}
	p4m.licenseUpdates.state.Values = p4m.p4license
	p4m.licenseUpdates.state.LastSuccess = at.Add(-time.Hour)
	for _, step := range []struct {
		name, counter, monitor string
		wantCall               bool
		wantUsers              string
	}{
		{"expired", "0", "", false, "12"},
		{"scan running", "0", "123 B server-host 00:01:00 calculateStats", true, "42"},
		{"scan no longer visible", "0", "", false, "42"},
		{"scan completed", fmt.Sprintf("lastCalculated(%d);", at.Add(-time.Minute).Unix()), "", true, "42"},
	} {
		t.Run(step.name, func(t *testing.T) {
			previous := p4m.licenseUpdates.state.LastSuccess
			require.NoError(t, os.WriteFile(marker+".counter", []byte(step.counter), 0600))
			require.NoError(t, os.WriteFile(marker+".monitor", []byte(step.monitor), 0600))
			p4m.pollLicenseUpdate(at)
			result := receiveLicenseResult(t, p4m)
			require.NoError(t, result.err)
			assert.Equal(t, !step.wantCall, result.skipped)
			p4m.finishLicenseUpdate(result)
			assert.Equal(t, step.wantUsers, p4m.p4license["userCount"])
			if !step.wantCall {
				assert.Equal(t, previous, p4m.licenseUpdates.state.LastSuccess)
			}
			assert.Empty(t, p4m.licenseUpdates.state.Attempts)
		})
	}
}

func TestServerInfoUsesShortOutput(t *testing.T) {
	p4m, marker := newLicenseTestMonitor(t, "scheduled", "success")
	require.NoError(t, p4m.runInfo())
	commands, err := os.ReadFile(marker + ".commands")
	require.NoError(t, err)
	assert.Equal(t, "info -s\n", string(commands))
	assert.Equal(t, "test", p4m.p4info["ServerID"])
}
