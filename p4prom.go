package main

// This command line utility builds on top of the p4d log analyzer
// and outputs Prometheus metrics in a single file to be picked up by
// node_exporter's textfile.collector module.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/machinebox/progress"
	p4dlog "github.com/rcowham/go-libp4dlog"
	"github.com/rcowham/go-libtail/tailer"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/go-libtail/tailer/glob"
	"github.com/rcowham/p4prometheus/config"
	"github.com/rcowham/p4prometheus/version"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/sirupsen/logrus"
)

var blankTime time.Time

// Structure for use with libtail
type logConfig struct {
	Type                 string
	Path                 string
	PollInterval         time.Duration
	Readall              bool
	FailOnMissingLogfile bool
}

// P4Prometheus structure
type P4Prometheus struct {
	config              *config.Config
	historical          bool
	logger              *logrus.Logger
	fp                  *p4dlog.P4dFileParser
	metricWriter        io.Writer
	cmdCounter          map[string]int32
	cmdCumulative       map[string]float64
	cmdByUserCounter    map[string]int32
	cmdByUserCumulative map[string]float64
	totalReadWait       map[string]float64
	totalReadHeld       map[string]float64
	totalWriteWait      map[string]float64
	totalWriteHeld      map[string]float64
	totalTriggerLapse   map[string]float64
	cmdsProcessed       int64
	linesRead           int64
	metrics             chan string
	lines               chan []byte
	cmdchan             chan p4dlog.Command
	lastOutputTime      time.Time
}

func newP4Prometheus(config *config.Config, logger *logrus.Logger, historical bool) (p4p *P4Prometheus) {
	return &P4Prometheus{
		config:              config,
		logger:              logger,
		historical:          historical,
		lines:               make(chan []byte, 10000),
		metrics:             make(chan string, 100),
		cmdchan:             make(chan p4dlog.Command, 10000),
		cmdCounter:          make(map[string]int32),
		cmdCumulative:       make(map[string]float64),
		cmdByUserCounter:    make(map[string]int32),
		cmdByUserCumulative: make(map[string]float64),
		totalReadWait:       make(map[string]float64),
		totalReadHeld:       make(map[string]float64),
		totalWriteWait:      make(map[string]float64),
		totalWriteHeld:      make(map[string]float64),
		totalTriggerLapse:   make(map[string]float64),
	}
}

func byteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

