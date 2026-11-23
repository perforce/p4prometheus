package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bitfield/script"
	"github.com/perforce/p4prometheus/cmd/p4metrics/config"
)

type licenseUpdateState struct {
	Command     string               `json:"command"`
	Attempts    map[string]time.Time `json:"attempts"`
	Values      map[string]string    `json:"values"`
	LastSuccess time.Time            `json:"last_success"`
}

type licenseUpdateResult struct {
	command   string
	statePath string
	lines     []string
	stderr    string
	err       error
	completed time.Time
	skipped   bool
	warnings  []string
}

type licenseUpdater struct {
	state     licenseUpdateState
	statePath string
	results   chan licenseUpdateResult
	cancel    context.CancelFunc
	done      chan struct{}
}

// Walk actual elapsed minutes, not constructed local times: nonexistent DST
// times never match, and both instances of a repeated time share the same key.
func licenseScheduleSlots(now time.Time, cfg config.LicenseUpdates) map[string]time.Time {
	loc := cfg.Location
	if loc == nil {
		loc = time.Local
	}
	slots := make(map[string]time.Time)
	for candidate := now.Truncate(time.Minute); !candidate.Before(now.Add(-cfg.StartWindow)); candidate = candidate.Add(-time.Minute) {
		local := candidate.In(loc)
		for _, scheduled := range cfg.Times {
			if local.Format("15:04") == scheduled {
				slots[local.Format("2006-01-02")+" "+scheduled] = candidate
			}
		}
	}
	return slots
}

func (p4m *P4MonitorMetrics) loadLicenseState() bool {
	u := &p4m.licenseUpdates
	filename := p4m.outputFilename("p4_license", ".json")
	if u.statePath == filename && u.state.Command == p4m.p4Cmd {
		return true
	}
	state := licenseUpdateState{Command: p4m.p4Cmd}
	data, err := os.ReadFile(filename)
	if err != nil && !os.IsNotExist(err) {
		p4m.logger.Errorf("Cannot read license update state %s: %v", filename, err)
		return false
	}
	if err == nil {
		var saved licenseUpdateState
		if err := json.Unmarshal(data, &saved); err != nil {
			p4m.logger.Errorf("Cannot parse license update state %s: %v", filename, err)
			return false
		}
		if saved.Command == p4m.p4Cmd {
			state = saved
		}
	}
	if state.Attempts == nil {
		state.Attempts = make(map[string]time.Time)
	}
	u.state, u.statePath = state, filename
	p4m.p4license = state.Values
	return true
}

func (p4m *P4MonitorMetrics) saveLicenseState() bool {
	if p4m.dryrun {
		return true
	}
	u := &p4m.licenseUpdates
	data, err := json.Marshal(u.state)
	if err == nil {
		err = os.WriteFile(u.statePath+".tmp", data, 0600)
	}
	if err == nil {
		err = os.Rename(u.statePath+".tmp", u.statePath)
	}
	if err != nil {
		p4m.logger.Errorf("Cannot save license update state %s: %v", u.statePath, err)
		return false
	}
	return true
}

func (p4m *P4MonitorMetrics) startLicenseUpdate(now time.Time) {
	select {
	case result := <-p4m.licenseUpdates.results:
		p4m.finishLicenseUpdate(result)
	default:
	}
	cfg := p4m.config.LicenseUpdates
	if cfg.Mode == "disabled" || !p4m.initialised {
		return
	}
	if !p4m.loadLicenseState() {
		return
	}
	u := &p4m.licenseUpdates
	if cfg.Mode == "scheduled" {
		due := false
		for key, slot := range licenseScheduleSlots(now, cfg) {
			if _, attempted := u.state.Attempts[key]; !attempted {
				u.state.Attempts[key] = slot
				due = true
			}
		}
		if !due {
			return
		}
		for key, slot := range u.state.Attempts {
			if slot.Before(now.Add(-48 * time.Hour)) {
				delete(u.state.Attempts, key)
			}
		}
		// Record the attempt before starting, including slots skipped while busy.
		if !p4m.saveLicenseState() {
			return
		}
		if u.cancel != nil {
			p4m.logger.Warn("Skipping scheduled license update: previous call is still running")
			return
		}
	} else if u.cancel != nil {
		return
	}
	p4m.launchLicenseUpdate(now, true)
}

