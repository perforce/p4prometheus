// This is a version of monitor_metrics.sh in Go as part of p4prometheus
// It is intended to be more reliable and cross platform than the original.
package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
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
const p4infotimeformat = "2006/01/02 15:04:05 -0700 MST"
const openssltimeformat = "Jan 2 15:04:05 2006 MST"

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
		p4m.outputMetric(metrics, m.name, m.help, m.mtype, m.value, fixedLabels)
	}
	return metrics.String()
}

// Writes metrics to appropriate file - writes to temp file first and renames it after
func (p4m *P4MonitorMetrics) writeMetricsFile(metrics []byte) {
	var f *os.File
	var err error
	tmpFile := p4m.config.MetricsOutput + ".tmp"
	f, err = os.Create(tmpFile)
	if err != nil {
		p4m.logger.Errorf("Error opening %s: %v", tmpFile, err)
		return
	}
	f.Write(bytes.ToValidUTF8(metrics, []byte{'?'}))
	err = f.Close()
	if err != nil {
		p4m.logger.Errorf("Error closing file: %v", err)
	}
	err = os.Chmod(tmpFile, 0644)
	if err != nil {
		p4m.logger.Errorf("Error chmod-ing file: %v", err)
	}
	err = os.Rename(tmpFile, p4m.config.MetricsOutput)
	if err != nil {
		p4m.logger.Errorf("Error renaming: %s to %s - %v", tmpFile, p4m.config.MetricsOutput, err)
	}
}

func (p4m *P4MonitorMetrics) newP4CmdPipe(cmd string) (string, *bytes.Buffer, *script.Pipe) {
	errbuf := new(bytes.Buffer)
	p := script.NewPipe().WithStderr(errbuf)
	cmd = fmt.Sprintf("%s %s", p4m.p4Cmd, cmd)
	p4m.logger.Debugf("cmd: %s", cmd)
	return cmd, errbuf, p
}

func (p4m *P4MonitorMetrics) monitorUptime() {
	p4m.logger.Debugf("monitorUptime")
	// Server uptime as a simple seconds parameter - parsed from p4 info:
	// Server uptime: 168:39:20
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
}

func (p4m *P4MonitorMetrics) parseLicense() {
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
				t, err := time.Parse(p4infotimeformat, v)
				if err == nil {
					diff := expT.Sub(t)
					licenseTimeRemaining = fmt.Sprintf("%.0f", diff.Seconds())
				}
			}
		}
	}

	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_licensed_user_count",
			help:  "P4D Licensed User count",
			mtype: "gauge",
			value: userCount})
	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_licensed_user_limit",
			help:  "P4D Licensed User Limit",
			mtype: "gauge",
			value: userLimit})
	if licenseExpires != "" {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_expires",
				help:  "P4D License expiry (epoch secs)",
				mtype: "gauge",
				value: licenseExpires})
	}
	p4m.metrics = append(p4m.metrics,
		metricStruct{name: "p4_license_time_remaining",
			help:  "P4D License time remaining (secs)",
			mtype: "gauge",
			value: licenseTimeRemaining})
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
	p4m.logger.Debugf("monitorLicense")
	// Server license expiry - parsed from "p4 license -u" - key fields:
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
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	for _, s := range licenseMap {
		parts := strings.Split(s, " ")
		if len(parts) == 3 {
			p4m.p4license[parts[0]] = parts[1]
		}
	}
	p4m.parseLicense()
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
	p4m.logger.Debugf("monitorFilesys")
	// Log current filesys.*.min settings
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
			logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		}
		for _, v := range vals {
			configValues = append(configValues, v)
		}
	}
	p4m.parseFilesys(configValues)
}

func (p4m *P4MonitorMetrics) monitorVersions() {
	p4m.logger.Debugf("monitorVersions")
	// P4D and SDP Versions
	// fname="$metrics_root/p4_version_info${sdpinst_suffix}-${SERVER_ID}.prom"

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
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_version",
				help:  "SDP Version",
				mtype: "gauge",
				value: "1",
				label: labelStruct{name: "version", value: SDPVersion}})
		}
	}
}

