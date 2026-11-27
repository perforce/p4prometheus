// p4proxyprometheus tails a p4p log and writes Prometheus textfile metrics.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/perforce/p4prometheus/cmd/p4proxyprometheus/config"
	"github.com/perforce/p4prometheus/version"
	"github.com/rcowham/go-libp4dlog/p4plog"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/go-libtail/tailer/glob"
	"github.com/rcowham/kingpin"
	"github.com/sirupsen/logrus"
)

type logConfig struct {
	Path                 string
	PollInterval         time.Duration
	ReadAll              bool
	FailOnMissingLogfile bool
}

type metricState struct {
	commandsCompleted int64
	durationSeconds   float64
	serverFiles       int64
	cacheFiles        int64
	serverBytes       int64
	cacheBytes        int64
	lastCompletion    time.Time
}

func (m *metricState) observe(command p4plog.ProxyCommand) {
	m.commandsCompleted++
	m.durationSeconds += float64(command.CompletedLapse)
	m.serverFiles += command.ProxyTotalsSvr
	m.cacheFiles += command.ProxyTotalsCache
	m.serverBytes += command.ProxyTotalsSvrBytes
	m.cacheBytes += command.ProxyTotalsCacheBytes
	if command.EndTime.After(m.lastCompletion) {
		m.lastCompletion = command.EndTime
	}
}

func (m metricState) render() []byte {
	lastCompletion := float64(0)
	if !m.lastCompletion.IsZero() {
		lastCompletion = float64(m.lastCompletion.Unix())
	}

	var output bytes.Buffer
	fmt.Fprintln(&output, "# HELP p4_proxy_commands_completed_total Number of completed commands observed in the p4p log.")
	fmt.Fprintln(&output, "# TYPE p4_proxy_commands_completed_total counter")
	fmt.Fprintf(&output, "p4_proxy_commands_completed_total %d\n", m.commandsCompleted)
	fmt.Fprintln(&output, "# HELP p4_proxy_command_duration_seconds_total Sum of completed command lapse times in seconds.")
	fmt.Fprintln(&output, "# TYPE p4_proxy_command_duration_seconds_total counter")
	fmt.Fprintf(&output, "p4_proxy_command_duration_seconds_total %.6f\n", m.durationSeconds)
	fmt.Fprintln(&output, "# HELP p4_proxy_files_total Files transferred by source.")
	fmt.Fprintln(&output, "# TYPE p4_proxy_files_total counter")
	fmt.Fprintf(&output, "p4_proxy_files_total{source=\"server\"} %d\n", m.serverFiles)
	fmt.Fprintf(&output, "p4_proxy_files_total{source=\"cache\"} %d\n", m.cacheFiles)
	fmt.Fprintln(&output, "# HELP p4_proxy_bytes_total Bytes transferred by source.")
	fmt.Fprintln(&output, "# TYPE p4_proxy_bytes_total counter")
	fmt.Fprintf(&output, "p4_proxy_bytes_total{source=\"server\"} %d\n", m.serverBytes)
	fmt.Fprintf(&output, "p4_proxy_bytes_total{source=\"cache\"} %d\n", m.cacheBytes)
	fmt.Fprintln(&output, "# HELP p4_proxy_last_command_completion_timestamp_seconds Unix timestamp of the most recently completed command.")
	fmt.Fprintln(&output, "# TYPE p4_proxy_last_command_completion_timestamp_seconds gauge")
	fmt.Fprintf(&output, "p4_proxy_last_command_completion_timestamp_seconds %.0f\n", lastCompletion)
	return output.Bytes()
}

func writeMetricsFile(filename string, metrics []byte) (err error) {
	directory := filepath.Dir(filename)
	file, err := os.CreateTemp(directory, "."+filepath.Base(filename)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary metrics file in %q: %w", directory, err)
	}
	tempName := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()

	if _, err := file.Write(metrics); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary metrics file %q: %w", tempName, err)
	}
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		return fmt.Errorf("set permissions on temporary metrics file %q: %w", tempName, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary metrics file %q: %w", tempName, err)
	}
	if err := os.Rename(tempName, filename); err != nil {
		return fmt.Errorf("replace metrics file %q: %w", filename, err)
	}
	return nil
}

