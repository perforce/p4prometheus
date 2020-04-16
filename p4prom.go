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
	"strings"
	"syscall"
	"time"

	"github.com/machinebox/progress"
	"github.com/perforce/p4prometheus/config"
	"github.com/perforce/p4prometheus/version"
	p4dlog "github.com/rcowham/go-libp4dlog"
	metrics "github.com/rcowham/go-libp4dlog/metrics"
	"github.com/rcowham/go-libtail/tailer"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/go-libtail/tailer/glob"

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
	timeLatestStartCmd  time.Time
	latestStartCmdBuf   []byte
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

// GO standard reference value/format: Mon Jan 2 15:04:05 -0700 MST 2006
const p4timeformat = "2006/01/02 15:04:05"

func newP4Prometheus(config *config.Config, logger *logrus.Logger, historical bool) (p4p *P4Prometheus) {
	return &P4Prometheus{
		config:     config,
		logger:     logger,
		historical: historical,
		lines:      make(chan []byte, 10000),
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
	var f *os.File
	var err error
	if p4p.historical {
		f, err = os.OpenFile(p4p.config.MetricsOutput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			p4p.logger.Errorf("Error opening %s: %v", p4p.config.MetricsOutput, err)
			return
		}
	} else {
		f, err = os.Create(p4p.config.MetricsOutput)
		if err != nil {
			p4p.logger.Errorf("Error opening %s: %v", p4p.config.MetricsOutput, err)
			return
		}
	}
	f.Write(metrics)
	err = f.Close()
	if err != nil {
		p4p.logger.Errorf("Error closing file: %v", err)
	}
	if !p4p.historical {
		err = os.Chmod(p4p.config.MetricsOutput, 0644)
		if err != nil {
			p4p.logger.Errorf("Error chmod-ing file: %v", err)
		}
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

	mconfig := &metrics.Config{
		Debug:               debug,
		ServerID:            cfg.ServerID,
		SDPInstance:         cfg.SDPInstance,
		UpdateInterval:      cfg.UpdateInterval,
		OutputCmdsByUser:    cfg.OutputCmdsByUser,
		CaseSensitiveServer: cfg.CaseSensitiveServer,
	}
	mp := metrics.NewP4DMetricsLogParser(mconfig, logger, false)

	linesChan := make(chan string, 10000)
	cmdChan, metricsChan := mp.ProcessEvents(ctx, linesChan)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Infof("Terminating - signal %v", sig)
		tailer.Close()
		cancel()
	}()

	// Process all commands - need to consume them even if we ignore them (overhead is minimal)
	go func() {
		for range cmdChan {
		}
		logger.Debug("Main: metrics closed")
	}()

	p4p.linesRead = 0
	for {
		select {
		case metric, ok := <-metricsChan:
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

	mconfig := &metrics.Config{
		Debug:               debug,
		ServerID:            cfg.ServerID,
		SDPInstance:         cfg.SDPInstance,
		UpdateInterval:      cfg.UpdateInterval,
		OutputCmdsByUser:    cfg.OutputCmdsByUser,
		CaseSensitiveServer: cfg.CaseSensitiveServer,
	}
	mp := metrics.NewP4DMetricsLogParser(mconfig, logger, false)

	linesChan := make(chan string, 10000)
	cmdChan, metricsChan := mp.ProcessEvents(ctx, linesChan)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Infof("Terminating - signal %v", sig)
		os.Exit(-1)
	}()

	startTime := time.Now()
	logger.Infof("Starting %s", startTime)

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

	// Process all commands - need to consume them even if we ignore them (overhead is minimal)
	go func() {
		for range cmdChan {
		}
		logger.Debug("Main: metrics closed")
	}()

	// Start a goroutine processing all lines in file
	go func() {
		for scanner.Scan() {
			p4p.linesRead++
			if p4p.linesRead%100 == 0 {
				logger.Debugf("Lines read %d", p4p.linesRead)
			}
			linesChan <- scanner.Text()
		}
		close(linesChan)
		fmt.Fprintln(os.Stderr, "\nprocessing completed")
		logger.Infof("Completed %s, elapsed %s", time.Now(), time.Since(startTime))
	}()

	for {
		select {
		case metric, ok := <-metricsChan:
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
	if *historical {
		cfg.MetricsOutput = *historicalMetricsFile
	}
	logger.Infof("Processing log file: '%s' output to '%s' SDP instance '%s'",
		cfg.LogPath, cfg.MetricsOutput, cfg.SDPInstance)
	if cfg.SDPInstance == "" && len(cfg.ServerID) == 0 {
		logger.Errorf("error loading config file - if no sdp_instance then please specifiy server_id!")
		os.Exit(-1)
	}
	if len(cfg.ServerID) == 0 && cfg.SDPInstance != "" {
		cfg.ServerID = readServerID(logger, cfg.SDPInstance)
	}
	logger.Infof("Server id: '%s'", cfg.ServerID)

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
