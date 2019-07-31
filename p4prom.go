package main

// This command line utility builds on top of the p4d log analyzer
// and outputs Prometheus metrics in a single file to be picked up by
// node_exporter's textfile.collector module.

import (
	"bytes"
	"fmt"
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
	config            *config.Config
	logger            *logrus.Logger
	fp                *p4dlog.P4dFileParser
	cmdCounter        map[string]int32
	cmdCumulative     map[string]float64
	totalReadWait     map[string]float64
	totalReadHeld     map[string]float64
	totalWriteWait    map[string]float64
	totalWriteHeld    map[string]float64
	totalTriggerLapse map[string]float64
	cmdsProcessed     int64
	linesRead         int64
	lines             chan []byte
	events            chan p4dlog.Command
	lastOutputTime    time.Time
}

func newP4Prometheus(config *config.Config, logger *logrus.Logger) (p4p *P4Prometheus) {
	return &P4Prometheus{
		config:            config,
		logger:            logger,
		lines:             make(chan []byte, 10000),
		events:            make(chan p4dlog.Command, 10000),
		cmdCounter:        make(map[string]int32),
		cmdCumulative:     make(map[string]float64),
		totalReadWait:     make(map[string]float64),
		totalReadHeld:     make(map[string]float64),
		totalWriteWait:    make(map[string]float64),
		totalWriteHeld:    make(map[string]float64),
		totalTriggerLapse: make(map[string]float64),
	}
}

func (p4p *P4Prometheus) publishCumulative() {
	if p4p.lastOutputTime == blankTime || time.Now().Sub(p4p.lastOutputTime) >= time.Second*10 {
		f, err := os.Create(p4p.config.MetricsOutput)
		if err != nil {
			p4p.logger.Errorf("Error opening %s: %v", p4p.config.MetricsOutput, err)
			return
		}
		p4p.logger.Infof("Writing stats\n")
		p4p.lastOutputTime = time.Now()

		fmt.Fprintf(f, "# HELP p4_prom_log_lines_read A count of log lines read\n"+
			"# TYPE p4_prom_log_lines_read counter\n")
		buf := fmt.Sprintf("p4_prom_log_lines_read{serverid=\"%s\",sdpinst=\"%s\"} %d\n",
			p4p.config.ServerID, p4p.config.SDPInstance, p4p.linesRead)
		p4p.logger.Debugf(buf)
		fmt.Fprint(f, buf)

		fmt.Fprintf(f, "# HELP p4_prom_cmds_processed A count of all cmds processed\n"+
			"# TYPE p4_prom_cmds_processed counter\n")
		buf = fmt.Sprintf("p4_prom_cmds_processed{serverid=\"%s\",sdpinst=\"%s\"} %d\n",
			p4p.config.ServerID, p4p.config.SDPInstance, p4p.cmdsProcessed)
		p4p.logger.Debugf(buf)
		fmt.Fprint(f, buf)

		fmt.Fprintf(f, "# HELP p4_prom_cmds_pending A count of all current cmds (not completed)\n"+
			"# TYPE p4_prom_cmds_pending gauge\n")
		buf = fmt.Sprintf("p4_prom_cmds_pending{serverid=\"%s\",sdpinst=\"%s\"} %d\n",
			p4p.config.ServerID, p4p.config.SDPInstance, p4p.fp.CmdsPendingCount())
		p4p.logger.Debugf(buf)
		fmt.Fprint(f, buf)

		fmt.Fprintf(f, "# HELP p4_cmd_counter A count of completed p4 cmds (by cmd)\n"+
			"# TYPE p4_cmd_counter counter\n")
		for cmd, count := range p4p.cmdCounter {
			buf := fmt.Sprintf("p4_cmd_counter{cmd=\"%s\",serverid=\"%s\",sdpinst=\"%s\"} %d\n",
				cmd, p4p.config.ServerID, p4p.config.SDPInstance, count)
			p4p.logger.Debugf(buf)
			fmt.Fprint(f, buf)
		}
		fmt.Fprintf(f, "# HELP p4_cmd_cumulative_seconds The total in seconds (by cmd)\n"+
			"# TYPE p4_cmd_cumulative_seconds counter\n")
		for cmd, lapse := range p4p.cmdCumulative {
			buf := fmt.Sprintf("p4_cmd_cumulative_seconds{cmd=\"%s\",serverid=\"%s\",sdpinst=\"%s\"} %0.3f\n",
				cmd, p4p.config.ServerID, p4p.config.SDPInstance, lapse)
			p4p.logger.Debugf(buf)
			fmt.Fprint(f, buf)
		}
		fmt.Fprintf(f, "# HELP p4_total_read_wait_seconds The total waiting for read locks in seconds (by table)\n"+
			"# TYPE p4_total_read_wait_seconds counter\n")
		for table, total := range p4p.totalReadWait {
			buf := fmt.Sprintf("p4_total_read_wait_seconds{table=\"%s\",serverid=\"%s\",sdpinst=\"%s\"} %0.3f\n",
				table, p4p.config.ServerID, p4p.config.SDPInstance, total)
			p4p.logger.Debugf(buf)
			fmt.Fprint(f, buf)
		}
		fmt.Fprintf(f, "# HELP p4_total_read_held_seconds The total read locks held in seconds (by table)\n"+
			"# TYPE p4_total_read_held_seconds counter\n")
		for table, total := range p4p.totalReadHeld {
			buf := fmt.Sprintf("p4_total_read_held_seconds{table=\"%s\",serverid=\"%s\",sdpinst=\"%s\"} %0.3f\n",
				table, p4p.config.ServerID, p4p.config.SDPInstance, total)
			p4p.logger.Debugf(buf)
			fmt.Fprint(f, buf)
		}
		fmt.Fprintf(f, "# HELP p4_total_write_wait_seconds The total waiting for write locks in seconds (by table)\n"+
			"# TYPE p4_total_write_wait_seconds counter\n")
		for table, total := range p4p.totalWriteWait {
			buf := fmt.Sprintf("p4_total_write_wait_seconds{table=\"%s\",serverid=\"%s\",sdpinst=\"%s\"} %0.3f\n",
				table, p4p.config.ServerID, p4p.config.SDPInstance, total)
			p4p.logger.Debugf(buf)
			fmt.Fprint(f, buf)
		}
		fmt.Fprintf(f, "# HELP p4_total_write_held_seconds The total write locks held in seconds (by table)\n"+
			"# TYPE p4_total_write_held_seconds counter\n")
		for table, total := range p4p.totalWriteHeld {
			buf := fmt.Sprintf("p4_total_write_held_seconds{table=\"%s\",serverid=\"%s\",sdpinst=\"%s\"} %0.3f\n",
				table, p4p.config.ServerID, p4p.config.SDPInstance, total)
			p4p.logger.Debugf(buf)
			fmt.Fprint(f, buf)
		}
		if len(p4p.totalTriggerLapse) > 0 {
			fmt.Fprintf(f, "# HELP p4_total_trigger_lapse_seconds The total lapse time for triggers in seconds (by trigger)\n"+
				"# TYPE p4_total_trigger_lapse_seconds counter\n")
			for table, total := range p4p.totalTriggerLapse {
				buf := fmt.Sprintf("p4_total_trigger_lapse_seconds{trigger=\"%s\",serverid=\"%s\",sdpinst=\"%s\"} %0.3f\n",
					table, p4p.config.ServerID, p4p.config.SDPInstance, total)
				p4p.logger.Debugf(buf)
				fmt.Fprint(f, buf)
			}
		}
		err = f.Close()
		if err != nil {
			p4p.logger.Errorf("Error closing file: %v", err)
		}
		err = os.Chmod(p4p.config.MetricsOutput, 0644)
		if err != nil {
			p4p.logger.Errorf("Error chmod-ing file: %v", err)
		}
	}
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
	p4p.logger.Debugf("publish cmd: %v\n", cmd)

	p4p.cmdCounter[string(cmd.Cmd)]++
	p4p.cmdCumulative[string(cmd.Cmd)] += float64(cmd.CompletedLapse)
	const triggerPrefix = "trigger_"

	for _, t := range cmd.Tables {
		if len(t.TableName) > len(triggerPrefix) && t.TableName[:len(triggerPrefix)] == triggerPrefix {
			triggerName := t.TableName[len(triggerPrefix):]
			p4p.totalTriggerLapse[triggerName] += float64(t.TriggerLapse)
		} else {
			p4p.totalReadHeld[t.TableName] += float64(t.TotalReadHeld)
			p4p.totalReadWait[t.TableName] += float64(t.TotalReadWait)
			p4p.totalWriteHeld[t.TableName] += float64(t.TotalWriteHeld)
			p4p.totalWriteWait[t.TableName] += float64(t.TotalWriteWait)
		}
	}
	p4p.publishCumulative()
}