func (p4m *P4MonitorMetrics) monitorSSL() {
	p4m.logger.Debugf("monitorSSL")
	// P4D certificate expiry
	// fname="$metrics_root/p4_ssl_info${sdpinst_suffix}-${SERVER_ID}.prom"
	certExpiry := ""
	if v, ok := p4m.p4info["Server cert expires"]; ok {
		certExpiry = v
	} else {
		return
	}
	// Parse the expiry date
	timeExpiry, err := time.Parse(openssltimeformat, certExpiry)
	if err != nil {
		p4m.logger.Errorf("failed to read parse sdp cert expiry: %v, %q", err, certExpiry)
		return
	}

	certExpirySecs := timeExpiry.Unix()
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_ssl_cert_expires",
		help:  "P4D SSL certificate expiry epoch seconds",
		mtype: "gauge",
		value: fmt.Sprintf("%d", certExpirySecs)})
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
	p4m.logger.Debugf("monitorHASSSL")
	// Check expiry of HAS SSL certificate - if it exists!
	// fname="$metrics_root/p4_has_ssl_info${sdpinst_suffix}-${SERVER_ID}.prom"

	// # Update every 60 mins
	// tmp_has_ssl="$metrics_root/tmp_has_ssl"
	// [[ ! -f "$tmp_has_ssl" || $(find "$tmp_has_ssl" -mmin +60) ]] || return

	// extExists=$(p4 extension --list --type extensions | grep 'extension Auth::loginhook')
	// if [[ -z "$extExists" ]]; then
	//     return
	// fi
	p4cmd, errbuf, p := p4m.newP4CmdPipe("extension --list --type extensions")
	ext, err := p.Exec(p4cmd).Match("extension Auth::loginhook").Column(2).String()
	if err != nil {
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
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
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	certURL := p4m.extractServiceURL(lines)
	if !strings.HasPrefix(certURL, "https") {
		return
	}
	certExpiryTime, err := p4m.getCertificateExpiry(certURL)
	if err != nil {
		logger.Errorf("Error getting cert url expiry %s: %v", certURL, err)
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_has_ssl_cert_expires",
		help:  "P4D HAS SSL certificate expiry epoch seconds",
		mtype: "gauge",
		value: fmt.Sprintf("%d", certExpiryTime.Unix()),
		label: labelStruct{name: "url", value: certURL}})
}

func (p4m *P4MonitorMetrics) monitorChange() {
	p4m.logger.Debugf("monitorChange")
	// Latest changelist counter as single counter value
	// fname="$metrics_root/p4_change${sdpinst_suffix}-${SERVER_ID}.prom"
	p4cmd, errbuf, p := p4m.newP4CmdPipe("counter change")
	change, err := p.Exec(p4cmd).String()
	if err != nil {
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_change_counter",
		help:  "P4D change counter",
		mtype: "counter",
		value: change})
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
	p4m.logger.Debugf("monitorProcesses")
	// Monitor metrics summarised by cmd or user
	// fname="$metrics_root/p4_monitor${sdpinst_suffix}-${SERVER_ID}.prom"
	p4cmd, errbuf, p := p4m.newP4CmdPipe("monitor show -l")
	monitorOutput, err := p.Exec(p4cmd).Slice()
	if err != nil {
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
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
		logger.Errorf("Error running 'ps ax': %v, err:%q", err, errbuf.String())
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_process_count",
		help:  "P4 ps running processes",
		mtype: "gauge",
		value: fmt.Sprintf("%d", pcount)})
}