func metricsOutputFilename(metricsOutput, sdpInstance string) string {
	return fmt.Sprintf(
		"%s-%s.prom",
		strings.TrimSuffix(metricsOutput, ".prom"),
		sdpInstance,
	)
}

func getTailer(cfg logConfig, logger *logrus.Logger) (fswatcher.FileTailer, error) {
	parsedGlob, err := glob.FromPath(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("parse p4p log path %q: %w", cfg.Path, err)
	}
	if cfg.PollInterval == 0 {
		return fswatcher.RunFileTailer([]glob.Glob{parsedGlob}, cfg.ReadAll, cfg.FailOnMissingLogfile, logger)
	}
	return fswatcher.RunPollingFileTailer(
		[]glob.Glob{parsedGlob},
		cfg.ReadAll,
		cfg.FailOnMissingLogfile,
		cfg.PollInterval,
		logger,
	)
}

func run(cfg *config.Config, logger *logrus.Logger) error {
	metricsOutput := metricsOutputFilename(cfg.MetricsOutput, cfg.SDPInstance)
	tailer, err := getTailer(logConfig{
		Path:                 cfg.P4PLog,
		PollInterval:         time.Duration(cfg.PollIntervalSeconds) * time.Second,
		ReadAll:              cfg.ReadAll,
		FailOnMissingLogfile: cfg.FailOnMissingLogfile,
	}, logger)
	if err != nil {
		return err
	}
	defer tailer.Close()

	state := metricState{}
	if err := writeMetricsFile(metricsOutput, state.render()); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parser := p4plog.NewP4pFileParser(logger)
	lines := make(chan string, 10000)
	commands := parser.LogParser(ctx, lines)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	for {
		select {
		case signal := <-signals:
			logger.Infof("terminating on signal %v", signal)
			return nil
		case line, ok := <-tailer.Lines():
			if !ok {
				return nil
			}
			lines <- line.Line
		case tailError, ok := <-tailer.Errors():
			if !ok {
				return nil
			}
			if tailError == nil {
				continue
			}
			if os.IsNotExist(tailError.Cause()) {
				return fmt.Errorf(
					"read p4p log: %w; set fail_on_missing_logfile: false to wait for it",
					tailError,
				)
			}
			return fmt.Errorf("read p4p log: %w", tailError)
		case command, ok := <-commands:
			if !ok {
				return nil
			}
			state.observe(command)
			if err := writeMetricsFile(metricsOutput, state.render()); err != nil {
				return err
			}
		}
	}
}

func main() {
	var (
		configFile = kingpin.Flag("config", "Config file for p4proxyprometheus.").Short('c').Default("p4proxyprometheus.yaml").String()
		p4pLog     = kingpin.Flag("p4plog", "P4P log file to process (overrides p4p_log).").String()
		metrics    = kingpin.Flag("metrics.output", "Prometheus metrics file (overrides metrics_output).").String()
		debug      = kingpin.Flag("debug", "Enable debug logging.").Bool()
		sample     = kingpin.Flag(
			"sample.config",
			"Output a sample config file and exit. Example: p4proxyprometheus --sample.config > p4proxyprometheus.yaml",
		).Bool()
	)
	kingpin.Version(version.Print("p4proxyprometheus"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	if *sample {
		fmt.Print(config.SampleConfig)
		return
	}

	logger := logrus.New()
	if *debug {
		logger.SetLevel(logrus.DebugLevel)
	}

	cfg, err := config.LoadConfigFile(*configFile)
	if err != nil {
		logger.Error(err)
		os.Exit(1)
	}
	if *p4pLog != "" {
		cfg.P4PLog = *p4pLog
	}
	if *metrics != "" {
		cfg.MetricsOutput = *metrics
	}
	if err := cfg.Validate(); err != nil {
		logger.Error(err)
		os.Exit(1)
	}

	logger.Infof("%s", version.Print("p4proxyprometheus"))
	logger.Infof(
		"Tailing p4p log %q and writing metrics to %q",
		cfg.P4PLog,
		metricsOutputFilename(cfg.MetricsOutput, cfg.SDPInstance),
	)
	if err := run(cfg, logger); err != nil {
		logger.Error(err)
		os.Exit(1)
	}
}