func (p4m *P4MonitorMetrics) launchLicenseUpdate(now time.Time, allowRefresh bool) {
	u := &p4m.licenseUpdates
	if u.results == nil {
		u.results = make(chan licenseUpdateResult, 1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.done = make(chan struct{})
	command, cfg, env := p4m.p4Cmd, p4m.config.LicenseUpdates, os.Environ()
	results, done, statePath := u.results, u.done, u.statePath
	started := time.Now()
	p4m.logger.Debug("Checking license update")
	go func() {
		defer close(done)
		result := runLicenseUpdate(ctx, command, env, cfg, now.Add(time.Since(started)), allowRefresh)
		result.statePath = statePath
		results <- result
	}()
}

// Safe cached reads are polled normally even outside the scheduled occurrences.
func (p4m *P4MonitorMetrics) pollLicenseUpdate(now time.Time) {
	p4m.startLicenseUpdate(now)
	if p4m.config.LicenseUpdates.Mode == "scheduled" && p4m.initialised &&
		p4m.licenseUpdates.cancel == nil && p4m.loadLicenseState() {
		p4m.launchLicenseUpdate(now, false)
	}
}

var lastCalculatedPattern = regexp.MustCompile(`(?:^|[;[:space:]])lastCalculated\(([0-9]+)\)(?:;|$)`)

func parseDepotStatsCalculated(lines []string) (time.Time, error) {
	value := strings.TrimSpace(strings.Join(lines, "\n"))
	if value == "0" || value == "" {
		return time.Time{}, nil
	}
	match := lastCalculatedPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return time.Time{}, fmt.Errorf("depotStats counter has no valid lastCalculated timestamp")
	}
	seconds, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid depotStats lastCalculated timestamp: %w", err)
	}
	if seconds == 0 {
		return time.Time{}, nil
	}
	return time.Unix(seconds, 0), nil
}

func calculateStatsActive(lines []string) bool {
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[4] != "calculateStats" {
			continue
		}
		if _, err := strconv.Atoi(fields[0]); err == nil && (fields[1] == "R" || fields[1] == "B") {
			return true
		}
	}
	return false
}

func licenseStatsFresh(now, calculated time.Time) bool {
	return !calculated.IsZero() && !calculated.After(now) && now.Sub(calculated) < 24*time.Hour
}

func runLicenseUpdate(ctx context.Context, command string, env []string, cfg config.LicenseUpdates, now time.Time, allowRefresh bool) licenseUpdateResult {
	started := time.Now()
	currentTime := func() time.Time { return now.Add(time.Since(started)) }
	result := licenseUpdateResult{command: command + " license -u"}
	execute := func(args string) ([]string, error) {
		errbuf := new(bytes.Buffer)
		lines, err := script.NewPipe().WithContext(ctx).WithEnv(env).WithStderr(errbuf).Exec(command + " " + args).Slice()
		if err != nil {
			result.stderr = errbuf.String()
			return nil, fmt.Errorf("%s: %w", args, err)
		}
		return lines, nil
	}
	if cfg.Mode == "scheduled" {
		counter, err := execute("counter depotStats")
		if err != nil {
			result.err = err
			return result
		}
		calculated, err := parseDepotStatsCalculated(counter)
		if err != nil {
			result.warnings = append(result.warnings, err.Error())
		}
		if calculated.After(currentTime()) {
			result.warnings = append(result.warnings, "depotStats lastCalculated is in the future; treating stats as unknown")
		}
		if !licenseStatsFresh(currentTime(), calculated) {
			monitor, err := execute("monitor show -l")
			if err != nil {
				result.err = err
				return result
			}
			if !calculateStatsActive(monitor) && (!allowRefresh || len(licenseScheduleSlots(currentTime(), cfg)) == 0) {
				result.skipped = true
				return result
			}
		}
		// Recheck freshness immediately before execution: preflight commands may
		// have crossed either the 24-hour boundary or the end of the start window.
		if licenseStatsFresh(now, calculated) && !licenseStatsFresh(currentTime(), calculated) {
			result.skipped = true
			return result
		}
	}
	result.lines, result.err = execute("license -u")
	result.completed = time.Now()
	return result
}