func (p4m *P4MonitorMetrics) monitorCheckpoint() {
	p4m.logger.Debugf("monitorCheckpoint")
	// Metric for when SDP checkpoint last ran and how long it took.
	// Not as easy as it might first appear because:
	// - might be in progress
	// - multiple rotate_journal.sh may be run in between daily_checkpoint.sh - and they
	// both write to checkpoint.log!
	// The strings searched for have been present in SDP logs for years now...

	if p4m.config.SDPInstance != "" {
		return
	}
	sdpInstance := p4m.config.SDPInstance
	// fname="$metrics_root/p4_checkpoint${sdpinst_suffix}-${SERVER_ID}.prom"

	errbuf := new(bytes.Buffer)
	p := script.NewPipe().WithStderr(errbuf)
	// Look for latest checkpoint log which has Start/End (avoids run in progress and rotate_journal logs)
	cmd := fmt.Sprintf("find -L /p4/%s/logs -type f -name checkpoint.log* -exec ls -t {} +", sdpInstance)
	files, err := p.Exec(cmd).Slice()
	if err != nil {
		logger.Errorf("Error running 'find': %v, err:%q", err, errbuf.String())
		return
	}

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
		p4m.logger.Debugf("Failed to find appropriate checkpoint file")
		return
	}
	fileInfo, err := os.Stat(ckpLog)
	if err != nil {
		p4m.logger.Errorf("error getting file info: %v", err)
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_checkpoint_log_time",
		help:  "Time of last checkpoint log",
		mtype: "gauge",
		value: fmt.Sprintf("%d", fileInfo.ModTime().Unix())})
	//TODO:
	// ckpDuration := 0
	reRemoveNonDate := regexp.MustCompile(` \/p4.*`)
	startTimeStr := reRemoveNonDate.ReplaceAllString(startEndLines[0], "")
	endTimeStr := reRemoveNonDate.ReplaceAllString(startEndLines[1], "")
	startTime, err := time.Parse(p4infotimeformat, startTimeStr)
	if err != nil {
		p4m.logger.Errorf("error parsing date/time: %v, %s", err, startTimeStr)
		return
	}
	endTime, err := time.Parse(p4infotimeformat, endTimeStr)
	if err != nil {
		p4m.logger.Errorf("error parsing date/time: %v, %s", err, endTimeStr)
		return
	}
	diff := endTime.Sub(startTime)
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_checkpoint_duration",
		help:  "Time taken for last checkpoint/restore action",
		mtype: "gauge",
		value: fmt.Sprintf("%.0f", diff.Seconds())})
}

// ServerInfo represents for servers/replicas
type ServerInfo struct {
	name     string
	services string
	journal  string
	offset   string
}

func (p4m *P4MonitorMetrics) monitorReplicas() {
	p4m.logger.Debugf("monitorReplicas")
	// Metric for server replicas
	// fname = "$metrics_root/p4_replication${sdpinst_suffix}-${SERVER_ID}.prom"

	p4cmd, errbuf, p := p4m.newP4CmdPipe("-F \"%serverID% %type% %services%\" servers")
	reServices := regexp.MustCompile("standard|replica|commit-server|edge-server|forwarding-replica|build-server|standby|forwarding-standby")
	servers, err := p.Exec(p4cmd).MatchRegexp(reServices).Slice()
	if err != nil {
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
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
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
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
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_replica_curr_jnl",
			help:  "Current journal for server",
			mtype: "gauge",
			value: s.journal,
			label: labelStruct{name: "servername", value: s.name}})
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_replica_curr_pos",
			help:  "Current offset within for server",
			mtype: "gauge",
			value: s.offset,
			label: labelStruct{name: "servername", value: s.name}})
	}
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

// monitor_pull () {
//     # p4 pull metrics - only valid for replica servers
//     [[ "${P4REPLICA}" == "TRUE" ]] || return

//     fname="$metrics_root/p4_pull${sdpinst_suffix}-${SERVER_ID}.prom"
//     tmpfname="$fname.$$"
//     tmp_pull_queue="$metrics_root/pullq.out"
//     $p4 pull -l > "$tmp_pull_queue" 2> /dev/null
//     rm -f "$tmpfname"

//     count=$(grep -cEa "failed\.$" "$tmp_pull_queue")
//     {
//         echo "# HELP p4_pull_errors P4 pull transfers failed count"
//         echo "# TYPE p4_pull_errors counter"
//         echo "p4_pull_errors{${serverid_label}${sdpinst_label}} $count"
//     } >> "$tmpfname"

//     count=$(grep -cvEa "failed\.$" "$tmp_pull_queue")
//     {
//         echo "# HELP p4_pull_queue P4 pull files in queue count"
//         echo "# TYPE p4_pull_queue counter"
//         echo "p4_pull_queue{${serverid_label}${sdpinst_label}} $count"
//     } >> "$tmpfname"

