// This is a version of monitor_metrics.sh in Go as part of p4prometheus
// It is intended to be more reliable and cross platform than the original.
// It should be run permanently as a systemd service on Linux, as it tails
// the errors.csv file
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/perforce/p4prometheus/cmd/p4metrics/config"
	"github.com/rcowham/go-libtail/tailer/fswatcher"

	"github.com/bitfield/script"
	"github.com/perforce/p4prometheus/version"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

// GO standard reference value/format: Mon Jan 2 15:04:05 -0700 MST 2006
const p4InfoTimeFormat = "2006/01/02 15:04:05 -0700 MST"
const checkpointTimeFormat = "2006-01-02 15:04:05"
const opensslTimeFormat = "Jan 2 15:04:05 2006 MST"
const BufferSize = 1024 * 1024 // 1MB buffer

// CompressFileResult holds the result of the compression operation
type CompressFileResult struct {
	InputFile      string
	OutputFile     string
	OriginalSize   int64
	CompressedSize int64
	Error          error
}

// CompressFileAsync compresses a file in a separate goroutine
func CompressFileAsync(logger *logrus.Logger, inputPath, outputPath string) <-chan CompressFileResult {
	resultChan := make(chan CompressFileResult, 1)

	go func() {
		defer close(resultChan)

		result := CompressFileResult{
			InputFile:  inputPath,
			OutputFile: outputPath,
		}

		// Open input file
		inputFile, err := os.Open(inputPath)
		if err != nil {
			result.Error = fmt.Errorf("failed to open input file: %w", err)
			resultChan <- result
			return
		}
		defer inputFile.Close()

		inputInfo, err := inputFile.Stat()
		if err != nil {
			result.Error = fmt.Errorf("failed to get input file info: %w", err)
			resultChan <- result
			return
		}
		result.OriginalSize = inputInfo.Size()
		logger.Debugf("Compressing file: %s, size: %d bytes", inputPath, result.OriginalSize)

		outputFile, err := os.Create(outputPath)
		if err != nil {
			result.Error = fmt.Errorf("failed to create output file: %w", err)
			resultChan <- result
			return
		}
		logger.Debugf("Creating compressed file: %s", outputPath)
		defer outputFile.Close()

		gzipWriter := gzip.NewWriter(outputFile)
		defer gzipWriter.Close()

		gzipWriter.Name = filepath.Base(inputPath)
		buffer := make([]byte, BufferSize)

		// Copy data from input to gzip writer using the buffer
		_, err = io.CopyBuffer(gzipWriter, inputFile, buffer)
		if err != nil {
			result.Error = fmt.Errorf("failed to compress file: %w", err)
			resultChan <- result
			return
		}

		// Close gzip writer to flush any remaining data
		err = gzipWriter.Close()
		if err != nil {
			result.Error = fmt.Errorf("failed to close gzip writer: %w", err)
			resultChan <- result
			return
		}
		outputInfo, err := outputFile.Stat()
		if err != nil {
			result.Error = fmt.Errorf("failed to get output file info: %w", err)
			resultChan <- result
			return
		}
		result.CompressedSize = outputInfo.Size()
		logger.Debugf("Compression completed: %s, size: %d bytes", outputPath, result.CompressedSize)
		resultChan <- result
	}()

	return resultChan
}

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
	// Get the current environment
	oldEnv := make(map[string]string)
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		oldEnv[pair[0]] = pair[1]
	}

	errbuf := new(bytes.Buffer)
	p := script.NewPipe().WithStderr(errbuf)
	cmd := fmt.Sprintf("bash -c \"source /p4/common/bin/p4_vars %s && env\"", sdpInstance)
	logger.Debugf("cmd: %s", cmd)
	output, err := p.Exec(cmd).Slice()
	if err != nil {
		logger.Errorf("Error: %v, %q", err, errbuf.String())
		logger.Fatalf("Can't source SDP vars: %s", sdpInstance)
	}

	// Parse the new environment
	newEnv := make(map[string]string)
	for _, line := range output {
		line = strings.TrimSpace(line)
		pair := strings.SplitN(line, "=", 2)
		if len(pair) == 2 {
			newEnv[pair[0]] = pair[1]
		}
	}

	// Other interesting env vars
	results := make(map[string]string, 0)
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
	logger.Debugf("envVars: %q", results)
	return results
}

func getVar(vars map[string]string, k string) string {
	if v, ok := vars[k]; ok {
		return v
	}
	return ""
}

// VolumeInfo represents disk usage information for a volume
type VolumeInfo struct {
	Name        string // Volume name, e.g. P4JOURNAL
	Type        string // Type of filesystem, e.g. ext4
	MountPoint  string // Mount point, e.g. /hxlogs
	Free        int64  // Free space in bytes
	Used        int64  // Used space in bytes
	Total       int64  // Total space in bytes
	PercentFull int    // Percent full, e.g. 45
}

// defines metrics label
type labelStruct struct {
	name  string
	value string
}

type metricStruct struct {
	name   string
	help   string
	mtype  string
	value  string
	labels []labelStruct
}

type ErrorMetric struct {
	Subsystem string
	Severity  string
}

// P4MonitorMetrics structure
type P4MonitorMetrics struct {
	config              *config.Config
	initialised         bool
	loginError          bool
	dryrun              bool
	env                 *map[string]string
	logger              *logrus.Logger
	p4User              string
	isSuper             bool // Is this user a super user?
	serverID            string
	rootDir             string
	logsDir             string
	p4Cmd               string
	sdpInstance         string
	sdpInstanceLabel    string
	sdpInstanceSuffix   string
	p4info              map[string]string
	p4license           map[string]string
	p4log               string
	p4journal           string
	journalPrefix       string
	p4errorsCSV         string
	version             string
	rotatedJournals     int       // Number of rotated journals
	rotatedLogs         int       // Number of rotated logs
	indErrSeverity      int       // Index of Severity in errors.csv
	indErrSubsys        int       // Index of subsys
	verifyLogModTime    time.Time // Time when last looked at verify
	verifyErrsSubmitted int64
	verifyErrsSpec      int64
	verifyErrsUnload    int64
	verifyErrsShelved   int64
	verifyDuration      int
	errorMetrics        map[ErrorMetric]int
	errLock             sync.Mutex
	metricsFilePrefix   string
	metricNames         map[string]int // Used when printing to avoid duplicate headers
	metrics             []metricStruct
	errTailer           *fswatcher.FileTailer
}

func newP4MonitorMetrics(config *config.Config, envVars *map[string]string, logger *logrus.Logger) (p4m *P4MonitorMetrics) {
	return &P4MonitorMetrics{
		config:       config,
		env:          envVars,
		logger:       logger,
		p4info:       make(map[string]string),
		p4license:    make(map[string]string),
		errorMetrics: make(map[ErrorMetric]int),
		metrics:      make([]metricStruct, 0),
	}
}

