// This is a version of monitor_metrics.sh in Go as part of p4prometheus
// It is intended to be more reliable and cross platform than the original.
// It should be run permanently as a systemd service on Linux, as it tails
// the errors.csv file
package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/perforce/p4prometheus/cmd/p4metrics/config"

	"github.com/bitfield/script"
	"github.com/perforce/p4prometheus/version"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

// GO standard reference value/format: Mon Jan 2 15:04:05 -0700 MST 2006
const p4InfoTimeFormat = "2006/01/02 15:04:05 -0700 MST"
const checkpointTimeFormat = "2006-01-02 15:04:05"
const opensslTimeFormat = "Jan 2 15:04:05 2006 MST"

var logger logrus.Logger

func sourceEnvVars() map[string]string {
	// Return a list of p4 env vars
	env := make(map[string]string)
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		env[pair[0]] = pair[1]
	}
	return env
}

func sourceSDPVars(sdpInstance string, logger *logrus.Logger) map[string]string {
	// Source SDP vars and return a list
	logger.Debugf("sourceSDPVars: %s", sdpInstance)
	cmd := exec.Command("bash", "-c", fmt.Sprintf("source /p4/common/bin/p4_vars %s && env", sdpInstance))

	// Get the current environment
	oldEnv := make(map[string]string)
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		oldEnv[pair[0]] = pair[1]
	}

	// Run the command and capture output
	results := make(map[string]string)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error running script: %v\n", err)
		return results
	}

	// Parse the new environment
	newEnv := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		pair := strings.SplitN(line, "=", 2)
		if len(pair) == 2 {
			newEnv[pair[0]] = pair[1]
		}
	}

	// Other interesting env vars
	otherVars := []string{"KEEPCKPS", "KEEPJNLS", "KEEPLOGS", "CHECKPOINTS", "LOGS", "OSUSER"}
	for k, v := range newEnv {
		if strings.HasPrefix(k, "P4") || strings.Contains(k, "SDP") {
			results[k] = v
		}
		for _, s := range otherVars {
			if k == s {
				results[k] = v
			}
		}
	}
	return results
}

func getVar(vars map[string]string, k string) string {
	if v, ok := vars[k]; ok {
		return v
	}
	return ""
}

// defines metrics label
type labelStruct struct {
	name  string
	value string
}

type metricStruct struct {
	name  string
	help  string
	mtype string
	value string
	label labelStruct
}

// P4MonitorMetrics structure
type P4MonitorMetrics struct {
	config            *config.Config
	env               *map[string]string
	logger            *logrus.Logger
	p4User            string
	serverID          string
	rootDir           string
	logsDir           string
	p4Cmd             string
	sdpInstance       string
	sdpInstanceLabel  string
	sdpInstanceSuffix string
	p4info            map[string]string
	p4license         map[string]string
	logFile           string
	errorsFile        string
	metricsFilePrefix string
	metrics           []metricStruct
}

func newP4MonitorMetrics(config *config.Config, envVars *map[string]string, logger *logrus.Logger) (p4m *P4MonitorMetrics) {
	return &P4MonitorMetrics{
		config:    config,
		env:       envVars,
		logger:    logger,
		p4info:    make(map[string]string),
		p4license: make(map[string]string),
		metrics:   make([]metricStruct, 0),
	}
}

func (p4m *P4MonitorMetrics) initVars() {
	// Note that P4BIN is defined by SDP by sourcing above file, as are P4USER, P4PORT
	p4m.sdpInstance = getVar(*p4m.env, "SDP_INSTANCE")
	p4m.p4User = getVar(*p4m.env, "P4USER")
	p4trust := getVar(*p4m.env, "P4TRUST")
	if p4trust != "" {
		p4m.logger.Debugf("setting P4TRUST=%s", p4trust)
		os.Setenv("P4TRUST", p4trust)
	}
	p4tickets := getVar(*p4m.env, "P4TICKETS")
	if p4tickets != "" {
		p4m.logger.Debugf("setting P4TICKETS=%s", p4tickets)
		os.Setenv("P4TICKETS", p4tickets)
	}
	p4m.p4Cmd = fmt.Sprintf("%s -u %s -p \"%s\"", getVar(*p4m.env, "P4BIN"), p4m.p4User, getVar(*p4m.env, "P4PORT"))
	p4m.logger.Debugf("p4Cmd: %s", p4m.p4Cmd)
	p4cmd, errbuf, p := p4m.newP4CmdPipe("info -s")
	i, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error: %v, %q", err, errbuf.String())
		p4m.logger.Fatalf("Can't connect to P4PORT: %s", getVar(*p4m.env, "P4PORT"))
	}
	for _, s := range i {
		parts := strings.Split(s, ": ")
		if len(parts) == 2 {
			p4m.p4info[parts[0]] = parts[1]
		}
	}
	p4m.logger.Debugf("p4info -s: %d %v\n%v", len(i), i, p4m.p4info)
	p4m.sdpInstanceLabel = fmt.Sprintf(",sdpinst=\"%s\"", p4m.sdpInstance)
	p4m.logger.Debugf("sdpInstanceLabel: %s", p4m.sdpInstanceLabel)
	p4m.sdpInstanceSuffix = fmt.Sprintf("-%s", p4m.sdpInstance)
	p4m.logger.Debugf("sdpInstanceSuffix: %s", p4m.sdpInstanceSuffix)
	p4m.logFile = getVar(*p4m.env, "P4LOG")
	p4m.logger.Debugf("logFile: %s", p4m.logFile)
	p4m.logsDir = getVar(*p4m.env, "LOGS")
	p4m.logger.Debugf("LOGS: %s", p4m.logsDir)
	p4m.errorsFile = path.Join(p4m.logsDir, "errors.csv")
	p4m.logger.Debugf("errorsFile: %s", p4m.errorsFile)
	// Get server id. Usually server.id files are a single line containing the
	// ServerID value. However, a server.id file will have a second line if a
	// 'p4 failover' was done containing an error message displayed to users
	// during the failover, and also preventing the service from starting
	// post-failover (to avoid split brain). For purposes of this check, we care
	// only about the ServerID value contained on the first line, so we use
	// 'head -1' on the server.id file.
	p4m.rootDir = getVar(*p4m.env, "P4ROOT")
	idFile := path.Join(p4m.rootDir, "server.id")
	if _, err := os.Stat(idFile); err == nil {
		s, err := script.File(idFile).Slice()
		if err == nil && len(s) > 0 {
			p4m.serverID = s[0]
			p4m.logger.Debugf("found server.id")
		} else {
			p4m.serverID = p4m.p4info["ServerID"]
		}
	}
	if p4m.serverID == "" {
		p4m.serverID = "UnsetServerID"
	}
	p4m.logger.Debugf("serverID: %s", p4m.serverID)
}

