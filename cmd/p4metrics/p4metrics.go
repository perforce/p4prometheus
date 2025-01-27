// This is a version of monitor_metrics.sh in Go as part of p4prometheus
// It is intended to be more reliable and cross platform than the original.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/perforce/p4prometheus/cmd/p4metrics/config"

	"github.com/bitfield/script"
	"github.com/perforce/p4prometheus/version"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

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
		p4m.logger.Debugf("parseUptime: failed to split: '%s'", value)
		return 0
	}
	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		p4m.logger.Debugf("parseUptime: invalid hours: %v", err)
		return 0
	}
	minutes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		p4m.logger.Debugf("parseUptime: invalid minutes: %v", err)
		return 0
	}
	if minutes > 59 {
		p4m.logger.Debugf("parseUptime: minutes must be between 0 and 59")
		return 0
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		p4m.logger.Debugf("parseUptime: invalid seconds: %v", err)
		return 0
	}
	if seconds > 59 {
		p4m.logger.Debugf("parseUptime: seconds must be between 0 and 59")
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

func (p4m *P4MonitorMetrics) monitorUptime() {
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
		licenseInfo = v
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
		pattern := `\S*(.*?)\S*(\(support [^\)]+\))(\(expires [^\)]+\))`
		re, err := regexp.Compile(pattern)
		if err != nil {
			p4m.logger.Errorf("failed to compile regex: %v", err)
		} else {
			m := re.FindStringSubmatch(licenseInfoFull)
			if len(m) < 2 {
				p4m.logger.Errorf("failed to compile regex: %v", err)
			} else {
				licenseInfo = m[1]
			}
		}

		// licenseInfo=$(grep "Server license: " "$tmp_info_data" | sed -e "s/Server license: //" | sed -Ee "s/\(expires [^\)]+\)//" | sed -Ee "s/\(support [^\)]+\)//" )
		// if [[ -z $licenseTimeRemaining && ! -z $supportExpires ]]; then
		// TODO: subtract date from the value
		//     dt=$(date +%s)
		//     licenseTimeRemaining=$(($supportExpires - $dt))
		// fi
		// # Trim trailing spaces
		// licenseInfo=$(echo $licenseInfo | sed -Ee 's/[ ]+$//')
		// licenseIP=$(grep "Server : " "$tmp_info_data" | sed -e "s/Server license-ip: //")

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

func (p4m *P4MonitorMetrics) newP4CmdPipe(cmd string) (string, *bytes.Buffer, *script.Pipe) {
	errbuf := new(bytes.Buffer)
	p := script.NewPipe().WithStderr(errbuf)
	cmd = fmt.Sprintf("%s %s", p4m.p4Cmd, cmd)
	p4m.logger.Debugf("cmd: %s", cmd)
	return cmd, errbuf, p
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
	// p4m.monitorUptime()
	p4m.logger.Debugf("metrics: %q", p4m.metrics)
}
