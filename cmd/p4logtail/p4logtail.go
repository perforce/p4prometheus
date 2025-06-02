// This is part of p4prometheus
// It runs a tailer on p4d text log and feeds the lines to logparser, and
// then outputs JSON records.
// It should be run permanently as a systemd service on Linux
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/perforce/p4prometheus/cmd/p4logtail/config"
	"github.com/perforce/p4prometheus/version"
	p4dlog "github.com/rcowham/go-libp4dlog"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/go-libtail/tailer/glob"
	"github.com/rcowham/kingpin"

	"github.com/sirupsen/logrus"
)

// Structure for use with libtail
type logConfig struct {
	Type                 string
	Path                 string
	PollInterval         time.Duration
	Readall              bool
	FailOnMissingLogfile bool
}

// P4LogTail structure
type P4LogTail struct {
	config *config.Config
	logger *logrus.Logger
}

func newP4LogTail(config *config.Config, logger *logrus.Logger) (p4l *P4LogTail) {
	return &P4LogTail{
		config: config,
		logger: logger,
	}
}

func appendFile(outputName string, line string) error {
	var fd *os.File
	var err error
	if outputName == "-" {
		fd = os.Stdout
	} else {
		fd, err = os.OpenFile(outputName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer fd.Close()
	}
	if _, err := fmt.Fprintln(fd, line); err != nil {
		return err
	}
	return nil
}

// Returns a tailer object for specified file
func (p4l *P4LogTail) getTailer(cfgInput *logConfig) (fswatcher.FileTailer, error) {

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
			tail, err = fswatcher.RunFileTailer(parsedGlobs, cfgInput.Readall, cfgInput.FailOnMissingLogfile, p4l.logger)
		} else {
			tail, err = fswatcher.RunPollingFileTailer(parsedGlobs, cfgInput.Readall, cfgInput.FailOnMissingLogfile, cfgInput.PollInterval, p4l.logger)
		}
		return tail, err
	default:
		return nil, fmt.Errorf("config error: Input type '%v' unknown", cfgInput.Type)
	}
}

// Loop tailing p4d log and writing JSON output when appropriate
func (p4l *P4LogTail) runLogTailer(logger *logrus.Logger) {

	logcfg := &logConfig{
		Type:                 "file",
		Path:                 p4l.config.P4Log,
		PollInterval:         time.Second * 1,
		Readall:              false,
		FailOnMissingLogfile: false,
	}
	tailer, err := p4l.getTailer(logcfg)
	if err != nil {
		logger.Errorf("error starting to tail log lines: %v", err)
		os.Exit(-2)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		logger.Infof("Terminating - signal %v", sig)
		tailer.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// NewP4dFileParser - create and initialise properly
	fp := p4dlog.NewP4dFileParser(logger)
	linesChan := make(chan string, 10000)
	cmdChan := fp.LogParser(ctx, linesChan)

	go func() {
		for {
			select {
			case line, ok := <-tailer.Lines():
				if ok {
					linesChan <- line.Line
				} else {
					p4l.logger.Error("Tail error")
					os.Exit(-1)
				}
			case err := <-tailer.Errors():
				if err != nil {
					if os.IsNotExist(err.Cause()) {
						p4l.logger.Errorf("error reading errors.csv lines: %v: use 'fail_on_missing_logfile: false' in the input configuration if you want p4logtail to start even though the logfile is missing", err)
						os.Exit(-3)
					}
					p4l.logger.Errorf("error reading errors.csv lines: %v", err)
					return
				}
				p4l.logger.Info("Finishing logTailer")
				os.Exit(-2)
			}
		}
	}()

	logger.Infof("Creating JSON output: %s", p4l.config.JSONLog)
	for cmd := range cmdChan {
		switch cmd := cmd.(type) {
		case p4dlog.Command:
			err := appendFile(p4l.config.JSONLog, cmd.String())
			if err != nil {
				logger.Errorf("error writing to JSON output file %q: %v", p4l.config.JSONLog, err)
				os.Exit(-4)
			}
		}
	}
}

func main() {
	var (
		configfile = kingpin.Flag(
			"config",
			"Config file for p4logtail.",
		).Short('c').Default("p4logtail.yaml").String()
		p4log = kingpin.Flag(
			"p4log",
			"P4LOG file to process (overrides value in config file if specified)",
		).Default("").String()
		jsonlog = kingpin.Flag(
			"jsonlog",
			"Name of ouput file in JSON format (overrides value in config file if specified)",
		).Default("").String()
		debug = kingpin.Flag(
			"debug",
			"Enable debugging.",
		).Bool()
	)

	kingpin.Version(version.Print("p4logtail"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := logrus.New()
	logger.Level = logrus.InfoLevel
	if *debug {
		logger.Level = logrus.DebugLevel
	}

	logger.Debugf("Loading config file: %q", *configfile)
	cfg, err := config.LoadConfigFile(*configfile)
	if err != nil {
		logger.Errorf("error loading config file: %v", err)
		os.Exit(-1)
	}

	logger.Infof("%v", version.Print("p4logtail"))
	logger.Infof("Config: %+v", *cfg)
	logger.Infof("Processing:  '%s', output to '%s'", cfg.P4Log, cfg.JSONLog)

	if *p4log != "" {
		if cfg.P4Log != "" {
			logger.Warnf("--p4log %q overwriting config value %q", *p4log, cfg.P4Log)
		}
		cfg.P4Log = *p4log
	}
	if *jsonlog != "" {
		if cfg.JSONLog != "" {
			logger.Warnf("--jsonlog %q overwriting config value %q", *jsonlog, cfg.JSONLog)
		}
		cfg.JSONLog = *jsonlog
	}

	p4l := newP4LogTail(cfg, logger)
	p4l.runLogTailer(logger)
}