// #     $ p4 pull -lj
// #     Current replica journal state is:       Journal 1237,  Sequence 2680510310.
// #     Current master journal state is:        Journal 1237,  Sequence 2680510310.
// #     The statefile was last modified at:     2022/03/29 14:15:16.
// #     The replica server time is currently:   2022/03/29 14:15:18 +0000 GMT

// #     $ p4 pull -lj
// #     Perforce password (P4PASSWD) invalid or unset.
// #     Perforce password (P4PASSWD) invalid or unset.
// #     Current replica journal state is:       Journal 1237,  Sequence 2568249374.
// #     Current master journal state is:        Journal 1237,  Sequence -1.
// #     Current master journal state is:        Journal 0,      Sequence -1.
// #     The statefile was last modified at:     2022/03/29 13:05:46.
// #     The replica server time is currently:   2022/03/29 14:13:21 +0000 GMT

// # perforce@gemini20:/p4 p4 -Ztag pull -lj
// # ... replicaJournalCounter 12671
// # ... replicaJournalNumber 12671
// # ... replicaJournalSequence 17984845
// # ... replicaStatefileModified 1664718313
// # ... replicaTime 1664718339
// # ... masterJournalNumber 12671
// # ... masterJournalSequence 17985009
// # ... masterJournalNumberLEOF 12671
// # ... masterJournalSequenceLEOF 17984845

// # perforce@gemini20:/p4 p4 -Ztag pull -lj
// # Perforce password (P4PASSWD) invalid or unset.
// # Perforce password (P4PASSWD) invalid or unset.
// # ... replicaJournalCounter 12671
// # ... replicaJournalNumber 12671
// # ... replicaJournalSequence 17984845
// # ... replicaStatefileModified 1664718313
// # ... replicaTime 1664718339
// # ... masterJournalNumber 12671
// # ... masterJournalSequence -1
// # ... currentJournalNumber 0
// # ... currentJournalSequence -1
// # ... masterJournalNumberLEOF 12671
// # ... masterJournalSequenceLEOF -1
// # ... currentJournalNumberLEOF 0
// # ... currentJournalSequenceLEOF -1

//     tmp_pull_stats="$metrics_root/pull-lj.out"
//     $p4 -Ztag pull -lj > "$tmp_pull_stats" 2> /dev/null

//     replica_jnl_file=$(grep "replicaJournalCounter " "$tmp_pull_stats" | awk '{print $3}' )
//     master_jnl_file=$(grep "masterJournalNumber " "$tmp_pull_stats" | awk '{print $3}' )
//     journals_behind=$((master_jnl_file - replica_jnl_file))

//     {
//         echo "# HELP p4_pull_replica_journals_behind Count of how many journals behind replica is"
//         echo "# TYPE p4_pull_replica_journals_behind gauge"
//         echo "p4_pull_replica_journals_behind{${serverid_label}${sdpinst_label}} $journals_behind"
//     }  >> "$tmpfname"

//     replica_jnl_seq=$(grep "replicaJournalSequence " "$tmp_pull_stats" | awk '{print $3}' )
//     master_jnl_seq=$(grep "masterJournalSequence " "$tmp_pull_stats" | awk '{print $3}' )
//     {
//         echo "# HELP p4_pull_replication_error Set to 1 if replication error"
//         echo "# TYPE p4_pull_replication_error gauge"
//     } >>  "$tmpfname"
//     if [[ $master_jnl_seq -lt 0 ]]; then
//         echo "p4_pull_replication_error{${serverid_label}${sdpinst_label}} 1" >> "$tmpfname"
//     else
//         echo "p4_pull_replication_error{${serverid_label}${sdpinst_label}} 0" >> "$tmpfname"
//     fi

//     {
//         echo "# HELP p4_pull_replica_lag Replica lag count (bytes)"
//         echo "# TYPE p4_pull_replica_lag gauge"
//     } >>  "$tmpfname"