func (p4m *P4MonitorMetrics) initVars() {
	if p4m.initialised {
		p4m.logger.Debug("initVars: already initialised")
		return
	}
	// Note that P4BIN is defined by SDP by sourcing above file, as are P4USER, P4PORT
	p4bin := "p4"
	p4m.p4User = getVar(*p4m.env, "P4USER")
	p4m.logger.Debugf("p4User: %s", p4m.p4User)
	p4port := getVar(*p4m.env, "P4PORT")
	p4trust := getVar(*p4m.env, "P4TRUST")
	p4tickets := getVar(*p4m.env, "P4TICKETS")
	p4config := getVar(*p4m.env, "P4CONFIG")
	p4configEnv := ""
	if p4m.config.SDPInstance == "" {
		p4m.logger.Debug("Non-SDP")
		if p4m.config.P4Bin != "" {
			p4bin = p4m.config.P4Bin
		}
		if p4m.config.P4Port != "" {
			p4port = p4m.config.P4Port
		}
		if p4m.config.P4User != "" {
			p4m.p4User = p4m.config.P4User
		}
		if p4m.config.P4Config != "" {
			p4config = p4m.config.P4Config
		}
		if p4config != "" {
			p4m.logger.Debugf("setting P4CONFIG=%s", p4config)
			os.Setenv("P4CONFIG", p4config)
			p4configEnv = fmt.Sprintf("-E P4CONFIG=%s", p4config)
		}
		p4m.sdpInstanceLabel = ""
		p4m.sdpInstanceSuffix = ""
	} else {
		p4m.sdpInstance = getVar(*p4m.env, "SDP_INSTANCE")
		p4m.logger.Debugf("SDP: %s", p4m.sdpInstance)
		p4bin = getVar(*p4m.env, "P4BIN")
		p4m.p4log = getVar(*p4m.env, "P4LOG")
		p4m.logger.Debugf("logFile: %s", p4m.p4log)
		p4m.logsDir = getVar(*p4m.env, "LOGS")
		p4m.logger.Debugf("LOGS: %s", p4m.logsDir)
		p4m.sdpInstanceLabel = fmt.Sprintf(",sdpinst=\"%s\"", p4m.sdpInstance)
		p4m.logger.Debugf("sdpInstanceLabel: %s", p4m.sdpInstanceLabel)
		p4m.sdpInstanceSuffix = fmt.Sprintf("-%s", p4m.sdpInstance)
		p4m.logger.Debugf("sdpInstanceSuffix: %s", p4m.sdpInstanceSuffix)
		p4m.p4errorsCSV = path.Join(p4m.logsDir, "errors.csv")
		if stat, err := os.Stat(p4m.logsDir); err != nil || !stat.IsDir() {
			p4m.logger.Errorf("SDP LOGS dir '%s' does not exist - is the sdp_instance '%s' correct? (Specified as a parameter or in the config file)", p4m.logsDir, p4m.sdpInstance)
			return
		}

	}
	if p4bin == "" {
		p4m.logger.Error("Failed to find P4BIN in environment!")
		return
	}
	if p4trust != "" {
		p4m.logger.Debugf("setting P4TRUST=%s", p4trust)
		os.Setenv("P4TRUST", p4trust)
	}
	if p4tickets != "" {
		p4m.logger.Debugf("setting P4TICKETS=%s", p4tickets)
		os.Setenv("P4TICKETS", p4tickets)
	}
	p4userStr := ""
	if p4m.p4User != "" {
		p4userStr = fmt.Sprintf("-u %s", p4m.p4User)
	}
	p4portStr := ""
	if p4port != "" {
		p4portStr = fmt.Sprintf("-p \"%s\"", p4port)
	}
	p4m.p4Cmd = fmt.Sprintf("%s %s %s %s", p4bin, p4configEnv, p4userStr, p4portStr)
	p4m.logger.Debugf("p4Cmd: %s", p4m.p4Cmd)
	p4cmd, errbuf, p := p4m.newP4CmdPipe("info -s")
	i, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.checkServerID()
		p4m.logger.Errorf("Error can't connect to P4PORT: %q, %v, %q", p4port, err, errbuf.String())
		return
	}
	for _, s := range i {
		parts := strings.Split(s, ": ")
		if len(parts) == 2 {
			p4m.p4info[parts[0]] = parts[1]
		}
	}
	p4m.logger.Debugf("p4info -s: %d %v\n%v", len(i), i, p4m.p4info)
	// Get server id. Usually server.id files are a single line containing the
	// ServerID value. However, a server.id file will have a second line if a
	// 'p4 failover' was done containing an error message displayed to users
	// during the failover, and also preventing the service from starting
	// post-failover (to avoid split brain). For purposes of this check, we care
	// only about the ServerID value contained on the first line, so we use
	// 'head -1' on the server.id file.
	p4m.serverID = p4m.p4info["ServerID"]
	p4m.rootDir = p4m.p4info["Server root"]
	p4m.checkServerID()
	if p4m.serverID == "" {
		p4m.serverID = "UnsetServerID"
	}
	p4m.logger.Debugf("serverID: %q", p4m.serverID)
	p4cmd, errbuf, p = p4m.newP4CmdPipe("configure show")
	cfg, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, %q", "configure show", err, errbuf)
		return
	}
	p4m.logger.Debugf("configure show: %q", cfg)
	// Searching for a line like:
	// serverlog.file.3=/p4/1/logs/errors.csv (configure)
	for _, line := range cfg {
		parts := strings.Split(line, "=")
		if len(parts) < 2 {
			continue
		}
		k := parts[0]
		v := parts[1]
		if strings.HasPrefix(k, "serverlog.") && strings.Contains(v, "errors.csv") {
			p4m.p4errorsCSV = strings.TrimSpace(strings.Split(v, " ")[0])
			break
		}
		if k == "P4LOG" {
			p4m.p4log = strings.TrimSpace(strings.Split(v, " ")[0])
			break
		}
		if k == "P4JOURNAL" {
			p4m.p4journal = strings.TrimSpace(strings.Split(v, " ")[0])
			break
		}
		if k == "journalPrefix" {
			p4m.journalPrefix = strings.TrimSpace(strings.Split(v, " ")[0])
			break
		}
	}
	p4m.logger.Debugf("errorsFile: %s", p4m.p4errorsCSV)
	if runtime.GOOS != "windows" && !strings.HasPrefix(p4m.p4errorsCSV, "/") {
		// If the path is not absolute, it is relative to the rootDir
		p4m.p4errorsCSV = path.Join(p4m.rootDir, p4m.p4errorsCSV)
		p4m.logger.Debugf("errorsFile abspath: %s", p4m.p4errorsCSV)
	}

	// Check if a super user
	p4cmd, errbuf, p = p4m.newP4CmdPipe("protects -m")
	protects, err := p.Exec(p4cmd).String()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, %q", "protects -m", err, errbuf)
		return
	}
	protects = strings.TrimSpace(protects)
	p4m.logger.Debugf("user %s protects level %s", p4m.p4User, protects)
	if protects == "super" {
		p4m.isSuper = true
	}

	p4m.initialised = true
}

func (p4m *P4MonitorMetrics) checkServerID() {
	if p4m.serverID == "" {
		if p4m.rootDir == "" {
			p4m.rootDir = getVar(*p4m.env, "P4ROOT")
		}
		idFile := path.Join(p4m.rootDir, "server.id")
		p4m.logger.Debugf("serverID file: %q", idFile)
		if _, err := os.Stat(idFile); err == nil {
			s, err := script.File(idFile).Slice()
			if err == nil && len(s) > 0 {
				p4m.serverID = s[0]
				p4m.logger.Debug("found server.id")
			} else {
			}
		} else {
			p4m.logger.Debugf("server.id file does not exist: %s, %v", idFile, err)
		}
	}
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
	if _, ok := p4m.metricNames[mname]; !ok {
		// Only write metric header once for any particular name
		p4m.printMetricHeader(metrics, mname, mhelp, mtype)
	}
	p4m.metricNames[mname] = 1
	p4m.printMetric(metrics, mname, fixedLabels, metricVal)
}

func (p4m *P4MonitorMetrics) getCumulativeMetrics() string {
	fixedLabels := []labelStruct{{name: "serverid", value: p4m.serverID}}
	p4m.metricNames = make(map[string]int, 0)
	if p4m.config.SDPInstance != "" {
		fixedLabels = append(fixedLabels, labelStruct{name: "sdpinst", value: p4m.sdpInstance})
	}
	metrics := new(bytes.Buffer)
	for _, m := range p4m.metrics {
		labels := fixedLabels
		labels = append(labels, m.labels...)
		p4m.outputMetric(metrics, m.name, m.help, m.mtype, m.value, labels)
	}
	return metrics.String()
}

func (p4m *P4MonitorMetrics) metricsFilename(filePrefix string) string {
	instanceStr := ""
	if p4m.config.SDPInstance != "" {
		instanceStr = fmt.Sprintf("-%s", p4m.config.SDPInstance)
	}
	return path.Join(p4m.config.MetricsRoot,
		fmt.Sprintf("%s%s-%s.prom", filePrefix, instanceStr, p4m.serverID))
}

func (p4m *P4MonitorMetrics) deleteMetricsFile(filePrefix string) {
	outputFile := p4m.metricsFilename(filePrefix)
	if p4m.dryrun {
		return
	}
	p4m.metricsFilePrefix = filePrefix
	if err := os.Remove(outputFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		p4m.logger.Debugf("Failed to remove: %s, %v", outputFile, err)
	}
}

