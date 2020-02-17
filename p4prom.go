package main

// This command line utility builds on top of the p4d log analyzer
// and outputs Prometheus metrics in a single file to be picked up by
// node_exporter's textfile.collector module.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

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

func newP4Prometheus(config *config.Config, logger *logrus.Logger) (p4p *P4Prometheus) {
	return &P4Prometheus{
		config:              config,
		logger:              logger,
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

func printMetricHeader(f io.Writer, name string, help string, metricType string) {
	fmt.Fprintf(f, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, metricType)
}

// Publish cumulative results - called on a ticker
func (p4p *P4Prometheus) getCumulativeMetrics() string {
	sdpInstanceLabel := ""
	serverIDLabel := fmt.Sprintf("serverid=\"%s\"", p4p.config.ServerID)
	if p4p.config.SDPInstance != "" {
		sdpInstanceLabel = fmt.Sprintf(",sdpinst=\"%s\"", p4p.config.SDPInstance)
	}
	metrics := new(bytes.Buffer)
	p4p.logger.Infof("Writing stats\n")
	p4p.lastOutputTime = time.Now()

	printMetricHeader(metrics, "p4_prom_log_lines_read", "A count of log lines read", "counter")
	buf := fmt.Sprintf("p4_prom_log_lines_read{%s%s} %d\n",
		serverIDLabel, sdpInstanceLabel, p4p.linesRead)
	p4p.logger.Debugf(buf)
	fmt.Fprint(metrics, buf)

	printMetricHeader(metrics, "p4_prom_cmds_processed", "A count of all cmds processed", "counter")
	buf = fmt.Sprintf("p4_prom_cmds_processed{%s%s} %d\n",
		serverIDLabel, sdpInstanceLabel, p4p.cmdsProcessed)
	p4p.logger.Debugf(buf)
	fmt.Fprint(metrics, buf)

	printMetricHeader(metrics, "p4_prom_cmds_pending", "A count of all current cmds (not completed)", "gauge")
	buf = fmt.Sprintf("p4_prom_cmds_pending{%s%s} %d\n",
		serverIDLabel, sdpInstanceLabel, p4p.fp.CmdsPendingCount())
	p4p.logger.Debugf(buf)
	fmt.Fprint(metrics, buf)

	printMetricHeader(metrics, "p4_cmd_counter", "A count of completed p4 cmds (by cmd)", "counter")
	for cmd, count := range p4p.cmdCounter {
		buf := fmt.Sprintf("p4_cmd_counter{cmd=\"%s\",%s%s} %d\n",
			cmd, serverIDLabel, sdpInstanceLabel, count)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	printMetricHeader(metrics, "p4_cmd_cumulative_seconds", "The total in seconds (by cmd)", "counter")
	for cmd, lapse := range p4p.cmdCumulative {
		buf := fmt.Sprintf("p4_cmd_cumulative_seconds{cmd=\"%s\",%s%s} %0.3f\n",
			cmd, serverIDLabel, sdpInstanceLabel, lapse)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	// For large sites this might not be sensible - so they can turn it off
	if p4p.config.OutputCmdsByUser {
		printMetricHeader(metrics, "p4_cmd_user_counter", "A count of completed p4 cmds (by user)", "counter")
		for cmd, count := range p4p.cmdByUserCounter {
			buf := fmt.Sprintf("p4_cmd_user_counter{user=\"%s\",%s%s} %d\n",
				cmd, serverIDLabel, sdpInstanceLabel, count)
			p4p.logger.Debugf(buf)
			fmt.Fprint(metrics, buf)
		}
		printMetricHeader(metrics, "p4_cmd_user_cumulative_seconds", "The total in seconds (by user)", "counter")
		for cmd, lapse := range p4p.cmdByUserCumulative {
			buf := fmt.Sprintf("p4_cmd_user_cumulative_seconds{user=\"%s\",%s%s} %0.3f\n",
				cmd, serverIDLabel, sdpInstanceLabel, lapse)
			p4p.logger.Debugf(buf)
			fmt.Fprint(metrics, buf)
		}
	}
	printMetricHeader(metrics, "p4_total_read_wait_seconds",
		"The total waiting for read locks in seconds (by table)", "counter")
	for table, total := range p4p.totalReadWait {
		buf := fmt.Sprintf("p4_total_read_wait_seconds{table=\"%s\",%s%s} %0.3f\n",
			table, serverIDLabel, sdpInstanceLabel, total)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	printMetricHeader(metrics, "p4_total_read_held_seconds",
		"The total read locks held in seconds (by table)", "counter")
	for table, total := range p4p.totalReadHeld {
		buf := fmt.Sprintf("p4_total_read_held_seconds{table=\"%s\",%s%s} %0.3f\n",
			table, serverIDLabel, sdpInstanceLabel, total)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	printMetricHeader(metrics, "p4_total_write_wait_seconds",
		"The total waiting for write locks in seconds (by table)", "counter")
	for table, total := range p4p.totalWriteWait {
		buf := fmt.Sprintf("p4_total_write_wait_seconds{table=\"%s\",%s%s} %0.3f\n",
			table, serverIDLabel, sdpInstanceLabel, total)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	printMetricHeader(metrics, "p4_total_write_held_seconds", "The total write locks held in seconds (by table)",
		"counter")
	for table, total := range p4p.totalWriteHeld {
		buf := fmt.Sprintf("p4_total_write_held_seconds{table=\"%s\",%s%s} %0.3f\n",
			table, serverIDLabel, sdpInstanceLabel, total)
		p4p.logger.Debugf(buf)
		fmt.Fprint(metrics, buf)
	}
	if len(p4p.totalTriggerLapse) > 0 {
		printMetricHeader(metrics, "p4_total_trigger_lapse_seconds",
			"The total lapse time for triggers in seconds (by trigger)", "counter")
		for table, total := range p4p.totalTriggerLapse {
			buf := fmt.Sprintf("p4_total_trigger_lapse_seconds{trigger=\"%s\",%s%s} %0.3f\n",
				table, serverIDLabel, sdpInstanceLabel, total)
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
	p4p.cmdByUserCounter[string(cmd.User)]++
	p4p.cmdByUserCumulative[string(cmd.User)] += float64(cmd.CompletedLapse)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	//---------------
	logcfg := &logConfig{
		Type:                 "file",
		Path:                 cfg.LogPath,
		PollInterval:         time.Second * 1,
		Readall:              false,
		FailOnMissingLogfile: false,
	}

	tailer, err := getTailer(logcfg, logger)
	if err != nil {
		logger.Errorf("error starting to tail log lines: %v", err)
		os.Exit(-2)
	}

	// Setup P4Prometheus object and a file parser
	p4p := newP4Prometheus(cfg, logger)
	fp := p4dlog.NewP4dFileParser(logger)
	p4p.fp = fp
	if *debug {
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
				// if p4p.lines != nil {
				// 	close(p4p.lines)
				// 	p4p.lines = nil
				// }
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
	os.Exit(0)
}