func (p4p *P4Prometheus) printMetricHeader(f io.Writer, name string, help string, metricType string) {
	if !p4p.historical {
		fmt.Fprintf(f, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
	}
}

type labelStruct struct {
	name  string
	value string
}

// Prometheus format: 	metric_name{label1="val1",label2="val2"}
// Graphite format:  	metric_name;label1=val1;label2=val2
func (p4p *P4Prometheus) formatLabels(mname string, labels []labelStruct) string {
	nonBlankLabels := make([]labelStruct, 0)
	for _, l := range labels {
		if l.value != "" {
			if !p4p.historical {
				l.value = fmt.Sprintf("\"%s\"", l.value)
			}
			nonBlankLabels = append(nonBlankLabels, l)
		}
	}
	vals := make([]string, 0)
	for _, l := range nonBlankLabels {
		vals = append(vals, fmt.Sprintf("%s=%s", l.name, l.value))
	}
	if p4p.historical {
		labelStr := strings.Join(vals, ";")
		return fmt.Sprintf("%s;%s", mname, labelStr)
	}
	labelStr := strings.Join(vals, ",")
	return fmt.Sprintf("%s{%s}", mname, labelStr)
}

func (p4p *P4Prometheus) formatMetric(mname string, labels []labelStruct, metricVal string) string {
	if p4p.historical {
		return fmt.Sprintf("%s %s %d\n", p4p.formatLabels(mname, labels),
			metricVal, p4p.lastOutputTime.Unix())
	}
	return fmt.Sprintf("%s %s\n", p4p.formatLabels(mname, labels), metricVal)
}

// Publish cumulative results - called on a ticker or in historical mode
func (p4p *P4Prometheus) getCumulativeMetrics() string {
	fixedLabels := []labelStruct{{name: "serverid", value: p4p.config.ServerID},
		{name: "sdpinst", value: p4p.config.SDPInstance}}
	metrics := new(bytes.Buffer)
	p4p.logger.Infof("Writing stats\n")
	p4p.lastOutputTime = time.Now()

	var mname string
	var buf string
	var metricVal string
	mname = "p4_prom_log_lines_read"
	p4p.printMetricHeader(metrics, mname, "A count of log lines read", "counter")
	metricVal = fmt.Sprintf("%d", p4p.linesRead)
	buf = p4p.formatMetric(mname, fixedLabels, metricVal)
	p4p.logger.Debugf(buf)
	fmt.Fprint(metrics, buf)

	mname = "p4_prom_cmds_processed"
	p4p.printMetricHeader(metrics, mname, "A count of all cmds processed", "counter")
	metricVal = fmt.Sprintf("%d", p4p.cmdsProcessed)
	buf = p4p.formatMetric(mname, fixedLabels, metricVal)
	p4p.logger.Debugf(buf)
	fmt.Fprint(metrics, buf)

	mname = "p4_prom_cmds_pending"
	p4p.printMetricHeader(metrics, mname, "A count of all current cmds (not completed)", "gauge")
	metricVal = fmt.Sprintf("%d", p4p.fp.CmdsPendingCount())
	buf = p4p.formatMetric(mname, fixedLabels, metricVal)
	p4p.logger.Debugf(buf)
	fmt.Fprint(metrics, buf)

	mname = "p4_cmd_counter"
	p4p.printMetricHeader(metrics, mname, "A count of completed p4 cmds (by cmd)", "counter")
	for cmd, count := range p4p.cmdCounter {
		metricVal = fmt.Sprintf("%d", count)
		labels := append(fixedLabels, labelStruct{"cmd", cmd})
		buf = p4p.formatMetric(mname, labels, metricVal)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	mname = "p4_cmd_cumulative_seconds"
	p4p.printMetricHeader(metrics, mname, "The total in seconds (by cmd)", "counter")
	for cmd, lapse := range p4p.cmdCumulative {
		metricVal = fmt.Sprintf("%0.3f", lapse)
		labels := append(fixedLabels, labelStruct{"cmd", cmd})
		buf = p4p.formatMetric(mname, labels, metricVal)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	// For large sites this might not be sensible - so they can turn it off
	if p4p.config.OutputCmdsByUser {
		mname = "p4_cmd_user_counter"
		p4p.printMetricHeader(metrics, mname, "A count of completed p4 cmds (by user)", "counter")
		for user, count := range p4p.cmdByUserCounter {
			metricVal = fmt.Sprintf("%d", count)
			labels := append(fixedLabels, labelStruct{"user", user})
			buf = p4p.formatMetric(mname, labels, metricVal)
			p4p.logger.Debugf(buf)
			fmt.Fprint(metrics, buf)
		}
		mname = "p4_cmd_user_cumulative_seconds"
		p4p.printMetricHeader(metrics, mname, "The total in seconds (by user)", "counter")
		for user, lapse := range p4p.cmdByUserCumulative {
			metricVal = fmt.Sprintf("%0.3f", lapse)
			labels := append(fixedLabels, labelStruct{"user", user})
			buf = p4p.formatMetric(mname, labels, metricVal)
			p4p.logger.Debugf(buf)
			fmt.Fprint(metrics, buf)
		}
	}
	mname = "p4_total_read_wait_seconds"
	p4p.printMetricHeader(metrics, mname,
		"The total waiting for read locks in seconds (by table)", "counter")
	for table, total := range p4p.totalReadWait {
		metricVal = fmt.Sprintf("%0.3f", total)
		labels := append(fixedLabels, labelStruct{"table", table})
		buf = p4p.formatMetric(mname, labels, metricVal)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	mname = "p4_total_read_held_seconds"
	p4p.printMetricHeader(metrics, mname,
		"The total read locks held in seconds (by table)", "counter")
	for table, total := range p4p.totalReadHeld {
		metricVal = fmt.Sprintf("%0.3f", total)
		labels := append(fixedLabels, labelStruct{"table", table})
		buf = p4p.formatMetric(mname, labels, metricVal)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	mname = "p4_total_write_wait_seconds"
	p4p.printMetricHeader(metrics, mname,
		"The total waiting for write locks in seconds (by table)", "counter")
	for table, total := range p4p.totalWriteWait {
		metricVal = fmt.Sprintf("%0.3f", total)
		labels := append(fixedLabels, labelStruct{"table", table})
		buf = p4p.formatMetric(mname, labels, metricVal)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	mname = "p4_total_write_held_seconds"
	p4p.printMetricHeader(metrics, mname,
		"The total write locks held in seconds (by table)", "counter")
	for table, total := range p4p.totalWriteHeld {
		metricVal = fmt.Sprintf("%0.3f", total)
		labels := append(fixedLabels, labelStruct{"table", table})
		buf = p4p.formatMetric(mname, labels, metricVal)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	if len(p4p.totalTriggerLapse) > 0 {
		mname = "p4_total_trigger_lapse_seconds"
		p4p.printMetricHeader(metrics, mname,
			"The total lapse time for triggers in seconds (by trigger)", "counter")
		for table, total := range p4p.totalTriggerLapse {
			metricVal = fmt.Sprintf("%0.3f", total)
			labels := append(fixedLabels, labelStruct{"trigger", table})
			buf = p4p.formatMetric(mname, labels, metricVal)
			p4p.logger.Debugf(buf)
			fmt.Fprint(metrics, buf)
		}
	}
	return metrics.String()
}

func (p4p *P4Prometheus) getSeconds(tmap map[string]interface{}, fieldName string) float64 {
	p4p.logger.Debugf("field %s %v, %v\n", fieldName, reflect.TypeOf(tmap[fieldName]), tmap[fieldName])
	if total, ok := tmap[fieldName].(float64); ok {
		return (total)
	}
	return 0
}

func (p4p *P4Prometheus) getMilliseconds(tmap map[string]interface{}, fieldName string) float64 {
	p4p.logger.Debugf("field %s %v, %v\n", fieldName, reflect.TypeOf(tmap[fieldName]), tmap[fieldName])
	if total, ok := tmap[fieldName].(float64); ok {
		return (total / 1000)
	}
	return 0
}

func (p4p *P4Prometheus) publishEvent(cmd p4dlog.Command) {
	p4p.logger.Debugf("publish cmd: %s\n", cmd.String())

	p4p.cmdCounter[string(cmd.Cmd)]++
	p4p.cmdCumulative[string(cmd.Cmd)] += float64(cmd.CompletedLapse)
	user := string(cmd.User)
	if !p4p.config.CaseSensitiveServer {
		user = strings.ToLower(user)
	}
	p4p.cmdByUserCounter[user]++
	p4p.cmdByUserCumulative[user] += float64(cmd.CompletedLapse)
	const triggerPrefix = "trigger_"

	for _, t := range cmd.Tables {
		if len(t.TableName) > len(triggerPrefix) && t.TableName[:len(triggerPrefix)] == triggerPrefix {
			triggerName := t.TableName[len(triggerPrefix):]
			p4p.totalTriggerLapse[triggerName] += float64(t.TriggerLapse)
		} else {
			p4p.totalReadHeld[t.TableName] += float64(t.TotalReadHeld) / 1000
			p4p.totalReadWait[t.TableName] += float64(t.TotalReadWait) / 1000
			p4p.totalWriteHeld[t.TableName] += float64(t.TotalWriteHeld) / 1000
			p4p.totalWriteWait[t.TableName] += float64(t.TotalWriteWait) / 1000
		}
	}
}

// ProcessEvents - main event loop for P4Prometheus - reads lines and outputs metrics
func (p4p *P4Prometheus) ProcessEvents(ctx context.Context,
	lines <-chan []byte, metrics chan<- string) int {
	ticker := time.NewTicker(p4p.config.UpdateInterval)
	for {
		select {
		case <-ctx.Done():
			p4p.logger.Debugf("Done received")
			close(metrics)
			return -1
		case <-ticker.C:
			p4p.logger.Debugf("publishCumulative")
			metrics <- p4p.getCumulativeMetrics()
		case cmd, ok := <-p4p.cmdchan:
			if ok {
				p4p.logger.Debugf("Publishing cmd: %s", cmd.String())
				p4p.cmdsProcessed++
				p4p.publishEvent(cmd)
			} else {
				metrics <- p4p.getCumulativeMetrics()
				close(metrics)
				return 0
			}
		case line, ok := <-lines:
			if ok {
				p4p.logger.Debugf("Line: %s", line)
				p4p.linesRead++
				p4p.lines <- []byte(line)
			} else {
				p4p.logger.Debugf("Tailer closed")
				if p4p.lines != nil {
					close(p4p.lines)
					p4p.lines = nil
				} else {
					time.Sleep(100 * time.Millisecond)
				}
				// We wait for the parser to close cmdchan before returning
			}
		}
	}
}

// Reads server id for SDP instance
func readServerID(logger *logrus.Logger, instance string) string {
	idfile := fmt.Sprintf("/p4/%s/root/server.id", instance)
	if _, err := os.Stat(idfile); err == nil {
		buf, err := ioutil.ReadFile(idfile) // just pass the file name
		if err != nil {
			logger.Errorf("Failed to read %v - %v", idfile, err)
			return ""
		}
		return string(bytes.TrimRight(buf, " \r\n"))
	}
	return ""
}

// Writes metrics to appropriate file
func (p4p *P4Prometheus) writeMetricsFile(metrics []byte) {
	f, err := os.Create(p4p.config.MetricsOutput)
	if err != nil {
		p4p.logger.Errorf("Error opening %s: %v", p4p.config.MetricsOutput, err)
		return
	}
	f.Write(metrics)
	err = f.Close()
	if err != nil {
		p4p.logger.Errorf("Error closing file: %v", err)
	}
	err = os.Chmod(p4p.config.MetricsOutput, 0644)
	if err != nil {
		p4p.logger.Errorf("Error chmod-ing file: %v", err)
	}
}

// Returns a tailer object for specified file
func getTailer(cfgInput *logConfig, logger *logrus.Logger) (fswatcher.FileTailer, error) {

	var tail fswatcher.FileTailer
	g, err := glob.FromPath(cfgInput.Path)
	if err != nil {
		return nil, err
	}
	switch {
	case cfgInput.Type == "file":
		if cfgInput.PollInterval == 0 {
			tail, err = fswatcher.RunFileTailer([]glob.Glob{g}, cfgInput.Readall, cfgInput.FailOnMissingLogfile, logger)
		} else {
			tail, err = fswatcher.RunPollingFileTailer([]glob.Glob{g}, cfgInput.Readall, cfgInput.FailOnMissingLogfile, cfgInput.PollInterval, logger)
		}
	case cfgInput.Type == "stdin":
		tail = tailer.RunStdinTailer()
	default:
		return nil, fmt.Errorf("config error: Input type '%v' unknown", cfgInput.Type)
	}
	return tail, nil
}

func runLogTailer(logger *logrus.Logger, logcfg *logConfig, cfg *config.Config, debug bool) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tailer, err := getTailer(logcfg, logger)
	if err != nil {
		logger.Errorf("error starting to tail log lines: %v", err)
		os.Exit(-2)
	}

	// Setup P4Prometheus object and a file parser
	p4p := newP4Prometheus(cfg, logger, false)
	fp := p4dlog.NewP4dFileParser(logger)
	p4p.fp = fp
	if debug {
		fp.SetDebugMode()
	}

	metrics := make(chan string, 100)
	lines := make(chan []byte, 100)
	go fp.LogParser(ctx, p4p.lines, p4p.cmdchan)
	go p4p.ProcessEvents(ctx, lines, metrics)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Infof("Terminating - signal %v", sig)
		tailer.Close()
		cancel()
	}()

	p4p.linesRead = 0
	for {
		select {
		case metric, ok := <-metrics:
			if ok {
				p4p.writeMetricsFile([]byte(metric))
			} else {
				os.Exit(0)
			}
		case line, ok := <-tailer.Lines():
			if ok {
				p4p.linesRead++
				p4p.lines <- []byte(line.Line)
			} else {
				os.Exit(0)
			}
		case err := <-tailer.Errors():
			if err != nil {
				if os.IsNotExist(err.Cause()) {
					p4p.logger.Errorf("error reading log lines: %v: use 'fail_on_missing_logfile: false' in the input configuration if you want p4prometheus to start even though the logfile is missing", err)
					os.Exit(-3)
				}
				p4p.logger.Errorf("error reading log lines: %v", err)
				os.Exit(-4)
			}
			os.Exit(0)
		}
	}
}

func readerFromFile(file *os.File) (io.Reader, int64, error) {
	//create a bufio.Reader so we can 'peek' at the first few bytes
	bReader := bufio.NewReader(file)
	testBytes, err := bReader.Peek(64) //read a few bytes without consuming
	if err != nil {
		return nil, 0, err
	}
	var fileSize int64
	stat, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	fileSize = stat.Size()

	//Detect if the content is gzipped
	contentType := http.DetectContentType(testBytes)
	if strings.Contains(contentType, "x-gzip") {
		gzipReader, err := gzip.NewReader(bReader)
		if err != nil {
			return nil, 0, err
		}
		// Estimate filesize
		return gzipReader, fileSize * 20, nil
	}
	return bReader, fileSize, nil
}

// Process existing log file and exit when finished
func processHistoricalLog(logger *logrus.Logger, cfg *config.Config, logfile string, debug bool) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup P4Prometheus object and a file parser
	p4p := newP4Prometheus(cfg, logger, true)
	fp := p4dlog.NewP4dFileParser(logger)
	p4p.fp = fp
	if debug {
		fp.SetDebugMode()
	}

	metrics := make(chan string, 100)
	lines := make(chan []byte, 100)
	go fp.LogParser(ctx, p4p.lines, p4p.cmdchan)
	go p4p.ProcessEvents(ctx, lines, metrics)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Infof("Terminating - signal %v", sig)
	}()

	var file *os.File
	if logfile == "-" {
		file = os.Stdin
	} else {
		var err error
		file, err = os.Open(logfile)
		if err != nil {
			logger.Fatal(err)
		}
	}
	defer file.Close()

	const maxCapacity = 1024 * 1024
	inbuf := make([]byte, maxCapacity)
	reader, fileSize, err := readerFromFile(file)
	if err != nil {
		logger.Fatalf("Failed to open file: %v", err)
	}
	logger.Debugf("Opened %s, size %v", logfile, fileSize)
	reader = bufio.NewReaderSize(reader, maxCapacity)
	preader := progress.NewReader(reader)
	scanner := bufio.NewScanner(preader)
	scanner.Buffer(inbuf, maxCapacity)

	// Start a goroutine printing progress
	go func() {
		d := 1 * time.Second
		if fileSize > 1*1000*1000*1000 {
			d = 10 * time.Second
		}
		if fileSize > 10*1000*1000*1000 {
			d = 30 * time.Second
		}
		if fileSize > 25*1000*1000*1000 {
			d = 60 * time.Second
		}
		logger.Infof("Report duration: %v", d)
		progressChan := progress.NewTicker(ctx, preader, fileSize, d)
		for p := range progressChan {
			fmt.Fprintf(os.Stderr, "%s: %s/%s %.0f%% estimated finish %s, %v remaining...\n",
				logfile, byteCountDecimal(p.N()), byteCountDecimal(fileSize),
				p.Percent(), p.Estimated().Format("15:04:05"),
				p.Remaining().Round(time.Second))
		}
	}()

	p4p.linesRead = 0

	// Start a goroutine printing progress
	go func() {
		for scanner.Scan() {
			p4p.linesRead++
			p4p.lines <- scanner.Bytes()
		}
		fmt.Fprintln(os.Stderr, "\nprocessing completed")
	}()

	for {
		select {
		case metric, ok := <-metrics:
			if ok {
				p4p.writeMetricsFile([]byte(metric))
			} else {
				os.Exit(0)
			}
		}
	}
}

func main() {
	// for profiling
	// defer profile.Start().Stop()
	var (
		configfile = kingpin.Flag(
			"config",
			"Config file for p4prometheus.",
		).Default("p4prometheus.yaml").String()
		debug = kingpin.Flag(
			"debug",
			"Enable debugging.",
		).Bool()
		logPath = kingpin.Flag(
			"log.path",
			"Log file to processe (if not specified in config file).",
		).String()
		serverID = kingpin.Flag(
			"server.id",
			"server id if required in metrics.",
		).String()
		sdpInstance = kingpin.Flag(
			"sdp.instance",
			"SDP instance if required in metrics.",
		).String()
		updateInterval = kingpin.Flag(
			"update.interval",
			"Update interval for metrics.",
		).Default("10s").Duration()
		noOutputCmdsByUser = kingpin.Flag(
			"no.output.cmds.by.user",
			"Update interval for metrics.",
		).Default("false").Bool()
		caseInsensitiveServer = kingpin.Flag(
			"case.insensitive.server",
			"Set if server is case insensitive.",
		).Default("false").Bool()
		historical = kingpin.Flag(
			"historical",
			"Enable historical output in VictoriaMetrics format (via Graphite interface).",
		).Bool()
		historicalMetricsFile = kingpin.Flag(
			"historical.metrics.file",
			"File to write historical metrics to in Graphite format for use with VictoriaMetrics.",
		).Default("history.txt").String()
	)

	kingpin.Version(version.Print("p4prometheus"))
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
	if len(*logPath) > 0 {
		cfg.LogPath = *logPath
	}
	if len(*serverID) > 0 {
		cfg.ServerID = *serverID
	}
	if len(*sdpInstance) > 0 {
		cfg.SDPInstance = *sdpInstance
	}
	if *updateInterval != 10*time.Second {
		cfg.UpdateInterval = *updateInterval
	}
	if !*noOutputCmdsByUser {
		cfg.OutputCmdsByUser = !*noOutputCmdsByUser
	}
	if *caseInsensitiveServer {
		cfg.CaseSensitiveServer = !*caseInsensitiveServer
	}
	if *historical && len(*historicalMetricsFile) == 0 {
		logger.Errorf("error in parameters - must specify --historical.metrics.file when --historical is set")
		os.Exit(-1)
	}
	logger.Infof("Processing log file: '%s' output to '%s' SDP instance '%s'\n",
		cfg.LogPath, cfg.MetricsOutput, cfg.SDPInstance)
	if cfg.SDPInstance == "" && len(cfg.ServerID) == 0 {
		logger.Errorf("error loading config file - if no sdp_instance then please specifiy server_id!")
		os.Exit(-1)
	}
	if len(cfg.ServerID) == 0 && cfg.SDPInstance != "" {
		cfg.ServerID = readServerID(logger, cfg.SDPInstance)
	}
	logger.Infof("Server id: '%s'\n", cfg.ServerID)

	var logcfg *logConfig

	//---------------
	if *historical {
		processHistoricalLog(logger, cfg, *logPath, *debug)
	} else {
		logcfg = &logConfig{
			Type:                 "file",
			Path:                 cfg.LogPath,
			PollInterval:         time.Second * 1,
			Readall:              false,
			FailOnMissingLogfile: false,
		}
		runLogTailer(logger, logcfg, cfg, *debug)
	}

}