// Writes metrics to appropriate file - writes to temp file first and renames it after
func (p4m *P4MonitorMetrics) writeMetricsFile() {
	var f *os.File
	var err error
	outputFile := p4m.metricsFilename(p4m.metricsFilePrefix)
	if len(p4m.metrics) == 0 {
		p4m.logger.Debug("No metrics to write")
		return
	}
	p4m.logger.Debugf("Metrics: %q", p4m.metrics)
	if p4m.dryrun {
		return
	}
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
		p4m.logger.Debugf("monitorUptime: no value for key: %s", k)
		seconds = 0
	}
	if !p4m.initialised {
		p4m.logger.Debugf("monitorUptime: not initialised, skipping")
		seconds = 0
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
	if v, ok := p4m.p4info["Server license"]; ok {
		licenseInfoFull = v
	}
	if v, ok := p4m.p4info["Server license-ip"]; ok {
		licenseIP = v
	}

	userCount := ""
	userLimit := ""
	licenseExpires := ""
	licenseTimeRemaining := ""
	supportExpires := ""
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

	reNumeric := regexp.MustCompile(`^[0-9.-]+$`) // Can see strings like "unlimited"

	if userCount != "" && reNumeric.MatchString(userCount) {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_licensed_user_count",
				help:  "P4D Licensed User count",
				mtype: "gauge",
				value: userCount})
	}
	if userLimit != "" && reNumeric.MatchString(userLimit) {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_licensed_user_limit",
				help:  "P4D Licensed User Limit",
				mtype: "gauge",
				value: userLimit})
	}
	if licenseExpires != "" && reNumeric.MatchString(licenseExpires) {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_expires",
				help:  "P4D License expiry (epoch secs)",
				mtype: "gauge",
				value: licenseExpires})
	}
	if licenseTimeRemaining != "" && reNumeric.MatchString(licenseTimeRemaining) {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_time_remaining",
				help:  "P4D License time remaining (secs)",
				mtype: "gauge",
				value: licenseTimeRemaining})
	}
	if supportExpires != "" && reNumeric.MatchString(supportExpires) {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_support_expires",
				help:  "P4D License support expiry (epoch secs)",
				mtype: "gauge",
				value: supportExpires})
	}
	if licenseInfo != "" { // Metric where the value is in the label not the series
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_info",
				help:   "P4D License info",
				mtype:  "gauge",
				value:  "1",
				labels: []labelStruct{{name: "licenseInfo", value: licenseInfo}}})
	}
	if licenseIP != "" { // Metric where the value is in the label not the series
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_license_IP",
				help:   "P4D Licensed IP",
				mtype:  "", // Should be a gauge but for backwards compatibility we leave as untyped
				value:  "1",
				labels: []labelStruct{{name: "licenseIP", value: licenseIP}}})
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

	p4cmd, errbuf, p := p4m.newP4CmdPipe("license -u")
	licenseArr, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
		return
	}
	for _, s := range licenseArr {
		parts := strings.Split(s, " ")
		if len(parts) == 3 {
			p4m.p4license[parts[1]] = parts[2]
		}
	}
	p4m.logger.Debugf("License: %q, %q", licenseArr, p4m.p4license)
	p4m.parseLicense()
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) convertToBytes(size string) int64 {
	result, err := config.ConvertToBytes(size)
	if err != nil {
		p4m.logger.Errorf("convertToBytes: %v", err)
		return 0
	}
	return result
}

// Examples:
// P4ROOT (type ext4 mounted on /hxmetadata) : 100.3G free, 273.2G used, 393.5G total (73% full)
// P4JOURNAL (type ext4 mounted on /hxlogs) : 48.6G free, 26G used, 78.6G total (34% full)
// P4LOG (type ext4 mounted on /hxlogs) : 48.6G free, 26G used, 78.6G total (34% full)
// TEMP (type ext4 mounted on /hxlogs) : 48.6G free, 26G used, 78.6G total (34% full)
// journalPrefix (type xfs mounted on /hxdepots) : 795.5G free, 7T used, 7.8T total (90% full)
// serverlog.file.1 (type ext4 mounted on /hxlogs) : 48.6G free, 26G used, 78.6G total (34% full)

// parseVolumeInfo parses a single line of volume information
func (p4m *P4MonitorMetrics) parseVolumeInfo(line string) (*VolumeInfo, error) {
	// Regular expression to parse the line format
	// Example: P4ROOT (type ext4 mounted on /hxmetadata) : 100.3G free, 273.2G used, 393.5G total (73% full)
	re := regexp.MustCompile(`^(\S+)\s*\(type\s+(\w+)\s+mounted\s+on\s+(.+?)\)\s*:\s*(.+?)\s+free,\s*(.+?)\s+used,\s*(.+?)\s+total\s*\((\d+)%\s+full\)`)
	matches := re.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 8 {
		return nil, fmt.Errorf("parseVolumeInfo: invalid line format: %s", line)
	}

	name := strings.TrimSpace(matches[1])
	fsType := matches[2]
	mountPoint := matches[3]

	free := p4m.convertToBytes(matches[4])
	used := p4m.convertToBytes(matches[5])
	total := p4m.convertToBytes(matches[6])

	percentFull, err := strconv.Atoi(matches[7])
	if err != nil {
		return nil, fmt.Errorf("error parsing percentage: %v", err)
	}

	return &VolumeInfo{
		Name:        name,
		Type:        fsType,
		MountPoint:  mountPoint,
		Free:        free,
		Used:        used,
		Total:       total,
		PercentFull: percentFull,
	}, nil
}

// parseVolumeData parses the entire volume data string and returns a map of volumes
func (p4m *P4MonitorMetrics) parseDiskspace(lines []string) (map[string]*VolumeInfo, error) {
	volumes := make(map[string]*VolumeInfo)

	for _, line := range lines {
		if line == "" {
			continue
		}
		volume, err := p4m.parseVolumeInfo(line)
		if err != nil {
			return nil, fmt.Errorf("error parsing line '%s': %v", line, err)
		}
		volumes[volume.Name] = volume
	}
	return volumes, nil
}