func (p4p *P4Prometheus) processEvents() {
	for {
		select {
		case cmd := <-p4p.events:
			p4p.publishEvent(cmd)
		default:
			return
		}
	}
}

func startTailer(cfgInput *logConfig, logger *logrus.Logger) (fswatcher.FileTailer, error) {

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
	if len(cfg.ServerID) == 0 {
		cfg.ServerID = readServerID(logger, cfg.SDPInstance)
	}
	logger.Infof("Server id: '%s'\n", cfg.ServerID)
	p4p := newP4Prometheus(cfg, logger)

	fp := p4dlog.NewP4dFileParser()
	p4p.fp = fp
	go fp.LogParser(p4p.lines, p4p.events, nil)

	//---------------

	logcfg := &logConfig{
		Type:                 "file",
		Path:                 cfg.LogPath,
		PollInterval:         time.Second * 1,
		Readall:              true,
		FailOnMissingLogfile: true,
	}

	tail, err := startTailer(logcfg, logger)
	if err != nil {
		logger.Errorf("error starting to tail log lines: %v", err)
		os.Exit(-2)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	p4p.linesRead = 0
	for {
		select {
		case <-time.After(time.Second * 1):
			p4p.processEvents()
			p4p.publishCumulative()
		case err := <-tail.Errors():
			if os.IsNotExist(err.Cause()) {
				logger.Errorf("error reading log lines: %v: use 'fail_on_missing_logfile: false' in the input configuration if you want p4prometheus to start even though the logfile is missing", err)
				os.Exit(-3)
			} else {
				logger.Errorf("error reading log lines: %v", err)
				os.Exit(-4)
			}
		case line := <-tail.Lines():
			p4p.linesRead++
			p4p.lines <- []byte(line.Line)
		case json := <-p4p.events:
			p4p.cmdsProcessed++
			p4p.publishEvent(json)
		case sig := <-sigs:
			logger.Infof("Terminating normally - signal %v", sig)
			os.Exit(0)
		}
	}

}
