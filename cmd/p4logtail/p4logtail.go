// This is part of p4prometheus
// It runs a tailer on p4d text log and feeds the lines to logparser, and
// then outputs JSON records.
// It should be run permanently as a systemd service on Linux
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
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

// BufferedFileWriter provides efficient buffered writing with periodic flushing
type BufferedFileWriter struct {
	filename      string
	flushInterval time.Duration
	file          *os.File
	writer        *bufio.Writer
	mutex         sync.RWMutex
	stopChan      chan struct{}
	stopped       bool
	wg            sync.WaitGroup
}

// NewBufferedFileWriter creates a new buffered file writer
func NewBufferedFileWriter(filename string, flushIntervalSeconds int) (*BufferedFileWriter, error) {
	bfw := &BufferedFileWriter{
		filename:      filename,
		flushInterval: time.Duration(flushIntervalSeconds) * time.Second,
		stopChan:      make(chan struct{}),
	}
	if err := bfw.openFile(); err != nil {
		return nil, err
	}

	// Start the periodic flush goroutine
	bfw.wg.Add(1)
	go bfw.periodicFlush()
	return bfw, nil
}

// openFile opens or creates the file and initializes the buffered writer
func (bfw *BufferedFileWriter) openFile() error {
	file, err := os.OpenFile(bfw.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", bfw.filename, err)
	}
	bfw.file = file
	bfw.writer = bufio.NewWriter(file)
	return nil
}

// WriteLine appends a line to the file with a newline character
func (bfw *BufferedFileWriter) WriteLine(line string) error {
	bfw.mutex.Lock()
	defer bfw.mutex.Unlock()
	if bfw.stopped {
		return fmt.Errorf("writer is closed")
	}
	_, err := bfw.writer.WriteString(line + "\n")
	return err
}

// Flush manually flushes the buffer and syncs to disk
func (bfw *BufferedFileWriter) Flush() error {
	bfw.mutex.Lock()
	defer bfw.mutex.Unlock()

	if bfw.stopped {
		return fmt.Errorf("writer is closed")
	}
	if err := bfw.writer.Flush(); err != nil {
		return err
	}
	return bfw.file.Sync()
}

// flushAndClose flushes the buffer, closes the file, and reopens it
func (bfw *BufferedFileWriter) flushAndClose() error {
	if bfw.stopped {
		return fmt.Errorf("writer is closed")
	}

	// Flush and close current file
	if err := bfw.writer.Flush(); err != nil {
		return err
	}
	if err := bfw.file.Sync(); err != nil {
		return err
	}
	if err := bfw.file.Close(); err != nil {
		return err
	}

	// Reopen the file (which may now be a new file if logrotate renamed the old one)
	return bfw.openFile()
}

// periodicFlush runs in a goroutine and flushes/closes/reopens the file periodically
func (bfw *BufferedFileWriter) periodicFlush() {
	defer bfw.wg.Done()

	ticker := time.NewTicker(bfw.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bfw.mutex.Lock()
			if !bfw.stopped {
				if err := bfw.flushAndClose(); err != nil {
					// In a production system, you might want to log this error
					fmt.Printf("Error during periodic flush: %v\n", err)
				}
			}
			bfw.mutex.Unlock()

		case <-bfw.stopChan:
			return
		}
	}
}

// Close stops the periodic flushing and closes the file
func (bfw *BufferedFileWriter) Close() error {
	bfw.mutex.Lock()
	defer bfw.mutex.Unlock()

	if bfw.stopped {
		return nil
	}
	bfw.stopped = true
	close(bfw.stopChan)

	// Wait for the periodic flush goroutine to stop
	bfw.wg.Wait()

	// Final flush and close
	if err := bfw.writer.Flush(); err != nil {
		return err
	}
	if err := bfw.file.Sync(); err != nil {
		return err
	}
	return bfw.file.Close()
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
	writer, err := NewBufferedFileWriter(p4l.config.JSONLog, 1)
	if err != nil {
		logger.Fatal(err)
	}
	defer writer.Close()

	for cmd := range cmdChan {
		switch cmd := cmd.(type) {
		case p4dlog.Command:
			err := writer.WriteLine(cmd.String())
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