func (p4m *P4MonitorMetrics) monitorJournalAndLogs() {
	p4m.startMonitor("monitorJournalAndLogs", "p4_journal_logs")

	p4cmd, errbuf, p := p4m.newP4CmdPipe("diskspace")
	vals, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
	}
	p4m.logger.Debugf("Diskspace values: %q", vals)
	volumes, err := p4m.parseDiskspace(vals)
	if err != nil {
		p4m.logger.Errorf("Error parsing diskspace: %v", err)
	}
	if volumes != nil {
		p4m.logger.Debugf("Parsed Diskspace values: %v", volumes)
	}

	jstat, err := os.Stat(p4m.p4journal)
	if err != nil {
		p4m.logger.Debugf("Failed to stat %s: %v", p4m.p4journal, err)
	} else {
		m := metricStruct{name: "p4_journal_size",
			help:  "Size of P4JOURNAL in bytes",
			mtype: "gauge",
			value: fmt.Sprintf("%d", jstat.Size()),
		}
		p4m.metrics = append(p4m.metrics, m)
	}
	lstat, err := os.Stat(p4m.p4log)
	if err != nil {
		p4m.logger.Debugf("Failed to stat %s: %v", p4m.p4log, err)
	} else {
		m := metricStruct{name: "p4_log_size",
			help:  "Size of P4LOG in bytes",
			mtype: "gauge",
			value: fmt.Sprintf("%d", lstat.Size()),
		}
		p4m.metrics = append(p4m.metrics, m)
	}

	if p4m.sdpInstance != "" {
		entries, err := os.ReadDir(p4m.logsDir)
		if err != nil {
			p4m.logger.Warnf("Error reading directory: %s %v", p4m.logsDir, err)
			return
		} else {
			fileCount := 0
			for _, entry := range entries {
				if !entry.IsDir() {
					fileCount++
				}
			}
			m := metricStruct{name: "p4_logs_file_count",
				help:  "Count of files in SDP logs directory",
				mtype: "gauge",
				value: fmt.Sprintf("%d", fileCount),
			}
			p4m.metrics = append(p4m.metrics, m)
		}
	}

	// Let's check for the size of the journal and whether we should rotate it
	jvol, ok1 := volumes["P4JOURNAL"]
	pvol, ok2 := volumes["journalPrefix"]
	p4dServices := "unknown"
	if v, ok := p4m.p4info["Server services"]; ok {
		p4dServices = v
	}
	rotateJournal := false
	if p4dServices != "standard" && p4dServices != "commit-server" {
		p4m.logger.Debugf("Not checking for journal rotation as not commit/standard: %s", p4dServices)
	} else {
		if ok1 && ok2 {
			p4m.logger.Debugf("journal volume - size %d, free space %d", jstat.Size(), jvol.Free)
			if float64(jstat.Size())*1.1 < float64(pvol.Free) {
				if p4m.config.MaxJournalSizeInt > 0 && jstat.Size() > p4m.config.MaxJournalSizeInt {
					p4m.logger.Debugf("journal will be rotated - size %d, max %d", jstat.Size(), p4m.config.MaxJournalSizeInt)
					rotateJournal = true
				}
			} else {
				p4m.logger.Warningf("Not enough free space to rotate journal - size %d, free space %d", jstat.Size(), pvol.Free)
			}
			p4m.logger.Debugf("journal volume - size %d, free space %d, rotate %v", jstat.Size(), jvol.Free, rotateJournal)
			if !rotateJournal && p4m.config.MaxJournalPercentInt > 0 {
				percentSize := float64(jvol.Total) * float64(p4m.config.MaxJournalPercentInt) / float64(100.0)
				if float64(jstat.Size()) > percentSize {
					p4m.logger.Debugf("journal will be rotated - size %d, max percent %d, val %.0f", jstat.Size(), p4m.config.MaxJournalPercentInt, percentSize)
					rotateJournal = true
				}
			}
		} else {
			if !ok1 {
				p4m.logger.Debug("No volume information for P4JOURNAL")
			}
			if !ok2 {
				p4m.logger.Debug("No volume information for journalPrefix")
			}
		}
	}
	if rotateJournal {
		if p4m.isSuper {
			p4m.logger.Infof("Rotating journal - size %d, free space %d", jstat.Size(), pvol.Free)
			p4cmd, errbuf, p := p4m.newP4CmdPipe("admin journal")
			err := p.Exec(p4cmd).Error()
			if err != nil {
				p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
			} else {
				p4m.logger.Info("Journal rotated")
				p4m.rotatedJournals++
			}
		} else {
			p4m.logger.Warning("Not super user - cannot rotate journal")
			rotateJournal = false
		}
	}

	// Same for size of the log file - but we can always rotate it irrespective of p4d services
	lvol, ok3 := volumes["P4LOG"]
	rotateLog := false
	if ok3 {
		p4m.logger.Debugf("log volume - size %d, free space %d", lstat.Size(), lvol.Free)
		if p4m.config.MaxLogSizeInt > 0 && lstat.Size() > p4m.config.MaxLogSizeInt {
			p4m.logger.Debugf("log will be rotated - size %d, max %d", lstat.Size(), p4m.config.MaxLogSizeInt)
			rotateLog = true
		}
		if !rotateLog && p4m.config.MaxLogPercentInt > 0 {
			percentSize := float64(lvol.Total) * float64(p4m.config.MaxLogPercentInt) / float64(100.0)
			if float64(lstat.Size()) > percentSize {
				p4m.logger.Debugf("log will be rotated - size %d, max percent %d, val %.0f", lstat.Size(), p4m.config.MaxLogPercentInt, percentSize)
				rotateLog = true
			}
		}
	}
	if rotateLog {
		p4m.logger.Infof("Rotating log - size %d, free space %d", lstat.Size(), lvol.Free)
		logDir := filepath.Dir(p4m.p4log)
		fileName := filepath.Base(p4m.p4log)
		now := time.Now()
		newFile := filepath.Join(logDir, now.Format(fmt.Sprintf("%s.2006-01-02_15-04-05", fileName)))
		zipFile := newFile + ".gz"
		err := os.Rename(p4m.p4log, newFile)
		if err != nil {
			p4m.logger.Errorf("Error renaming %q to %q, err:%q", p4m.p4log, newFile, err)
		} else {
			p4m.logger.Infof("Log rotated - starting compression in background: %q to %q", newFile, zipFile)
			p4m.rotatedLogs++
			zipResultChan := CompressFileAsync(p4m.logger, newFile, zipFile)
			go func() {
				// Wait for compression to complete
				result := <-zipResultChan
				if result.Error != nil {
					p4m.logger.Errorf("Compression failed: %v", result.Error)
				} else {
					compressionRatio := float64(result.CompressedSize) / float64(result.OriginalSize) * 100
					p4m.logger.Infof("Log compression completed successfully!")
					p4m.logger.Infof("Original size: %d, compressed %d, ratio %.2f%%",
						result.OriginalSize, result.CompressedSize, compressionRatio)
					err := os.Remove(newFile)
					if err != nil {
						p4m.logger.Errorf("Error removing compressed file %q, err:%q", zipFile, err)
					} else {
						p4m.logger.Infof("Removed compressed file: %q", newFile)
					}
				}
			}()
		}
	}

	m := metricStruct{name: "p4_journals_rotated",
		help:  "Count of rotations of P4JOURNAL by p4metrics",
		mtype: "counter",
		value: fmt.Sprintf("%d", p4m.rotatedJournals),
	}
	p4m.metrics = append(p4m.metrics, m)
	m = metricStruct{name: "p4_logs_rotated",
		help:  "Count of rotations of P4LOG by p4metrics",
		mtype: "counter",
		value: fmt.Sprintf("%d", p4m.rotatedLogs),
	}
	p4m.metrics = append(p4m.metrics, m)

	p4m.writeMetricsFile()
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
			m.value = fmt.Sprintf("%d", p4m.convertToBytes(value))
			m.labels = []labelStruct{{name: "filesys", value: filesysName}}
			p4m.metrics = append(p4m.metrics, m)
		}
	}
}

func (p4m *P4MonitorMetrics) monitorFilesys() {
	// Log current filesys.*.min settings
	p4m.startMonitor("monitorFilesys", "p4_filesys")
	// p4 configure show can give 2 values, or just the (default)
	//    filesys.P4ROOT.min=5G (configure)
	//    filesys.P4ROOT.min=250M (default)
	// fname="$metrics_root/p4_filesys${sdpinst_suffix}-${SERVER_ID}.prom"
	configurables := strings.Split("filesys.depot.min filesys.P4ROOT.min filesys.P4JOURNAL.min filesys.P4LOG.min filesys.TEMP.min", " ")
	configValues := make([]string, 0)
	for _, c := range configurables {
		p4cmd, errbuf, p := p4m.newP4CmdPipe(fmt.Sprintf("configure show %s", c))
		vals, err := p.Exec(p4cmd).Slice()
		if err != nil {
			p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
		}
		configValues = append(configValues, vals...)
	}
	p4m.logger.Debugf("Filesys config values: %q", configValues)
	p4m.parseFilesys(configValues)
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) monitorVersions() {
	// P4D, SDP and p4metrics Versions
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
		help:   "P4D Version/build info",
		mtype:  "gauge",
		value:  "1",
		labels: []labelStruct{{name: "version", value: p4dVersion}}})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_p4d_server_type",
		help:   "P4D server type/services",
		mtype:  "gauge",
		value:  "1",
		labels: []labelStruct{{name: "services", value: p4dServices}}})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_p4metrics_version",
		help:   "P4Metrics version",
		mtype:  "gauge",
		value:  "1",
		labels: []labelStruct{{name: "version", value: p4m.version}}})

	if p4m.config.SDPInstance != "" {
		SDPVersion, err := script.File("/p4/sdp/Version").First(1).String()
		if err != nil {
			p4m.logger.Errorf("failed to read sdp version: %v", err)
		} else {
			SDPVersion = strings.TrimSpace(SDPVersion)
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_version",
				help:   "SDP Version",
				mtype:  "gauge",
				value:  "1",
				labels: []labelStruct{{name: "version", value: SDPVersion}}})
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

// HASVersionResponse represents the structure of the JSON response
type HASVersionResponse struct {
	AppVersion string `json:"app_version"`
	Status     string `json:"status"`
}