// $ p4 info -s
// User name: perforce
// Client name: 84e26b1e03ba
// Client host: 84e26b1e03ba
// Current directory: /home/perforce
// Peer address: 127.0.0.1:54110
// Client address: 127.0.0.1
// Server address: localhost:1999
// Server root: /p4/1/root
// Server date: 2025/01/20 17:14:47 +0000 UTC
// Server uptime: 73:17:53
// Server version: P4D/LINUX26AARCH64/2024.2/2697822 (2024/12/18)
// Server encryption: encrypted
// Server cert expires: Jan 15 15:56:45 2035 GMT
// ServerID: master.1
// Server services: standard
// Server license: none
// Case Handling: sensitive

func (p4m *P4MonitorMetrics) parseUptime(value string) int64 {
	// Takes a string, e.g. 123:23:19
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		p4m.logger.Warningf("parseUptime: failed to split: '%s'", value)
		return 0
	}
	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		p4m.logger.Warningf("parseUptime: invalid hours: %v", err)
		return 0
	}
	minutes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		p4m.logger.Warningf("parseUptime: invalid minutes: %v", err)
		return 0
	}
	if minutes > 59 {
		p4m.logger.Warningf("parseUptime: minutes must be between 0 and 59")
		return 0
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		p4m.logger.Warningf("parseUptime: invalid seconds: %v", err)
		return 0
	}
	if seconds > 59 {
		p4m.logger.Warningf("parseUptime: seconds must be between 0 and 59")
		return 0
	}
	return hours*3600 + minutes*60 + seconds
}

// Prometheus format: 	metric_name{label1="val1",label2="val2"}
// Graphite format:  	metric_name;label1=val1;label2=val2
func (p4m *P4MonitorMetrics) formatLabels(mname string, labels []labelStruct) string {
	nonBlankLabels := make([]labelStruct, 0)
	for _, l := range labels {
		if l.value != "" {
			nonBlankLabels = append(nonBlankLabels, l)
		}
	}
	vals := make([]string, 0)
	for _, l := range nonBlankLabels {
		vals = append(vals, fmt.Sprintf("%s=\"%s\"", l.name, l.value))
	}
	labelStr := strings.Join(vals, ",")
	return fmt.Sprintf("%s{%s}", mname, labelStr)
}