func (p4m *P4MonitorMetrics) finishLicenseUpdate(result licenseUpdateResult) {
	u := &p4m.licenseUpdates
	u.cancel()
	u.cancel = nil
	if result.statePath != p4m.outputFilename("p4_license", ".json") ||
		result.command != p4m.p4Cmd+" license -u" {
		p4m.logger.Warn("Discarding license update result after server configuration changed")
		return
	}
	for _, warning := range result.warnings {
		p4m.logger.Warn(warning)
	}
	if result.skipped {
		p4m.logger.Debug("Skipping license -u: depotStats expired or unknown outside an unused scheduled window, and calculateStats is not active")
		p4m.writeLicenseMetrics(time.Now())
		return
	}
	if result.err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", result.command, result.err,
			bytes.NewBufferString(result.stderr))
	} else {
		previous := p4m.p4license
		p4m.parseLicenseResponse(result.lines)
		if len(p4m.p4license) == 0 {
			p4m.p4license = previous
			p4m.logger.Errorf("License update returned no license fields: %q", result.lines)
			p4m.writeLicenseMetrics(time.Now())
			return
		}
		u.state.Values = p4m.p4license
		u.state.LastSuccess = result.completed
		p4m.saveLicenseState()
		p4m.logger.Debug("License update completed")
	}
	p4m.writeLicenseMetrics(time.Now())
}

func (p4m *P4MonitorMetrics) stopLicenseUpdates() {
	u := &p4m.licenseUpdates
	if u.cancel != nil {
		u.cancel()
		<-u.done
		u.cancel = nil
	}
}

func (p4m *P4MonitorMetrics) writeLicenseMetrics(now time.Time) {
	p4m.startMonitor("monitorLicense", "p4_license")
	defer p4m.completeMonitor()
	if p4m.config.LicenseUpdates.Mode == "disabled" {
		return
	}
	lastSuccess := p4m.licenseUpdates.state.LastSuccess
	if !lastSuccess.IsZero() {
		p4m.parseLicenseAt(now)
	}
	lastSuccessSeconds := int64(0)
	if !lastSuccess.IsZero() {
		lastSuccessSeconds = lastSuccess.Unix()
	}
	p4m.metrics = append(p4m.metrics, metricStruct{
		name:  "p4_license_last_success_timestamp_seconds",
		help:  "Last successful P4D license collection (epoch secs), or 0 if never collected",
		mtype: "gauge", value: strconv.FormatInt(lastSuccessSeconds, 10),
	})
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) cachedLicenseTimeRemaining(now time.Time) string {
	for _, field := range []string{"licenseExpires", "supportExpires"} {
		if value := p4m.p4license[field]; value != "" {
			expiry, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				p4m.logger.Warnf("Invalid license %s %q: %v", field, value, err)
				continue
			}
			return strconv.FormatInt(expiry-now.Unix(), 10)
		}
	}
	if value := p4m.p4license["licenseTimeRemaining"]; value != "" {
		remaining, err := strconv.ParseFloat(value, 64)
		if err != nil {
			p4m.logger.Warnf("Invalid licenseTimeRemaining %q: %v", value, err)
			return ""
		}
		return fmt.Sprintf("%.0f", remaining-now.Sub(p4m.licenseUpdates.state.LastSuccess).Seconds())
	}
	return ""
}