// getAuthVersion retrieves the app version from Helix Auth URL
func (p4m *P4MonitorMetrics) getAuthVersion(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound { // Pre 2022.2 versions of HAS - or status page disabled
		return "unknown", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP request failed with status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	var versionResp HASVersionResponse
	err = json.Unmarshal(body, &versionResp)
	if err != nil {
		return "", fmt.Errorf("failed to parse JSON: %v", err)
	}
	return versionResp.AppVersion, nil
}

func (p4m *P4MonitorMetrics) monitorHelixAuthSvc() {
	// Check expiry of HAS SSL certificate - if it exists!
	p4m.startMonitor("monitorHelixAuthSvc", "p4_auth_ssl_info")
	// TODO: update frequency
	p4cmd, errbuf, p := p4m.newP4CmdPipe("extension --list --type extensions")
	ext, err := p.Exec(p4cmd).Match("extension Auth::loginhook").Column(2).String()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
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
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
		return
	}
	urlAuth := p4m.extractServiceURL(lines)
	if !strings.HasPrefix(urlAuth, "https") {
		p4m.logger.Debug("Auth URL not https so exiting")
		return
	}
	p4m.logger.Debugf("Auth URL: %q", urlAuth)
	certExpiryTime, err := p4m.getCertificateExpiry(urlAuth)
	if err != nil {
		p4m.logger.Errorf("Error getting cert url expiry %s: %v", urlAuth, err)
		return
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_auth_ssl_cert_expires",
		help:   "P4D Auth SSL certificate expiry epoch seconds",
		mtype:  "gauge",
		value:  fmt.Sprintf("%d", certExpiryTime.Unix()),
		labels: []labelStruct{{name: "url", value: urlAuth}}})

	urlStatus := fmt.Sprintf("%s/status", urlAuth)
	versionString, err := p4m.getAuthVersion(urlStatus)
	if err != nil {
		p4m.logger.Errorf("Error getting Auth status %s: %v", urlStatus, err)
	} else {
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_auth_version",
			help:   "P4 Auth Svc version string",
			mtype:  "gauge",
			value:  "1",
			labels: []labelStruct{{name: "version", value: versionString}}})
	}
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) handleP4Error(fmtStr string, p4cmd string, err error, errbuf *bytes.Buffer) {
	p4m.logger.Errorf(fmtStr, p4cmd, err, errbuf.String())
	if strings.Contains(errbuf.String(), "Connect to server failed; check $P4PORT") {
		p4m.initialised = false
	}
	if strings.Contains(errbuf.String(), "Perforce password (P4PASSWD) invalid or unset") {
		p4m.loginError = true
	}
}

func (p4m *P4MonitorMetrics) monitorMonitoring() {
	// Make sure we keep an eye on the monitoring and login status
	p4m.startMonitor("monitorMonitoring", "p4_status")
	initVal := "1"
	if !p4m.initialised {
		initVal = "0"
	}
	loginVal := "0"
	if !p4m.loginError {
		loginVal = "1"
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_monitoring_up",
		help:  "P4 monitoring initialised and working",
		mtype: "gauge",
		value: initVal})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_login_error",
		help:  "P4 monitoring login error",
		mtype: "gauge",
		value: loginVal})
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) monitorChange() {
	// Latest changelist counter as single counter value
	p4m.startMonitor("monitorChange", "p4_change")
	p4cmd, errbuf, p := p4m.newP4CmdPipe("counter change")
	change, err := p.Exec(p4cmd).String()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
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

func (p4m *P4MonitorMetrics) getTime(t string) int {
	// Convert fields with HH:MM:SS into seconds (hours can be hundreds)
	parts := strings.Split(t, ":")
	if len(parts) != 3 {
		return 0
	}
	var h, m, s int
	var err error
	if h, err = strconv.Atoi(parts[0]); err == nil {
		h = h * 3600
	}
	if m, err = strconv.Atoi(parts[1]); err == nil {
		m = m * 60
	}
	if s, err = strconv.Atoi(parts[2]); err == nil {
		return h + m + s
	}
	return 0
}

func (p4m *P4MonitorMetrics) getMaxNonSvcCmdTime(lines []string) int {
	// Extract the max time for any command which isn't a service user
	maxTime := 0
	uFieldNum := 3
	tFieldNum := 4
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < tFieldNum { // Skip lines that don't have sufficient fields
			continue
		}
		u := fields[uFieldNum-1]
		if strings.HasPrefix(u, "svc_") { // Ignore service users as very long running
			continue
		}
		t := p4m.getTime(fields[tFieldNum-1])
		if t > maxTime {
			maxTime = t
		}
	}
	return maxTime
}

func (p4m *P4MonitorMetrics) monitorProcesses() {
	// Monitor metrics summarised by cmd or user
	p4m.startMonitor("monitorProcesses", "p4_monitor")
	// Exepected columns in monitor show -l output:
	// Pid, state, user, time, cmd
	// 	8764 R user 00:00:00 edit
	p4cmd, errbuf, p := p4m.newP4CmdPipe("monitor show -l")
	monitorOutput, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
		return
	}
	p4m.logger.Debugf("Monitor output: %q", monitorOutput)
	cmdCounts := p4m.getFieldCounts(monitorOutput, 5)
	for cmd, count := range cmdCounts {
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_monitor_by_cmd",
			help:   "P4 running processes by cmd in monitor table",
			mtype:  "counter",
			value:  fmt.Sprintf("%d", count),
			labels: []labelStruct{{name: "cmd", value: cmd}}})
	}
	if p4m.config.CmdsByUser {
		userCounts := p4m.getFieldCounts(monitorOutput, 3)
		for user, count := range userCounts {
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_monitor_by_user",
				help:   "P4 running processes by user in monitor table",
				mtype:  "counter",
				value:  fmt.Sprintf("%d", count),
				labels: []labelStruct{{name: "user", value: user}}})
		}
	}
	stateCounts := p4m.getFieldCounts(monitorOutput, 2)
	for state, count := range stateCounts {
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_monitor_by_state",
			help:   "P4 running processes by state in monitor table",
			mtype:  "gauge",
			value:  fmt.Sprintf("%d", count),
			labels: []labelStruct{{name: "state", value: state}}})
	}
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_monitor_max_cmd_time",
		help:  "P4 monitor max (non-svc) command run time",
		mtype: "gauge",
		value: fmt.Sprintf("%d", p4m.getMaxNonSvcCmdTime(monitorOutput))})
	if runtime.GOOS == "linux" { // Don't bother on Windows
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
		} else {
			// Old monitor_metrics.sh has p4_process_count but as a counter - should be a gauge!
			// So new name
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_processes_count",
				help:  "P4 count of running processes (via ps)",
				mtype: "gauge",
				value: fmt.Sprintf("%d", pcount)})
		}
	}
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
		p4m.logger.Debug("No SDP instance so exiting")
		return
	}
	if runtime.GOOS != "linux" { // Don't bother on Windows
		p4m.logger.Debug("Not running on Windows so exiting")
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
	p4m.logger.Debugf("Checkpoint file modtime %v", fileInfo.ModTime())
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_checkpoint_log_time",
		help:  "Time of last checkpoint log",
		mtype: "gauge",
		value: fmt.Sprintf("%d", fileInfo.ModTime().Unix())})
	reRemoveNonDate := regexp.MustCompile(` \/p4.*`)
	startTimeStr := reRemoveNonDate.ReplaceAllString(startEndLines[0], "")
	endTimeStr := reRemoveNonDate.ReplaceAllString(startEndLines[1], "")
	p4m.logger.Debugf("Start/end time %q/%q", startTimeStr, endTimeStr)
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