func (p4m *P4MonitorMetrics) printMetricHeader(f io.Writer, name string, help string, metricType string) {
	fmt.Fprintf(f, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

func (p4m *P4MonitorMetrics) formatMetric(mname string, labels []labelStruct, metricVal string) string {
	return fmt.Sprintf("%s %s\n", p4m.formatLabels(mname, labels), metricVal)
}

func (p4m *P4MonitorMetrics) printMetric(metrics *bytes.Buffer, mname string, labels []labelStruct, metricVal string) {
	buf := p4m.formatMetric(mname, labels, metricVal)
	// node_exporter requires doubling of backslashes
	buf = strings.Replace(buf, `\`, "\\\\", -1)
	fmt.Fprint(metrics, buf)
}

func (p4m *P4MonitorMetrics) outputMetric(metrics *bytes.Buffer, mname string, mhelp string, mtype string, metricVal string, fixedLabels []labelStruct) {
	p4m.printMetricHeader(metrics, mname, mhelp, mtype)
	p4m.printMetric(metrics, mname, fixedLabels, metricVal)
}

func (p4m *P4MonitorMetrics) getCumulativeMetrics() string {
	fixedLabels := []labelStruct{{name: "serverid", value: p4m.serverID},
		{name: "sdpinst", value: p4m.sdpInstance}}
	metrics := new(bytes.Buffer)
	for _, m := range p4m.metrics {
		labels := fixedLabels
		if m.label.name != "" {
			labels = append(labels, m.label)
		}
		p4m.outputMetric(metrics, m.name, m.help, m.mtype, m.value, labels)
	}
	return metrics.String()
}

func (p4m *P4MonitorMetrics) deleteMetricsFile(filePrefix string) {
	outputFile := path.Join(p4m.config.MetricsRoot,
		fmt.Sprintf("%s-%s-%s.prom", filePrefix, p4m.config.SDPInstance, p4m.config.ServerID))
	p4m.metricsFilePrefix = filePrefix
	if err := os.Remove(outputFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		p4m.logger.Debugf("Failed to remove: %s, %v", outputFile, err)
	}
}

// Writes metrics to appropriate file - writes to temp file first and renames it after
func (p4m *P4MonitorMetrics) writeMetricsFile() {
	var f *os.File
	var err error
	outputFile := path.Join(p4m.config.MetricsRoot,
		fmt.Sprintf("%s-%s-%s.prom", p4m.metricsFilePrefix, p4m.config.SDPInstance, p4m.config.ServerID))
	if len(p4m.metrics) == 0 {
		p4m.logger.Debug("No metrics to write")
		return
	}
	p4m.logger.Debugf("Metrics: %q", p4m.metrics)
	tmpFile := outputFile + ".tmp"
	f, err = os.Create(tmpFile)
	if err != nil {
		p4m.logger.Errorf("Error opening %s: %v", tmpFile, err)
		return
	}

	f.Write(bytes.ToValidUTF8([]byte(p4m.getCumulativeMetrics()), []byte{'?'}))
	err = f.Close()
	if err != nil {
		p4m.logger.Errorf("Error closing file: %v", err)
	}
	err = os.Chmod(tmpFile, 0644)
	if err != nil {
		p4m.logger.Errorf("Error chmod-ing file: %v", err)
	}
	err = os.Rename(tmpFile, outputFile)
	if err != nil {
		p4m.logger.Errorf("Error renaming: %s to %s - %v", tmpFile, outputFile, err)
	}
}

func (p4m *P4MonitorMetrics) newP4CmdPipe(cmd string) (string, *bytes.Buffer, *script.Pipe) {
	errbuf := new(bytes.Buffer)
	p := script.NewPipe().WithStderr(errbuf)
	cmd = fmt.Sprintf("%s %s", p4m.p4Cmd, cmd)
	p4m.logger.Debugf("cmd: %s", cmd)
	return cmd, errbuf, p
}

func (p4m *P4MonitorMetrics) startMonitor(functionName, metricsFilePrefix string) {
	p4m.logger.Debugf("start: %s", functionName)
	p4m.metrics = make([]metricStruct, 0)
	p4m.deleteMetricsFile(metricsFilePrefix)
}

func (p4m *P4MonitorMetrics) monitorUptime() {
	// Server uptime as a simple seconds parameter - parsed from p4 info:
	// Server uptime: 168:39:20
	p4m.startMonitor("monitorUptime", "p4_uptime")
	k := "Server uptime"
	var seconds int64
	if v, ok := p4m.p4info[k]; ok {
		p4m.logger.Debugf("monitorUptime: parsing: %s", v)
		seconds = p4m.parseUptime(v)
	} else {
		p4m.logger.Debugf("monitorUptime: failed to find 'Server uptime' in p4 info")
		return
	}
	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_server_uptime",
			help:  "P4D Server uptime (seconds)",
			mtype: "counter",
			value: fmt.Sprintf("%d", seconds)})
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) parseLicense() {
	p4m.metrics = make([]metricStruct, 0)
	// Called by monitorLicense
	// Assume that p4m.p4license is already setup with data from p4 license -u
	// Server license expiry - parsed from "p4 license -u" - key fields:
	// ... userCount 893
	// ... userLimit 1000
	// ... licenseExpires 1677628800
	// ... licenseTimeRemaining 34431485
	// ... supportExpires 1677628800
	// Note that sometimes you only get supportExpires - we calculate licenseTimeRemaining in that case

	// Check for no license
	licenseInfoFull := ""
	licenseInfo := ""
	licenseIP := ""
	noLicense := false
	if v, ok := p4m.p4info["Server license"]; ok {
		licenseInfoFull = v
		if v == "none" {
			noLicense = true
		}
	}
	if v, ok := p4m.p4info["Server license-ip"]; ok {
		licenseIP = v
	}

	userCount := ""
	userLimit := ""
	licenseExpires := ""
	licenseTimeRemaining := ""
	supportExpires := ""
	if !noLicense {
		if v, ok := p4m.p4license["userCount"]; ok {
			userCount = v
		}
		if v, ok := p4m.p4license["userLimit"]; ok {
			userLimit = v
		}
		if v, ok := p4m.p4license["licenseExpires"]; ok {
			licenseExpires = v
		}
		if v, ok := p4m.p4license["licenseTimeRemaining"]; ok {
			licenseTimeRemaining = v
		}
		if v, ok := p4m.p4license["supportExpires"]; ok {
			supportExpires = v
		}
		reSupport := regexp.MustCompile(`\S*\(support [^\)]+\)`)
		reExpires := regexp.MustCompile(`\S*\(expires [^\)]+\)`)
		licenseInfo = reSupport.ReplaceAllString(licenseInfoFull, "")
		licenseInfo = reExpires.ReplaceAllString(licenseInfo, "")
		licenseInfo = strings.TrimSpace(licenseInfo)
		if licenseTimeRemaining == "" && supportExpires != "" {
			if v, ok := p4m.p4info["Server date"]; ok {
				expSecs, _ := strconv.ParseInt(supportExpires, 10, 64)
				expT := time.Unix(expSecs, 0)
				t, err := time.Parse(p4InfoTimeFormat, v)
				if err == nil {
					diff := expT.Sub(t)
					licenseTimeRemaining = fmt.Sprintf("%.0f", diff.Seconds())
				}
			}
		}
	}

	if userCount != "" {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_licensed_user_count",
				help:  "P4D Licensed User count",
				mtype: "gauge",
				value: userCount})
	}
	if userLimit != "" {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_licensed_user_limit",
				help:  "P4D Licensed User Limit",
				mtype: "gauge",
				value: userLimit})
	}
	if licenseExpires != "" {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_expires",
				help:  "P4D License expiry (epoch secs)",
				mtype: "gauge",
				value: licenseExpires})
	}
	if licenseTimeRemaining != "" {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_time_remaining",
				help:  "P4D License time remaining (secs)",
				mtype: "gauge",
				value: licenseTimeRemaining})
	}
	if supportExpires != "" {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_support_expires",
				help:  "P4D License support expiry (epoch secs)",
				mtype: "gauge",
				value: supportExpires})
	}
	if licenseInfo != "" { // Metric where the value is in the label not the series
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_info",
				help:  "P4D License info",
				mtype: "gauge",
				value: "1",
				label: labelStruct{name: "licenseInfo", value: licenseInfo}})
	}
	if licenseIP != "" { // Metric where the value is in the label not the series
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_IP",
				help:  "P4D Licensed IP",
				mtype: "gauge",
				value: "1",
				label: labelStruct{name: "licenseIP", value: licenseIP}})
	}
}

func (p4m *P4MonitorMetrics) monitorLicense() {
	// Server license expiry - parsed from "p4 license -u"
	p4m.startMonitor("monitorLicense", "p4_license")
	// Key fields:
	// ... userCount 893
	// ... userLimit 1000
	// ... licenseExpires 1677628800
	// ... licenseTimeRemaining 34431485
	// ... supportExpires 1677628800
	// Note that sometimes you only get supportExpires - we calculate licenseTimeRemaining in that case

	// TODO: only check license according to config update value
	p4cmd, errbuf, p := p4m.newP4CmdPipe("license -u")
	licenseMap, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	for _, s := range licenseMap {
		parts := strings.Split(s, " ")
		if len(parts) == 3 {
			p4m.p4license[parts[0]] = parts[1]
		}
	}
	p4m.parseLicense()
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) ConvertToBytes(size string) string {
	// Handle empty input
	if len(size) == 0 {
		p4m.logger.Error("empty input string")
		return "0"
	}
	// Find the numeric part and unit
	var numStr string
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
		p4m.logger.Errorf("invalid number format: %v", err)
		return "0"
	}
	// Convert based on unit
	var multiplier uint64
	switch unit {
	case "B":
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
		p4m.logger.Errorf("unsupported unit: %s", unit)
		return "0"
	}
	return fmt.Sprintf("%d", uint64(num*float64(multiplier)))
}

func (p4m *P4MonitorMetrics) parseFilesys(values []string) {
	configurables := strings.Split("filesys.depot.min filesys.P4ROOT.min filesys.P4JOURNAL.min filesys.P4LOG.min filesys.TEMP.min", " ")
	reConfig := regexp.MustCompile(`\S+=(\S+) \(\S+\)`)
	reLabel := regexp.MustCompile(`\S+\.(\S+)\.\S+`)
	for _, c := range configurables {
		filesysName := reLabel.ReplaceAllString(c, "$1")
		configuredValue := ""
		defaultValue := ""
		for _, v := range values {
			if strings.HasPrefix(v, c) {
				if strings.Contains(v, "(configure)") {
					configuredValue = reConfig.ReplaceAllString(v, "$1")
				} else if strings.Contains(v, "(default)") {
					defaultValue = reConfig.ReplaceAllString(v, "$1")
				}
			}
		}
		value := configuredValue
		if value == "" {
			value = defaultValue
		}
		if value != "" {
			m := metricStruct{name: "p4_filesys_min",
				help:  "Minimum space for filesystem",
				mtype: "gauge"}
			m.value = p4m.ConvertToBytes(value)
			m.label = labelStruct{name: "filesys", value: filesysName}
			p4m.metrics = append(p4m.metrics, m)
		}
	}
}

func (p4m *P4MonitorMetrics) monitorFilesys() {
	// Log current filesys.*.min settings
	p4m.logger.Debugf("monitorFilesys")
	p4m.metrics = make([]metricStruct, 0)
	// p4 configure show can give 2 values, or just the (default)
	//    filesys.P4ROOT.min=5G (configure)
	//    filesys.P4ROOT.min=250M (default)
	// fname="$metrics_root/p4_filesys${sdpinst_suffix}-${SERVER_ID}.prom"
	// TODO: check for update frequency
	// configurables=
	configurables := strings.Split("filesys.depot.min filesys.P4ROOT.min filesys.P4JOURNAL.min filesys.P4LOG.min filesys.TEMP.min", " ")
	configValues := make([]string, 0)
	for _, c := range configurables {
		p4cmd, errbuf, p := p4m.newP4CmdPipe(fmt.Sprintf("configure show %s", c))
		vals, err := p.Exec(p4cmd).Slice()
		if err != nil {
			p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		}
		configValues = append(configValues, vals...)
	}
	p4m.logger.Debugf("Filesys config values: %q", configValues)
	p4m.parseFilesys(configValues)
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) monitorVersions() {
	// P4D and SDP Versions
	p4m.startMonitor("monitorVersions", "p4_version_info")
	p4dVersion := "unknown"
	p4dServices := "unknown"
	reDate := regexp.MustCompile(` \([0-9/]+\)`)
	if v, ok := p4m.p4info["Server version"]; ok {
		p4dVersion = reDate.ReplaceAllString(v, "")
	}
	if v, ok := p4m.p4info["Server services"]; ok {
		p4dServices = v
	}

	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_p4d_build_info",
		help:  "P4D Version/build info",
		mtype: "gauge",
		value: "1",
		label: labelStruct{name: "version", value: p4dVersion}})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_p4d_server_type",
		help:  "P4D server type/services",
		mtype: "gauge",
		value: "1",
		label: labelStruct{name: "services", value: p4dServices}})

	if p4m.config.SDPInstance != "" {
		SDPVersion, err := script.File("/p4/sdp/Version").First(1).String()
		if err != nil {
			p4m.logger.Errorf("failed to read sdp version: %v", err)
		} else {
			SDPVersion = strings.TrimSpace(SDPVersion)
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_version",
				help:  "SDP Version",
				mtype: "gauge",
				value: "1",
				label: labelStruct{name: "version", value: SDPVersion}})
		}
	}
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) monitorSSL() {
	// P4D certificate expiry
	p4m.startMonitor("monitorSSL", "p4_ssl_info")
	certExpiry := ""
	if v, ok := p4m.p4info["Server cert expires"]; ok {
		certExpiry = v
	} else {
		return
	}
	// Parse the expiry date
	timeExpiry, err := time.Parse(opensslTimeFormat, certExpiry)
	if err != nil {
		p4m.logger.Errorf("failed to read parse sdp cert expiry: %v, %q", err, certExpiry)
		return
	}

	certExpirySecs := timeExpiry.Unix()
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_ssl_cert_expires",
		help:  "P4D SSL certificate expiry epoch seconds",
		mtype: "gauge",
		value: fmt.Sprintf("%d", certExpirySecs)})
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) extractServiceURL(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Service-URL:") {
			// Check if there's a next line available
			if i+1 < len(lines) {
				return strings.TrimSpace(lines[i+1])
			}
			return ""
		}
	}
	return ""
}

// GetCertificateExpiry takes a URL string and returns the expiration date
// of its SSL certificate. It returns the expiry time and any error encountered.
func (p4m *P4MonitorMetrics) getCertificateExpiry(certURL string) (time.Time, error) {
	// Parse the URL to ensure it's valid
	parsedURL, err := url.Parse(certURL)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme != "https" { // Ensure we're using HTTPS
		return time.Time{}, fmt.Errorf("URL must use HTTPS scheme")
	}

	// Create a custom transport to skip certificate verification
	// as we just want to inspect the certificate
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	client := &http.Client{
		Transport: transport,
		// Set a reasonable timeout
		Timeout: 30 * time.Second,
	}

	// Make a HEAD request to get the certificate
	resp, err := client.Head(certURL)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	// Get the TLS connection state
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return time.Time{}, fmt.Errorf("no TLS certificates found")
	}

	// Return the expiration date of the first (leaf) certificate
	return resp.TLS.PeerCertificates[0].NotAfter, nil
}

func (p4m *P4MonitorMetrics) monitorHASSSL() {
	// Check expiry of HAS SSL certificate - if it exists!
	p4m.startMonitor("monitorHASSSL", "p4_has_ssl_info")

	// # Update every 60 mins
	// tmp_has_ssl="$metrics_root/tmp_has_ssl"
	// [[ ! -f "$tmp_has_ssl" || $(find "$tmp_has_ssl" -mmin +60) ]] || return
	// TODO: update frequency
	p4cmd, errbuf, p := p4m.newP4CmdPipe("extension --list --type extensions")
	ext, err := p.Exec(p4cmd).Match("extension Auth::loginhook").Column(2).String()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	if ext == "" {
		return
	}

	// # Find URL - trimming spaces
	// certURL=$(p4 extension --configure Auth::loginhook -o | grep -A1 Service-URL | tail -1 | tr -d '[:space:]')
	// if [[ -z "$certURL" ]]; then
	//     return
	// fi
	p4cmd, errbuf, p = p4m.newP4CmdPipe("extension --configure Auth::loginhook -o")
	lines, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	certURL := p4m.extractServiceURL(lines)
	if !strings.HasPrefix(certURL, "https") {
		return
	}
	certExpiryTime, err := p4m.getCertificateExpiry(certURL)
	if err != nil {
		p4m.logger.Errorf("Error getting cert url expiry %s: %v", certURL, err)
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_has_ssl_cert_expires",
		help:  "P4D HAS SSL certificate expiry epoch seconds",
		mtype: "gauge",
		value: fmt.Sprintf("%d", certExpiryTime.Unix()),
		label: labelStruct{name: "url", value: certURL}})
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) monitorChange() {
	// Latest changelist counter as single counter value
	p4m.startMonitor("monitorChange", "p4_change")
	p4cmd, errbuf, p := p4m.newP4CmdPipe("counter change")
	change, err := p.Exec(p4cmd).String()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_change_counter",
		help:  "P4D change counter",
		mtype: "counter",
		value: strings.TrimSpace(change)})
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) getFieldCounts(lines []string, fieldNum int) map[string]int {
	// Extract the appropriate field from the lines
	fieldCounts := make(map[string]int)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < fieldNum { // Skip lines that don't have sufficient fields
			continue
		}
		fieldCounts[fields[fieldNum-1]]++
	}
	return fieldCounts
}

func (p4m *P4MonitorMetrics) monitorProcesses() {
	// Monitor metrics summarised by cmd or user
	p4m.startMonitor("monitorProcesses", "p4_monitor")
	p4cmd, errbuf, p := p4m.newP4CmdPipe("monitor show -l")
	monitorOutput, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	cmdCounts := p4m.getFieldCounts(monitorOutput, 5)
	for cmd, count := range cmdCounts {
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_monitor_by_cmd",
			help:  "P4 running processes by cmd in monitor table",
			mtype: "gauge",
			value: fmt.Sprintf("%d", count),
			label: labelStruct{name: "cmd", value: cmd}})
	}

	if p4m.config.CmdsByUser {
		userCounts := p4m.getFieldCounts(monitorOutput, 3)
		for user, count := range userCounts {
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_monitor_by_user",
				help:  "P4 running processes by user in monitor table",
				mtype: "gauge",
				value: fmt.Sprintf("%d", count),
				label: labelStruct{name: "user", value: user}})
		}
	}
	var proc string
	if p4m.config.SDPInstance != "" {
		proc = fmt.Sprintf("p4d_%s", p4m.config.SDPInstance)
	} else {
		proc = "p4d"
	}
	errbuf = new(bytes.Buffer)
	p = script.NewPipe().WithStderr(errbuf)
	pcount, err := p.Exec("ps ax").Match(proc + " ").CountLines()
	if err != nil {
		p4m.logger.Errorf("Error running 'ps ax': %v, err:%q", err, errbuf.String())
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_process_count",
		help:  "P4 ps running processes",
		mtype: "gauge",
		value: fmt.Sprintf("%d", pcount)})

	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) monitorCheckpoint() {
	// Metric for when SDP checkpoint last ran and how long it took.
	p4m.startMonitor("monitorCheckpoint", "p4_checkpoint")
	// Not as easy as it might first appear because:
	// - might be in progress
	// - multiple rotate_journal.sh may be run in between daily_checkpoint.sh - and they
	// both write to checkpoint.log!
	// The strings searched for have been present in SDP logs for years now...

	if p4m.config.SDPInstance == "" {
		p4m.logger.Debug("No SDP instance")
		return
	}
	sdpInstance := p4m.config.SDPInstance

	errbuf := new(bytes.Buffer)
	p := script.NewPipe().WithStderr(errbuf)
	// Look for latest checkpoint log which has Start/End (avoids run in progress and rotate_journal logs)
	cmd := fmt.Sprintf("find -L /p4/%s/logs -type f -name checkpoint.log* -exec ls -t {} +", sdpInstance)
	p4m.logger.Debugf("Executing: %s", cmd)
	files, err := p.Exec(cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running 'find': %v, err:%q", err, errbuf.String())
		return
	}
	if len(files) == 0 {
		p4m.logger.Warnf("No checkpoint.log files found")
		return
	}
	p4m.logger.Debugf("Checkpoint files to process: %q", files)

	ckpLog := ""
	reStartEnd := regexp.MustCompile(fmt.Sprintf("Start p4_%s Checkpoint|End p4_%s Checkpoint", sdpInstance, sdpInstance))
	var startEndLines []string
	for _, f := range files {
		startEndLines, err = script.File(f).MatchRegexp(reStartEnd).Slice()
		if len(startEndLines) == 2 && err == nil {
			ckpLog = f
			break
		}
	}
	if ckpLog == "" {
		p4m.logger.Debugf("Failed to find an appropriate checkpoint file")
		return
	}
	p4m.logger.Debugf("Found file: '%s'", ckpLog)
	fileInfo, err := os.Stat(ckpLog)
	if err != nil {
		p4m.logger.Errorf("error getting file info: %v", err)
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_checkpoint_log_time",
		help:  "Time of last checkpoint log",
		mtype: "gauge",
		value: fmt.Sprintf("%d", fileInfo.ModTime().Unix())})
	reRemoveNonDate := regexp.MustCompile(` \/p4.*`)
	startTimeStr := reRemoveNonDate.ReplaceAllString(startEndLines[0], "")
	endTimeStr := reRemoveNonDate.ReplaceAllString(startEndLines[1], "")
	startTime, err := time.Parse(checkpointTimeFormat, startTimeStr)
	if err != nil {
		p4m.logger.Errorf("error parsing date/time: %v, %s", err, startTimeStr)
		return
	}
	endTime, err := time.Parse(checkpointTimeFormat, endTimeStr)
	if err != nil {
		p4m.logger.Errorf("error parsing date/time: %v, %s", err, endTimeStr)
		return
	}
	diff := endTime.Sub(startTime)
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_checkpoint_duration",
		help:  "Time taken for last checkpoint/restore action",
		mtype: "gauge",
		value: fmt.Sprintf("%.0f", diff.Seconds())})
	p4m.writeMetricsFile()
}

// ServerInfo represents for servers/replicas
type ServerInfo struct {
	name     string
	services string
	journal  string
	offset   string
}

func (p4m *P4MonitorMetrics) monitorReplicas() {
	// Metric for server replicas
	p4m.startMonitor("monitorReplicas", "p4_replication")
	p4cmd, errbuf, p := p4m.newP4CmdPipe("-F \"%serverID% %type% %services%\" servers")
	reServices := regexp.MustCompile("standard|replica|commit-server|edge-server|forwarding-replica|build-server|standby|forwarding-standby")
	servers, err := p.Exec(p4cmd).MatchRegexp(reServices).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	p4m.logger.Debugf("servers: %q", servers)
	if len(servers) == 0 {
		return
	}
	validServers := make(map[string]*ServerInfo, 0)
	for _, line := range servers {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		validServers[fields[0]] = &ServerInfo{name: fields[0], services: fields[2]}
	}

	p4cmd, errbuf, p = p4m.newP4CmdPipe("-F \"%serverID% %appliedJnl% %appliedPos%\" servers -J")
	serverPositions, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	p4m.logger.Debugf("serverPositions: %q", serverPositions)
	for _, line := range serverPositions {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		validServers[fields[0]].journal = fields[1]
		validServers[fields[0]].offset = fields[2]
	}

	for _, s := range validServers {
		if s.journal != "" {
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_replica_curr_jnl",
				help:  "Current journal for server",
				mtype: "gauge",
				value: s.journal,
				label: labelStruct{name: "servername", value: s.name}})
		}
		if s.offset != "" {
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_replica_curr_pos",
				help:  "Current offset within for server",
				mtype: "gauge",
				value: s.offset,
				label: labelStruct{name: "servername", value: s.name}})
		}
	}
	p4m.writeMetricsFile()
}

// monitor_errors () {
//     # Metric for error counts - but only if structured error log exists
//     fname="$metrics_root/p4_errors${sdpinst_suffix}-${SERVER_ID}.prom"
//     tmpfname="$fname.$$"

//     rm -f "$fname"
//     [[ -f "$errors_file" ]] || return

//     declare -A subsystems=([0]=OS [1]=SUPP [2]=LBR [3]=RPC [4]=DB [5]=DBSUPP [6]=DM [7]=SERVER [8]=CLIENT \
//     [9]=INFO [10]=HELP [11]=SPEC [12]=FTPD [13]=BROKER [14]=P4QT [15]=X3SERVER [16]=GRAPH [17]=SCRIPT \
//     [18]=SERVER2 [19]=DM2)

//     # Log format differs according to p4d versions - first column - abort if file is empty
//     ver=$(head -1 "$errors_file" | awk -F, '{print $1}')
//     [[ -z "$ver" ]] && return

//     # Output of logschema is:
//     # ... f_field 16
//     # ... f_name f_severity
//     line=$($p4 logschema "$ver" | grep -B1 f_severity | head -1)
//     if ! [[ $line =~ f_field ]]; then
//         touch "$fname"
//         return
//     fi
//     indSeverity=$(echo "$line" | awk '{print $3}')
//     indSeverity=$((indSeverity+1))    # 0->1 index
//     indSS=$((indSeverity+1))
//     indID=$((indSS+1))

//     rm -f "$tmpfname"
//     echo "#HELP p4_error_count Server errors by id" >> "$tmpfname"
//     echo "#TYPE p4_error_count counter" >> "$tmpfname"
//     while read count level ss_id error_id
//     do
//         if [[ ! -z ${ss_id:-} ]]; then
//             subsystem=${subsystems[$ss_id]}
//             [[ -z "$subsystem" ]] && subsystem=$ss_id
//             echo "p4_error_count{${serverid_label}${sdpinst_label},subsystem=\"$subsystem\",error_id=\"$error_id\",level=\"$level\"} $count" >> "$tmpfname"
//         fi
//     done < <(awk -F, -v indID="$indID" -v indSS="$indSS" -v indSeverity="$indSeverity" '{printf "%s %s %s\n", $indID, $indSS, $indSeverity}' "$errors_file" | sort | uniq -c)

//     chmod 644 "$tmpfname"
//     mv "$tmpfname" "$fname"
// }

func (p4m *P4MonitorMetrics) monitorPull() {
	// p4 pull metrics - only valid for replica servers
	p4m.startMonitor("monitorPull", "p4_pull")
	if getVar(*p4m.env, "P4REPLICA") != "TRUE" {
		p4m.logger.Debugf("Exiting as not a replica")
		return
	}

	// TODO: use pull -ls
	tempPullQ := path.Join(p4m.config.MetricsRoot, "pullq.out")
	p4cmd, errbuf, p := p4m.newP4CmdPipe("pull -l")
	_, err := p.Exec(p4cmd).WriteFile(tempPullQ)
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}

	reFailed := regexp.MustCompile(`failed\.$`)
	failedCount, err := script.File(tempPullQ).MatchRegexp(reFailed).CountLines()
	if err != nil {
		p4m.logger.Errorf("Error counting failed pull errors: %v", err)
		return
	}
	otherCount, err := script.File(tempPullQ).RejectRegexp(reFailed).CountLines()
	if err != nil {
		p4m.logger.Errorf("Error counting pull queue: %v", err)
		return
	}

	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_errors",
		help:  "Count of p4 pull transfers in failed state",
		mtype: "gauge",
		value: fmt.Sprintf("%d", failedCount)})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_queue",
		help:  "Count of p4 pull files (not in failed state)",
		mtype: "gauge",
		value: fmt.Sprintf("%d", otherCount)})

	// Various possible results from p4 pull
	// $ p4 pull -lj
	// Current replica journal state is:       Journal 1237,  Sequence 2680510310.
	// Current master journal state is:        Journal 1237,  Sequence 2680510310.
	// The statefile was last modified at:     2022/03/29 14:15:16.
	// The replica server time is currently:   2022/03/29 14:15:18 +0000 GMT

	// $ p4 pull -lj
	// Perforce password (P4PASSWD) invalid or unset.
	// Perforce password (P4PASSWD) invalid or unset.
	// Current replica journal state is:       Journal 1237,  Sequence 2568249374.
	// Current master journal state is:        Journal 1237,  Sequence -1.
	// Current master journal state is:        Journal 0,      Sequence -1.
	// The statefile was last modified at:     2022/03/29 13:05:46.
	// The replica server time is currently:   2022/03/29 14:13:21 +0000 GMT

	// perforce@gemini20:/p4 p4 -Ztag pull -ljv
	// ... replicaJournalCounter 17823
	// ... replicaJournalNumber 17823
	// ... replicaJournalSequence 706254077
	// ... replicaStatefileModified 1738761870
	// ... replicaTime 1738761870
	// ... masterJournalNumber 17823
	// ... masterJournalSequence 706269139
	// ... masterJournalNumberLEOF 17823
	// ... masterJournalSequenceLEOF 706269139
	// ... journalBytesBehind 15062
	// ... journalRotationsBehind 0

	// perforce@gemini20:/p4 p4 -Ztag pull -ljv
	// Perforce password (P4PASSWD) invalid or unset.
	// Perforce password (P4PASSWD) invalid or unset.
	// ... replicaJournalCounter 12671
	// ... replicaJournalNumber 12671
	// ... replicaJournalSequence 17984845
	// ... replicaStatefileModified 1664718313
	// ... replicaTime 1664718339
	// ... masterJournalNumber 12671
	// ... masterJournalSequence -1
	// ... currentJournalNumber 0
	// ... currentJournalSequence -1
	// ... masterJournalNumberLEOF 12671
	// ... masterJournalSequenceLEOF -1
	// ... currentJournalNumberLEOF 0
	// ... currentJournalSequenceLEOF -1

	//     tmp_pull_stats="$metrics_root/pull-ljv.out"
	//     $p4 -Ztag pull -lj > "$tmp_pull_stats" 2> /dev/null

	tempPullStats := path.Join(p4m.config.MetricsRoot, "pull-ljv.out")
	p4cmd, errbuf, p = p4m.newP4CmdPipe("-Ztag pull -ljv")
	_, err = p.Exec(p4cmd).WriteFile(tempPullStats)
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}

	journalRotationsBehind, err := script.File(tempPullStats).Match("... journalRotationsBehind").Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error getting journalRotationsBehind %v", err)
		return
	}
	if journalRotationsBehind == "" {
		journalRotationsBehind = "-1"
	}
	journalBytesBehind, err := script.File(tempPullStats).Match("... journalBytesBehind").Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error getting journalBytesBehind %v", err)
		return
	}
	if journalBytesBehind == "" {
		journalBytesBehind = "-1"
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_replica_journals_behind",
		help:  "Count of how many journals behind replica is",
		mtype: "gauge",
		value: journalRotationsBehind})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_replica_bytes_behind",
		help:  "Count of how many bytes behind replica is",
		mtype: "gauge",
		value: journalBytesBehind})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_replica_lag",
		help:  "Count of how many bytes behind replica is",
		mtype: "gauge",
		value: journalBytesBehind})

	masterJournalSequence, err := script.File(tempPullStats).Match("... masterJournalSequence").Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error getting masterJournalSequence %v", err)
		return
	}
	replicationError := "0"
	if masterJournalSequence == "-1" {
		replicationError = "1"
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_replication_error",
		help:  "Set to 1 if replication error is true",
		mtype: "gauge",
		value: replicationError})

	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) monitorRealTime() {
	// p4d --show-realtime - only for 2021.1 or greater
	p4m.startMonitor("monitorRealTime", "p4_realtime")

	// Intially only available for SDP
	if p4m.config.SDPInstance == "" {
		return
	}

	p4dbin := getVar(*p4m.env, "P4DBIN")
	if p4dbin == "" {
		return
	}
	errbuf := new(bytes.Buffer)
	p := script.NewPipe().WithStderr(errbuf)
	p4dVersion, err := p.Exec(fmt.Sprintf("%s -V", p4dbin)).Match("Rev.").String()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4dbin, err, errbuf.String())
		return
	}
	parts := strings.Split(p4dVersion, "/")
	if len(parts) >= 3 {
		v := parts[2]
		if strings.Compare("2020.0", v) >= 0 {
			p4m.logger.Debugf("P4D Version < 2020.0: %s", v)
			return
		}
	}

	errbuf = new(bytes.Buffer)
	p = script.NewPipe().WithStderr(errbuf)
	cmd := fmt.Sprintf("%s --show-realtime", p4dbin)
	lines, err := p.Exec(cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", cmd, err, errbuf.String())
		return
	}
	p4m.logger.Debugf("Realtime values: %q", lines)
	// File format:
	// rtv.db.lockwait (flags 0) 0 max 382
	// rtv.db.ckp.active (flags 0) 0
	// rtv.db.ckp.records (flags 0) 34 max 34
	// rtv.db.io.records (flags 0) 126389592854
	// rtv.rpl.behind.bytes (flags 0) 0 max -1
	// rtv.rpl.behind.journals (flags 0) 0 max -1
	// rtv.svr.sessions.active (flags 0) 110 max 585
	// rtv.svr.sessions.total (flags 0) 5997080
	for _, line := range lines {
		if !strings.HasPrefix(line, "rtv.") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := "p4_" + strings.ReplaceAll(fields[0], ".", "_")
		p4m.metrics = append(p4m.metrics, metricStruct{name: name,
			help:  fmt.Sprintf("P4 realtime metric %s", fields[0]),
			mtype: "gauge",
			value: fields[3]})
	}
	p4m.writeMetricsFile()
}

// TaskResponse represents the structure of the Swarm JSON response, excluding pingError
type TaskResponse struct {
	Authorized     bool   // Whether successfully authorized or not
	Tasks          int    `json:"tasks"`
	FutureTasks    int    `json:"futureTasks"`
	Workers        int    `json:"workers"`
	MaxWorkers     int    `json:"maxWorkers"`
	WorkerLifetime string `json:"workerLifetime"` // We're not particularly interested in this one
}

// getSwarmQueueInfo performs an HTTP request and parses the JSON response
func (p4m *P4MonitorMetrics) getSwarmQueueInfo(url, userid, password string) (*TaskResponse, error) {
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	p4m.logger.Debugf("SetBasicAuth: '%s/%s'", userid, password)
	req.SetBasicAuth(userid, password)
	client := &http.Client{}
	p4m.logger.Debugf("req: %v", req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()
	p4m.logger.Debugf("Response: %v", resp)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return &TaskResponse{Authorized: false}, nil
		} else {
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	// Parse the JSON response
	var response TaskResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v", err)
	}
	response.Authorized = true
	return &response, nil
}

func (p4m *P4MonitorMetrics) monitorSwarm() {
	// Find Swarm URL and get information from it
	p4m.startMonitor("monitorSwarm", "p4_swarm")

	p4cmd, errbuf, p := p4m.newP4CmdPipe("-ztag info -s")
	authID, err := p.Exec(p4cmd).Match("... serverCluster").Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	authID = strings.TrimSpace(authID)

	// Use authID to find ticket value
	// localhost:auth.cust (fred) 492F0A7EEF5F7DA68A305274ASDF
	search := fmt.Sprintf("localhost:%s (%s)", authID, p4m.p4User)
	p4m.logger.Debugf("search: %s", search)
	p4cmd, errbuf, p = p4m.newP4CmdPipe("tickets")
	ticket, err := p.Exec(p4cmd).Match(search).Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	ticket = strings.TrimSpace(ticket)
	p4m.logger.Debugf("ticket: '%s'", ticket)

	// Get Swarm URL from property
	p4cmd, errbuf, p = p4m.newP4CmdPipe("property -l")
	url, err := p.Exec(p4cmd).Match("P4.Swarm.URL").Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	if url == "" {
		p4m.logger.Warningf("No Swarm property")
		return
	}
	url = fmt.Sprintf("%s/queue/status", strings.TrimSpace(url))
	p4m.logger.Debugf("url: '%s'", url)

	swarmerror := "0"
	swarminfo, err := p4m.getSwarmQueueInfo(url, p4m.p4User, ticket)
	if err != nil {
		swarmerror = "1"
		p4m.logger.Errorf("error: %v", err)
	}
	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_swarm_error",
			help:  "Swarm error (0=no or 1=yes)",
			mtype: "gauge",
			value: swarmerror})
	m := metricStruct{name: "p4_swarm_authorized",
		help:  "Swarm API call authorized (1=yes or 0=no)",
		mtype: "gauge",
		value: "1"}
	if !swarminfo.Authorized {
		m.value = "0"
		p4m.metrics = append(p4m.metrics, m)
		return
	} else {
		p4m.metrics = append(p4m.metrics, m)
	}
	if err != nil {
		return
	}

	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_swarm_tasks",
			help:  "Swarm current task queue size",
			mtype: "gauge",
			value: fmt.Sprintf("%d", swarminfo.Tasks)})
	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_swarm_future_tasks",
			help:  "Swarm future task queue size",
			mtype: "gauge",
			value: fmt.Sprintf("%d", swarminfo.FutureTasks)})
	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_swarm_workers",
			help:  "Swarm current number of workers",
			mtype: "gauge",
			value: fmt.Sprintf("%d", swarminfo.Workers)})
	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_swarm_max_workers",
			help:  "Swarm current max number of workers",
			mtype: "gauge",
			value: fmt.Sprintf("%d", swarminfo.MaxWorkers)})
	p4m.writeMetricsFile()
}

// Reads server id for SDP instance or the server.id path
func readServerID(logger *logrus.Logger, instance string, path string) string {
	idfile := path
	if idfile == "" {
		idfile = fmt.Sprintf("/p4/%s/root/server.id", instance)
	}
	if _, err := os.Stat(idfile); err == nil {
		buf, err := os.ReadFile(idfile) // just pass the file name
		if err != nil {
			logger.Errorf("Failed to read %v - %v", idfile, err)
			return ""
		}
		return string(bytes.TrimRight(buf, " \r\n"))
	}
	return ""
}

func main() {
	var (
		configfile = kingpin.Flag(
			"config",
			"Config file for p4prometheus.",
		).Default("p4metrics.yaml").String()
		sdpInstance = kingpin.Flag(
			"sdp.instance",
			"SDP Instance, typically 1 or alphanumeric.",
		).Default("").String()
		// p4port = kingpin.Flag(
		// 	"p4port",
		// 	"P4PORT to use (if sdp.instance is not set).",
		// ).Default("").String()
		// p4user = kingpin.Flag(
		// 	"p4user",
		// 	"P4USER to use (if sdp.instance is not set).",
		// ).Default("").String()
		debug = kingpin.Flag(
			"debug",
			"Enable debugging.",
		).Bool()
	)

	kingpin.Version(version.Print("p4metrics"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := logrus.New()
	logger.Level = logrus.InfoLevel
	if *debug {
		logger.Level = logrus.DebugLevel
	}

	cfg, err := config.LoadConfigFile(*configfile)
	if err != nil {
		logger.Errorf("error loading config file: %v", err)
		os.Exit(-1)
	}
	if len(*sdpInstance) > 0 {
		cfg.SDPInstance = *sdpInstance
	}

	logger.Infof("%v", version.Print("p4metrics"))
	logger.Infof("Processing: output to '%s' SDP instance '%s'",
		cfg.MetricsRoot, cfg.SDPInstance)

	if cfg.SDPInstance == "" && len(cfg.ServerID) == 0 && cfg.ServerIDPath == "" {
		logger.Errorf("error loading config file - if no sdp_instance then please specify server_id or server_id_path!")
		os.Exit(-1)
	}
	if len(cfg.ServerID) == 0 && (cfg.SDPInstance != "" || cfg.ServerIDPath != "") {
		cfg.ServerID = readServerID(logger, cfg.SDPInstance, cfg.ServerIDPath)
	}
	logger.Infof("Server id: '%s'", cfg.ServerID)

	var env map[string]string
	if cfg.SDPInstance != "" {
		env = sourceSDPVars(*sdpInstance, logger)
	} else {
		env = sourceEnvVars()
	}
	p4m := newP4MonitorMetrics(cfg, &env, logger)
	p4m.initVars()
	if cfg.MonitorSwarm {
		p4m.monitorSwarm()
	}
	p4m.monitorUptime()
	p4m.monitorChange()
	p4m.monitorCheckpoint()
	p4m.monitorFilesys()
	p4m.monitorHASSSL()
	p4m.monitorLicense()
	p4m.monitorProcesses()
	p4m.monitorReplicas()
	p4m.monitorSSL()
	p4m.monitorPull()
	p4m.monitorRealTime()
	p4m.monitorVersions()
}
