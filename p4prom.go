package main

// This command line utility builds on top of the p4d log analyzer
// and outputs Prometheus metrics in a single file to be picked up by
// node_exporter's textfile.collector module.

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/perforce/p4prometheus/config"
	"github.com/perforce/p4prometheus/version"
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
	config *config.Config
	logger *logrus.Logger
}

// GO standard reference value/format: Mon Jan 2 15:04:05 -0700 MST 2006
const p4timeformat = "2006/01/02 15:04:05"

func newP4Prometheus(config *config.Config, logger *logrus.Logger) (p4p *P4Prometheus) {
	return &P4Prometheus{
		config: config,
		logger: logger,
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
	var f *os.File
	var err error
	f, err = os.Create(p4p.config.MetricsOutput)
	if err != nil {
		p4p.logger.Errorf("Error opening %s: %v", p4p.config.MetricsOutput, err)
		return
	}
	f.Write(bytes.ToValidUTF8(metrics, []byte{'?'}))
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
	var parsedGlobs []glob.Glob
	g, err := glob.FromPath(cfgInput.Path)
	if err != nil {
		return nil, err
	}
	parsedGlobs = append(parsedGlobs, g)

	switch {
	case cfgInput.Type == "file":
		if cfgInput.PollInterval == 0 {
			tail, err = fswatcher.RunFileTailer(parsedGlobs, cfgInput.Readall, cfgInput.FailOnMissingLogfile, logger)
		} else {
			tail, err = fswatcher.RunPollingFileTailer(parsedGlobs, cfgInput.Readall, cfgInput.FailOnMissingLogfile, cfgInput.PollInterval, logger)
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
	p4p := newP4Prometheus(cfg, logger)

	debugInt := 0
	if debug {
		debugInt = 1
	}
	mcfg := &metrics.Config{
		Debug:                 debugInt,
		ServerID:              cfg.ServerID,
		SDPInstance:           cfg.SDPInstance,
		UpdateInterval:        cfg.UpdateInterval,
		OutputCmdsByUser:      cfg.OutputCmdsByUser,
		OutputCmdsByUserRegex: cfg.OutputCmdsByUserRegex,
		OutputCmdsByIP:        cfg.OutputCmdsByIP,
		CaseSensitiveServer:   cfg.CaseSensitiveServer,
	}
	logger.Infof("P4Prometheus config: %+v", mcfg)
	mp := metrics.NewP4DMetricsLogParser(mcfg, logger, false)

	linesChan := make(chan string, 10000)
	_, metricsChan := mp.ProcessEvents(ctx, linesChan, false)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Infof("Terminating - signal %v", sig)
		tailer.Close()
		cancel()
	}()

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
				linesChan <- line.Line
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
			"Set (for large servers) to not output cmds by user.",
		).Default("false").Bool()
		outputCmdsByUserRegex = kingpin.Flag(
			"output.cmds.by.user.regex",
			"Set to output cmds by user in detail for users matching this value as a regexp.",
		).String()
		noOutputCmdsByIP = kingpin.Flag(
			"no.output.cmds.by.ip",
			"Set (for large servers) to not output cmds by IP.",
		).Default("false").Bool()
		caseInsensitiveServer = kingpin.Flag(
			"case.insensitive.server",
			"Set if server is case insensitive.",
		).Default("false").Bool()
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
	if *outputCmdsByUserRegex != "" {
		cfg.OutputCmdsByUserRegex = *outputCmdsByUserRegex
	}
	if !*noOutputCmdsByIP {
		cfg.OutputCmdsByIP = !*noOutputCmdsByUser
	}
	if *caseInsensitiveServer {
		cfg.CaseSensitiveServer = !*caseInsensitiveServer
	}
	logger.Infof("%v", version.Print("p4prometheus"))
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

	logcfg = &logConfig{
		Type:                 "file",
		Path:                 cfg.LogPath,
		PollInterval:         time.Second * 1,
		Readall:              false,
		FailOnMissingLogfile: false,
	}
	runLogTailer(logger, logcfg, cfg, *debug)

}