func (p4m *P4MonitorMetrics) parseVerifyLog(lines []string) {
	// Expected lines in log file (SDP 2023.1 or later!)
	// Summary of Errors by Type:
	//    Submitted File Errors:          9
	//    Spec Depot Errors:              0
	//    Unload Depot Errors:            0
	//    Total Non-Shelve Errors:        9 (sum of error types listed above)
	//    Shelved Changes with Errors:    0
	totalNonShelveErrors := int64(0)
	found := false

	// Regular expression to extract numbers
	reErrors := regexp.MustCompile(`Errors:\s+(\d+)`)
	reCompletion := regexp.MustCompile(` taking (\d+) hours (\d+) minutes (\d+) seconds`)
	// Time: Completed verifications at Tue Apr  8 11:56:23 UTC 2025, taking 0 hours 0 minutes 1 seconds.

	// Look for the summary header
	for i, line := range lines {
		if strings.HasPrefix(line, "Status: OK: All scanned depots verified OK.") {
			p4m.verifyErrsSubmitted = 0
			p4m.verifyErrsSpec = 0
			p4m.verifyErrsUnload = 0
			p4m.verifyErrsShelved = 0
			found = true
		}
		if strings.HasPrefix(line, "Summary of Errors by Type:") {
			found = true
			// Process the next few lines to extract values
			for j := i + 1; j < len(lines) && j < i+10; j++ {
				currentLine := lines[j]
				matches := reErrors.FindStringSubmatch(currentLine)
				if len(matches) < 2 {
					continue
				}
				value, err := strconv.ParseInt(matches[1], 10, 64)
				if err != nil {
					continue
				}
				switch {
				case strings.Contains(currentLine, "Submitted File Errors"):
					p4m.verifyErrsSubmitted = value
				case strings.Contains(currentLine, "Spec Depot Errors"):
					p4m.verifyErrsSpec = value
				case strings.Contains(currentLine, "Unload Depot Errors"):
					p4m.verifyErrsUnload = value
				case strings.Contains(currentLine, "Total Non-Shelve Errors"):
					totalNonShelveErrors = value
				case strings.Contains(currentLine, "Shelved Changes with Errors"):
					p4m.verifyErrsShelved = value
				}
			}
		}
		if strings.HasPrefix(line, "Time: Completed verifications at") {
			matches := reCompletion.FindStringSubmatch(line)
			if len(matches) == 4 {
				hours, _ := strconv.Atoi(matches[1])
				minutes, _ := strconv.Atoi(matches[2])
				seconds, _ := strconv.Atoi(matches[3])
				p4m.verifyDuration = hours*3600 + minutes*60 + seconds
			}
		}
	}
	if found {
		if totalNonShelveErrors != p4m.verifyErrsSubmitted+p4m.verifyErrsSpec+p4m.verifyErrsUnload {
			p4m.logger.Debugf("Failed to verify the sum of these values: %d + %d +%d = %d",
				p4m.verifyErrsSubmitted, p4m.verifyErrsSpec, p4m.verifyErrsUnload, totalNonShelveErrors)
		}
	}
}

func (p4m *P4MonitorMetrics) createVerifyMetrics() {
	// Create the metrics using stored values - all same metric name with different labels
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_verify_errors",
		help:   "Count of verify errors in SDP p4verify.log",
		mtype:  "gauge",
		value:  fmt.Sprintf("%d", p4m.verifyErrsSubmitted),
		labels: []labelStruct{{name: "type", value: "submitted"}}})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_verify_errors",
		help:   "Count of verify errors in SDP p4verify.log",
		mtype:  "gauge",
		value:  fmt.Sprintf("%d", p4m.verifyErrsSpec),
		labels: []labelStruct{{name: "type", value: "spec"}}})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_verify_errors",
		help:   "Count of verify errors in SDP p4verify.log",
		mtype:  "gauge",
		value:  fmt.Sprintf("%d", p4m.verifyErrsUnload),
		labels: []labelStruct{{name: "type", value: "unload"}}})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_verify_errors",
		help:   "Count of verify errors in SDP p4verify.log",
		mtype:  "gauge",
		value:  fmt.Sprintf("%d", p4m.verifyErrsShelved),
		labels: []labelStruct{{name: "type", value: "shelved"}}})
	// Metric for when log was last written
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_verify_log_modtime",
		help:  "Time of modification of last SDP p4verify log",
		mtype: "gauge",
		value: fmt.Sprintf("%d", p4m.verifyLogModTime.Unix())})
	p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_sdp_verify_duration",
		help:  "Duration of last p4verify.sh script run",
		mtype: "gauge",
		value: fmt.Sprintf("%d", p4m.verifyDuration)})

}

func (p4m *P4MonitorMetrics) monitorVerify() {
	// Metric for when verify last ran and how many errors there are.
	p4m.startMonitor("monitorVerify", "p4_verify")
	// Not as easy as it might first appear because:
	// - might be in progress
	// The strings searched for have been present in SDP logs since 2023.1 now...
	if p4m.config.SDPInstance == "" {
		p4m.logger.Debug("No SDP instance so exiting")
		return
	}
	if runtime.GOOS != "linux" { // Don't bother on Windows - as yet at least!
		p4m.logger.Debug("Not running on Windows so exiting")
		return
	}
	sdpInstance := p4m.config.SDPInstance

	// We check and use previously stored values if:
	// * there are some!
	// * the verify log file has not been updated since we last looked
	verifyLog := fmt.Sprintf("/p4/%s/logs/p4verify.log", sdpInstance)
	fstat, err := os.Stat(verifyLog)
	if err != nil {
		p4m.logger.Debugf("Failed to find p4verify.log: %q, %v", verifyLog, err)
		return
	}
	if fstat.ModTime() == p4m.verifyLogModTime {
		p4m.logger.Debugf("Exiting since p4verify.log file not modified since: %v", p4m.verifyLogModTime)
		p4m.createVerifyMetrics() // Write same values as last time
		p4m.writeMetricsFile()
		return
	}
	p4m.logger.Debugf("p4verify.log last modified: %v", fstat.ModTime())

	// Search latest verify log - if there is a verify in progress the log may not include summary yet (so we will exit and try again next poll)
	reSummaryFound := regexp.MustCompile(`^Status: OK: All scanned depots verified OK.|^Summary of Errors by Type:| Errors:\s+\d+|^Time: Completed verifications at`)
	lines, err := script.File(verifyLog).MatchRegexp(reSummaryFound).Slice()
	if err != nil {
		p4m.logger.Debugf("Exiting since p4verify.log summary not found: %v", err)
		p4m.createVerifyMetrics() // Write same values as last time
		p4m.writeMetricsFile()
		return
	}
	p4m.logger.Debugf("p4verify.log lines: %q", lines)
	p4m.parseVerifyLog(lines)
	p4m.verifyLogModTime = fstat.ModTime()
	p4m.createVerifyMetrics()
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
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
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
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
		return
	}
	p4m.logger.Debugf("serverPositions: %q", serverPositions)
	for _, line := range serverPositions {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		serverID := fields[0]
		journal := fields[1]
		offset := fields[2]
		if _, ok := validServers[serverID]; ok {
			validServers[serverID].journal = journal
			validServers[serverID].offset = offset
		} else {
			p4m.logger.Warningf("Error finding server: %q", serverID)
		}
	}

	for _, s := range validServers {
		if s.journal != "" {
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_replica_curr_jnl",
				help:   "Current journal for server",
				mtype:  "counter",
				value:  s.journal,
				labels: []labelStruct{{name: "servername", value: s.name}}})
		}
		if s.offset != "" {
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_replica_curr_pos",
				help:   "Current offset within for server",
				mtype:  "counter", // Probably should be a gauge but kept for compatibility
				value:  s.offset,
				labels: []labelStruct{{name: "servername", value: s.name}}})
		}
	}
	p4m.writeMetricsFile()
}

func (p4m *P4MonitorMetrics) getPullTransfersAndBytes(pullOutput []string) (int64, int64) {
	// pull -ls output - tagged:
	// ... replicaTransfersActive 0
	// ... replicaTransfersTotal 169
	// ... replicaBytesActive 0
	// ... replicaBytesTotal 460828016
	// ... replicaOldestChange 0
	transfersTotal := int64(-1)
	bytesTotal := int64(-1)
	for _, line := range pullOutput {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		f := fields[1]
		if f == "replicaTransfersTotal" {
			transfersTotal, _ = strconv.ParseInt(fields[2], 10, 64)
		} else if f == "replicaBytesTotal" {
			bytesTotal, _ = strconv.ParseInt(fields[2], 10, 64)
		}
	}
	return transfersTotal, bytesTotal
}