//     if [[ $master_jnl_seq -lt 0 ]]; then
//         echo "p4_pull_replica_lag{${serverid_label}${sdpinst_label}} -1" >> "$tmpfname"
//     else
//         # Ensure base 10 arithmetic used to avoid overflow errors
//         lag_count=$((10#$master_jnl_seq - 10#$replica_jnl_seq))
//         echo "p4_pull_replica_lag{${serverid_label}${sdpinst_label}} $lag_count" >> "$tmpfname"
//     fi

//     chmod 644 "$tmpfname"
//     mv "$tmpfname" "$fname"
// }

// monitor_realtime () {
//     # p4d --show-realtime - only for 2021.1 or greater
//     # Intially only available for SDP
//     [[ $UseSDP -eq 1 ]] || return
//     p4dver=$($P4DBIN -V |grep Rev.|awk -F / '{print $3}' )
//     # shellcheck disable=SC2072
//     [[ "$p4dver" > "2020.0" ]] || return

//     realtimefile="/tmp/show-realtime.out"
//     $P4DBIN --show-realtime > "$realtimefile" 2> /dev/null || return

//     # File format:
//     # rtv.db.lockwait (flags 0) 0 max 382
//     # rtv.db.ckp.active (flags 0) 0
//     # rtv.db.ckp.records (flags 0) 34 max 34
//     # rtv.db.io.records (flags 0) 126389592854
//     # rtv.rpl.behind.bytes (flags 0) 0 max -1
//     # rtv.rpl.behind.journals (flags 0) 0 max -1
//     # rtv.svr.sessions.active (flags 0) 110 max 585
//     # rtv.svr.sessions.total (flags 0) 5997080

//     fname="$metrics_root/p4_realtime${sdpinst_suffix}-${SERVER_ID}.prom"
//     tmpfname="$fname.$$"

//     rm -f "$tmpfname"
//     metric_count=0
//     origname="rtv.db.lockwait"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime lockwait counter" >> "$tmpfname"
//         echo "# TYPE $mname gauge" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     origname="rtv.db.ckp.active"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime checkpoint active indicator" >> "$tmpfname"
//         echo "# TYPE $mname gauge" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     origname="rtv.db.ckp.records"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime checkpoint records counter" >> "$tmpfname"
//         echo "# TYPE $mname gauge" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     origname="rtv.db.io.records"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime IO records counter" >> "$tmpfname"
//         echo "# TYPE $mname counter" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     origname="rtv.rpl.behind.bytes"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime replica bytes lag counter" >> "$tmpfname"
//         echo "# TYPE $mname gauge" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     origname="rtv.rpl.behind.journals"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime replica journal lag counter" >> "$tmpfname"
//         echo "# TYPE $mname gauge" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     origname="rtv.svr.sessions.active"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime server active sessions counter" >> "$tmpfname"
//         echo "# TYPE $mname gauge" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     origname="rtv.svr.sessions.total"
//     mname="p4_${origname//./_}"
//     count=$(grep "$origname" "$realtimefile" | awk '{print $4}')
//     if [[ ! -z $count ]]; then
//         metric_count=$(($metric_count + 1))
//         echo "# HELP $mname P4 realtime server total sessions counter" >> "$tmpfname"
//         echo "# TYPE $mname counter" >> "$tmpfname"
//         echo "$mname{${serverid_label}${sdpinst_label}} $count" >> "$tmpfname"
//     fi

//     if [[ $metric_count -gt 0 ]]; then
//         chmod 644 "$tmpfname"
//         mv "$tmpfname" "$fname"
//     fi
// }

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
	p4m.logger.Debugf("monitorSwarm")

	p4cmd, errbuf, p := p4m.newP4CmdPipe("-ztag info -s")
	authID, err := p.Exec(p4cmd).Match("... serverCluster").Column(3).String()
	if err != nil {
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
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
		logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
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
		cfg.MetricsOutput, cfg.SDPInstance)

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
		m := p4m.getCumulativeMetrics()
		p4m.writeMetricsFile([]byte(m))
	}
	p4m.monitorUptime()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorChange()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorCheckpoint()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorFilesys()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorHASSSL()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorLicense()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorProcesses()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorReplicas()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)
	p4m.monitorSSL()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
	p4m.metrics = make([]metricStruct, 0)

}