func (p4m *P4MonitorMetrics) monitorPull() {
	// p4 pull metrics - only valid for replica servers
	p4m.startMonitor("monitorPull", "p4_pull")
	if getVar(*p4m.env, "P4REPLICA") != "TRUE" {
		p4m.logger.Debugf("Exiting as not a replica")
		return
	}

	p4cmd, errbuf, p := p4m.newP4CmdPipe("-Ztag pull -ls")
	pullOutput, err := p.Exec(p4cmd).Slice()
	var transfersTotal, bytesTotal int64
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
	} else {
		p4m.logger.Debugf("pull ls: %q", pullOutput)
		transfersTotal, bytesTotal = p4m.getPullTransfersAndBytes(pullOutput)
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_queue_total",
			help:  "Count of p4 pull queue total files",
			mtype: "gauge",
			value: fmt.Sprintf("%d", transfersTotal)})
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_queue_bytes",
			help:  "Count of p4 pull total bytes",
			mtype: "gauge",
			value: fmt.Sprintf("%d", bytesTotal)})
	}

	// Only process pull queue looking for errors if below some magic number - 10k seems reasonable!
	// The reason is that large pull queues tend to thrash and this command produces a lot of output and takes a long time!
	if transfersTotal != -1 && transfersTotal < 10000 {
		tempPullQ := path.Join(p4m.config.MetricsRoot, "pullq.out")
		p4cmd, errbuf, p = p4m.newP4CmdPipe("pull -l")
		_, err = p.Exec(p4cmd).WriteFile(tempPullQ)
		if err != nil {
			p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
			return
		}

		reFailed := regexp.MustCompile(`failed\.$`)
		failedCount, err := script.File(tempPullQ).MatchRegexp(reFailed).CountLines()
		if err != nil {
			p4m.logger.Errorf("Error counting failed pull errors: %v", err)
			p4m.writeMetricsFile()
			return
		}
		otherCount, err := script.File(tempPullQ).RejectRegexp(reFailed).CountLines()
		if err != nil {
			p4m.logger.Errorf("Error counting pull queue: %v", err)
		} else {
			// Old monitor_metrics.sh has p4_pull_errors - but incorrectly as a counter
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_error_count",
				help:  "Count of p4 pull transfers in failed state",
				mtype: "gauge",
				value: fmt.Sprintf("%d", failedCount)})
			// Old monitor_metrics.sh has p4_pull_queue - but incorrectly as a counter
			p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_pull_queue_count",
				help:  "Count of p4 pull files (not in failed state)",
				mtype: "gauge",
				value: fmt.Sprintf("%d", otherCount)})
		}
	}

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
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
		p4m.writeMetricsFile()
		return
	}

	journalRotationsBehind, err := script.File(tempPullStats).Match("... journalRotationsBehind").Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error getting journalRotationsBehind %v", err)
		p4m.writeMetricsFile()
		return
	}
	if journalRotationsBehind == "" {
		journalRotationsBehind = "-1"
	}
	journalBytesBehind, err := script.File(tempPullStats).Match("... journalBytesBehind").Column(3).String()
	if err != nil {
		p4m.logger.Errorf("Error getting journalBytesBehind %v", err)
		p4m.writeMetricsFile()
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
		p4m.writeMetricsFile()
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
		p4m.handleP4Error("Error running %s: %v, err:%q", cmd, err, errbuf)
		return
	}
	p4m.logger.Debugf("Realtime values: %q", lines)
	// Output format examples:
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
		mtype := "gauge"
		if fields[0] == "rtv.db.io.records" || fields[0] == "rtv.svr.sessions.total" {
			mtype = "counter" // For backwards compatibility with monitor_metrics.sh and historical data
		}
		p4m.metrics = append(p4m.metrics, metricStruct{name: name,
			help:  fmt.Sprintf("P4 realtime metric %s", fields[0]),
			mtype: mtype,
			value: fields[3]})
	}
	p4m.writeMetricsFile()
}

// SwarmTaskResponse represents the structure of the Swarm JSON response, excluding pingError
type SwarmTaskResponse struct {
	Authorized     bool   // Whether successfully authorized or not
	Tasks          int    `json:"tasks"`
	FutureTasks    int    `json:"futureTasks"`
	Workers        int    `json:"workers"`
	MaxWorkers     int    `json:"maxWorkers"`
	WorkerLifetime string `json:"workerLifetime"` // We're not particularly interested in this one
}

// SwarmVersionResponse represents the structure of the Swarm version JSON response
type SwarmVersionResponse struct {
	Version string `json:"version"`
	Year    string `json:"year"`
}

// getSwarmVersion retrieves the version from the Swarm API
func (p4m *P4MonitorMetrics) getSwarmVersion(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP request failed with status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}
	var versionResp SwarmVersionResponse
	err = json.Unmarshal(body, &versionResp)
	if err != nil {
		return "", fmt.Errorf("failed to parse JSON: %v", err)
	}

	// Replace escaped slashes with regular slashes
	// "version":"SWARM\/2024.6-MAIN-TEST_ONLY\/2701191 (2025\/01\/07)"
	cleanedVersion := strings.ReplaceAll(versionResp.Version, "\\/", "/")
	return cleanedVersion, nil
}

// getSwarmQueueInfo performs an HTTP request and parses the JSON response
func (p4m *P4MonitorMetrics) getSwarmQueueInfo(url, userid, password string) (*SwarmTaskResponse, error) {
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	p4m.logger.Debugf("SetBasicAuth: '%s/%s'", userid, password)
	req.SetBasicAuth(userid, password)

	var client *http.Client
	if !p4m.config.SwarmSecure {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}

		client = &http.Client{
			Transport: transport,
			// Set a reasonable timeout
			Timeout: 30 * time.Second,
		}
	} else {
		client = &http.Client{ // Set a reasonable timeout
			Timeout: 30 * time.Second,
		}
	}

	p4m.logger.Debugf("req: %v", req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()
	p4m.logger.Debugf("Response: %v", resp)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return &SwarmTaskResponse{Authorized: false}, nil
		} else {
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	// Parse the JSON response
	var response SwarmTaskResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %v", err)
	}
	response.Authorized = true
	return &response, nil
}

func (p4m *P4MonitorMetrics) getSwarmMetrics(urlSwarm string, user string, ticket string) {
	swarmerror := "0"
	urlStatus := fmt.Sprintf("%s/queue/status", urlSwarm)
	p4m.logger.Debugf("urlStatus: %v", urlStatus)
	swarminfo, err := p4m.getSwarmQueueInfo(urlStatus, user, ticket)
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
		value: "0"}
	if swarminfo != nil && swarminfo.Authorized {
		m.value = "1"
		p4m.metrics = append(p4m.metrics, m)
		p4m.logger.Debug("authorized!")
	} else {
		p4m.metrics = append(p4m.metrics, m)
		p4m.logger.Debug("unauthorized!")
	}
	if err != nil {
		p4m.logger.Debugf("error: %v", err)
	}

	urlVersion := fmt.Sprintf("%s/api/version", urlSwarm)
	p4m.logger.Debugf("urlVersion: '%s'", urlVersion)
	versionString, err := p4m.getSwarmVersion(urlVersion)
	if err != nil {
		p4m.logger.Errorf("Error getting Swarm status %s: %v", urlVersion, err)
	} else {
		p4m.metrics = append(p4m.metrics, metricStruct{name: "p4_swarm_version",
			help:   "P4 Swarm version string",
			mtype:  "gauge",
			value:  "1",
			labels: []labelStruct{{name: "version", value: versionString}}})
	}
	if swarminfo == nil {
		swarminfo = &SwarmTaskResponse{} // Blanks
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

func (p4m *P4MonitorMetrics) monitorSwarm() {
	// Find Swarm URL and get information from it
	p4m.startMonitor("monitorSwarm", "p4_swarm")

	p4cmd, errbuf, p := p4m.newP4CmdPipe("-ztag info -s")
	authID, err := p.Exec(p4cmd).Match("... serverCluster").Column(3).String()
	if err != nil {
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
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
		p4m.handleP4Error("Error running %s: %v, err:%q", p4cmd, err, errbuf)
		return
	}
	ticket = strings.TrimSpace(ticket)
	p4m.logger.Debugf("ticket: '%s'", ticket)

	// Get Swarm URL from config file or property
	// Note that if set in config file we assume this might be different for a reason from the property
	urlSwarm := p4m.config.SwarmURL
	if urlSwarm != "" {
		p4m.logger.Debugf("Using Swarm URL from config file: '%s'", urlSwarm)
	} else {
		p4cmd, errbuf, p = p4m.newP4CmdPipe("property -l")
		urlSwarm, err = p.Exec(p4cmd).Match("P4.Swarm.URL =").Column(3).String()
		if err != nil {
			p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
			return
		}
		if urlSwarm == "" {
			p4m.logger.Warningf("No Swarm property")
			return
		}
	}
	urlSwarm = strings.TrimSpace(urlSwarm)
	urlSwarm = strings.TrimSuffix(urlSwarm, "/")
	p4m.logger.Debugf("Swarm url: '%s'", urlSwarm)
	p4m.getSwarmMetrics(urlSwarm, p4m.p4User, ticket)
	if len(p4m.metrics) > 0 {
		p4m.writeMetricsFile()
	}
}

func (p4m *P4MonitorMetrics) monitorErrors() {
	p4m.startMonitor("monitorErrors", "p4_errors")
	p4m.errLock.Lock()
	defer p4m.errLock.Unlock()
	for m, count := range p4m.errorMetrics {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_errors_count",
				help:  "P4D error count by subsystem and level",
				mtype: "counter",
				value: fmt.Sprintf("%d", count),
				labels: []labelStruct{{name: "subsys", value: m.Subsystem},
					{name: "severity", value: m.Severity},
				}})
	}
	p4m.writeMetricsFile()
}

func loadConfigFile(logger *logrus.Logger, configFileName string, sdpInstance string, p4port string, p4user string, p4config string) (*config.Config, error) {
	logger.Debugf("Loading config file: %q", configFileName)
	cfg, err := config.LoadConfigFile(configFileName)
	if err != nil {
		logger.Errorf("error loading config file: %v", err)
		return nil, err
	}
	if len(sdpInstance) > 0 {
		cfg.SDPInstance = sdpInstance
	}

	logger.Infof("%v", version.Print("p4metrics"))
	logger.Infof("Config: %+v", *cfg)
	logger.Infof("Processing: output to '%s' SDP instance '%s'",
		cfg.MetricsRoot, cfg.SDPInstance)

	if p4port != "" {
		if cfg.SDPInstance != "" {
			logger.Warnf("SDP instance %q specified so ignoring --p4port: %q", cfg.SDPInstance, p4port)
		} else {
			if cfg.P4Port != "" {
				logger.Warnf("--p4port %q overwriting config value %q", p4port, cfg.P4Port)
			}
			cfg.P4Port = p4port
		}
	}
	if p4user != "" {
		if cfg.SDPInstance != "" {
			logger.Warnf("SDP instance %q specified so ignoring --p4user: %q", cfg.SDPInstance, p4user)
		} else {
			if cfg.P4User != "" {
				logger.Warnf("--p4user %q overwriting config value %q", p4user, cfg.P4User)
			}
			cfg.P4User = p4user
		}
	}
	if p4config != "" {
		if cfg.SDPInstance != "" {
			logger.Warnf("SDP instance %q specified so ignoring --p4config: %q", cfg.SDPInstance, p4config)
		} else {
			if cfg.P4Config != "" {
				logger.Warnf("--p4config %q overwriting config value %q", p4config, cfg.P4Config)
			}
			cfg.P4Config = p4config
		}
	}
	return cfg, err
}

func (p4m *P4MonitorMetrics) runMonitorFunctions() {
	// This is called in a loop by the ticker - allow for p4d service to go down and up
	// So reconnect to p4d if necessary
	p4m.logger.Debug("Running monitor functions")
	if !p4m.initialised {
		p4m.logger.Debug("Running initVars")
		p4m.initVars()
		if !p4m.initialised {
			p4m.monitorMonitoring()
			p4m.logger.Warnf("Failed to initialise P4MonitorMetrics")
		}

		// Manages its own updates on a seperate thread because of log tailing
		if p4m.initialised {
			go func() {
				p4m.setupErrorMonitoring()
			}()
		}
	}

	if p4m.config.MonitorSwarm {
		p4m.monitorSwarm()
	}
	p4m.monitorUptime()
	p4m.monitorChange()
	p4m.monitorCheckpoint()
	p4m.monitorJournalAndLogs()
	p4m.monitorFilesys()
	p4m.monitorHelixAuthSvc()
	p4m.monitorLicense()
	p4m.monitorProcesses()
	p4m.monitorReplicas()
	p4m.monitorSSL()
	p4m.monitorPull()
	p4m.monitorRealTime()
	p4m.monitorVersions()
	p4m.monitorVerify()
	p4m.monitorErrors()
	p4m.monitorMonitoring()
}

func main() {
	var (
		configFilename = kingpin.Flag(
			"config",
			"Config file for p4prometheus.",
		).Short('c').Default("p4metrics.yaml").String()
		sdpInstance = kingpin.Flag(
			"sdp.instance",
			"SDP Instance, typically 1 or alphanumeric.",
		).Default("").String()
		p4port = kingpin.Flag(
			"p4port",
			"P4PORT to use (if sdp.instance is not set).",
		).Default("").String()
		p4user = kingpin.Flag(
			"p4user",
			"P4USER to use (if sdp.instance is not set).",
		).Default("").String()
		p4config = kingpin.Flag(
			"p4config",
			"P4CONFIG file to use (if sdp.instance is not set and no value in config file).",
		).Default("").String()
		debug = kingpin.Flag(
			"debug",
			"Enable debugging.",
		).Bool()
		dryrun = kingpin.Flag(
			"dry.run",
			"Don't write metrics - but show the results - useful for debugging with --debug.",
		).Short('n').Bool()
		sampleConfig = kingpin.Flag(
			"sample.config",
			"Output a sample config file and exit. Useful for getting started to create p4metrics.yaml. E.g. p4metrics --sample.config > p4metrics.yaml",
		).Short('C').Bool()
	)

	kingpin.Version(version.Print("p4metrics"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	if *sampleConfig {
		fmt.Print(config.SampleConfig)
		return
	}

	logger := logrus.New()
	logger.Level = logrus.InfoLevel
	if *debug {
		logger.Level = logrus.DebugLevel
	}

	cfg, err := loadConfigFile(logger, *configFilename, *sdpInstance, *p4port, *p4user, *p4config)
	if err != nil {
		logger.Fatalf("Failed to load config file: %v", err)
	}

	err = os.MkdirAll(cfg.MetricsRoot, 0755) // Check dir exists
	if err != nil {
		logger.Fatalf("Failed to create MetricsRoot: %q, %v", cfg.MetricsRoot, err)
	}

	var env map[string]string
	if cfg.SDPInstance != "" {
		env = sourceSDPVars(cfg.SDPInstance, logger)
	} else {
		env = sourceEnvVars()
	}
	p4m := newP4MonitorMetrics(cfg, &env, logger)
	p4m.version = version.Version
	if *dryrun {
		p4m.dryrun = true
	}

	ticker := time.NewTicker(p4m.config.UpdateInterval)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	p4m.runMonitorFunctions()
	for {
		select {
		case sig := <-sigs:
			if sig == syscall.SIGHUP {
				p4m.logger.Debug("Received signal SIGHUP, reloading config and calling runMonitorFunctions")
				cfg, err := loadConfigFile(logger, *configFilename, *sdpInstance, *p4port, *p4user, *p4config)
				if err != nil {
					logger.Errorf("Failed to load config file: %v", err)
					break
				}
				if cfg.SDPInstance != "" {
					env = sourceSDPVars(cfg.SDPInstance, logger)
				} else {
					env = sourceEnvVars()
				}
				p4m.config = cfg
				p4m.env = &env
				p4m.initialised = false // Force re-init
				ticker = time.NewTicker(p4m.config.UpdateInterval)
				p4m.runMonitorFunctions()
			} else {
				p4m.logger.Infof("Terminating due to signal %v", sig)
				if p4m.errTailer != nil {
					(*p4m.errTailer).Close() // Stop the error tailer if running
				}
				return
			}
		case <-ticker.C:
			p4m.runMonitorFunctions()
		}
	}
}
