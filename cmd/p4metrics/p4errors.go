// This is created from p4d error defintions and is used to parse structured log errors.csv
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rcowham/go-libtail/tailer"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/go-libtail/tailer/glob"
	"github.com/sirupsen/logrus"
)

type ErrorSeverity int

const (
	E_EMPTY  ErrorSeverity = 0 // nothing yet
	E_INFO                 = 1 // something good happened
	E_WARN                 = 2 // something not good happened
	E_FAILED               = 3 // user did somthing wrong
	E_FATAL                = 4 // system broken -- nothing can continue
)

type ErrorGeneric int

const (
	EV_NONE ErrorGeneric = 0 // misc
	// The fault of the user
	EV_USAGE   = 0x01 // request not consistent with dox
	EV_UNKNOWN = 0x02 // using unknown entity
	EV_CONTEXT = 0x03 // using entity in wrong context
	EV_ILLEGAL = 0x04 // trying to do something you can't
	EV_NOTYET  = 0x05 // something must be corrected first
	EV_PROTECT = 0x06 // protections prevented operation
	// No fault at all
	EV_EMPTY = 0x11 // action returned empty results
	// not the fault of the user
	EV_FAULT   = 0x21 // inexplicable program fault
	EV_CLIENT  = 0x22 // client side program errors
	EV_ADMIN   = 0x23 // server administrative action required
	EV_CONFIG  = 0x24 // client configuration inadequate
	EV_UPGRADE = 0x25 // client or server too old to interact
	EV_COMM    = 0x26 // communications error
	EV_TOOBIG  = 0x27 // not ever Perforce can handle this much
)

type ErrorSubsystem int

const (
	ES_OS       ErrorSubsystem = 0  // OS error
	ES_SUPP                    = 1  // Misc support
	ES_LBR                     = 2  // librarian
	ES_RPC                     = 3  // messaging
	ES_DB                      = 4  // database
	ES_DBSUPP                  = 5  // database support
	ES_DM                      = 6  // data manager
	ES_SERVER                  = 7  // top level of server
	ES_CLIENT                  = 8  // top level of client
	ES_INFO                    = 9  // pseudo subsystem for information messages
	ES_HELP                    = 10 // pseudo subsystem for help messages
	ES_SPEC                    = 11 // pseudo subsystem for spec/comment messages
	ES_FTPD                    = 12 // P4FTP server
	ES_BROKER                  = 13 // Perforce Broker
	ES_P4QT                    = 14 // P4V and other Qt based clients
	ES_X3SERVER                = 15 // P4X3 server
	ES_GRAPH                   = 16 // graph depot messages
	ES_SCRIPT                  = 17 // scripting
	ES_SERVER2                 = 18 // server overflow
	ES_DM2                     = 19 // dm overflow
	ES_CONFIG                  = 20 // help for configurables
)

var subsystems map[ErrorSubsystem]string = map[ErrorSubsystem]string{
	ES_OS:       "OS",
	ES_SUPP:     "SUPP",
	ES_LBR:      "LBR",
	ES_RPC:      "RPC",
	ES_DB:       "DB",
	ES_DBSUPP:   "DBSUPP",
	ES_DM:       "DM",
	ES_SERVER:   "SERVER",
	ES_CLIENT:   "CLIENT",
	ES_INFO:     "INFO",
	ES_HELP:     "HELP",
	ES_SPEC:     "SPEC",
	ES_FTPD:     "FTPD",
	ES_BROKER:   "BROKER",
	ES_P4QT:     "P4QT",
	ES_X3SERVER: "X3SERVER",
	ES_GRAPH:    "GRAPH",
	ES_SCRIPT:   "SCRIPT",
	ES_SERVER2:  "SERVER2",
	ES_DM2:      "DM2",
	ES_CONFIG:   "CONFIG",
}

// Structure for use with libtail
type logConfig struct {
	Type                 string
	Path                 string
	PollInterval         time.Duration
	Readall              bool
	FailOnMissingLogfile bool
}

// LogSchema represents a single parsed record
type LogSchema struct {
	RecordType    string
	RecordVersion string
	RecordName    string
	FieldIndex    int
	Name          string
}

// The output of p4 logschema -a
// ... f_recordType 4
// ... f_recordVersion 58
// ... f_recordName Error
// ... f_field 0
// ... f_name f_eventtype

// ParseLogSchema parses the input text and returns an array of Records
func ParseLogSchema(lines []string) []LogSchema {
	var records []LogSchema
	var currentRecord *LogSchema

	for _, line := range lines {
		line := strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "... ")
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]

		// Start a new record when we see f_recordType
		if key == "f_recordType" {
			// Save the previous record if it exists
			if currentRecord != nil {
				records = append(records, *currentRecord)
			}
			currentRecord = &LogSchema{}
		}
		if currentRecord != nil {
			switch key {
			case "f_recordType":
				currentRecord.RecordType = value
			case "f_recordVersion":
				currentRecord.RecordVersion = value
			case "f_recordName":
				currentRecord.RecordName = value
			case "f_field":
				if val, err := strconv.Atoi(value); err == nil {
					currentRecord.FieldIndex = val
				}
			case "f_name":
				currentRecord.Name = value
			}
		}
	}
	// Don't forget to add the last record
	if currentRecord != nil {
		records = append(records, *currentRecord)
	}
	return records
}

// Processes the output of p4 logschema -a
func (p4m *P4MonitorMetrics) setupErrorParsing(schemaLines []string) {
	schema := ParseLogSchema(schemaLines)
	p4m.logger.Debugf("logschema count: %d", len(schema))
	for _, s := range schema {
		if s.RecordName == "Error" {
			switch s.Name {
			case "f_severity":
				p4m.indErrSeverity = s.FieldIndex
			case "f_subsys":
				p4m.indErrSubsys = s.FieldIndex
			}
		}
	}
	p4m.logger.Debugf("logschema error indexes: %d %d", p4m.indErrSeverity, p4m.indErrSubsys)
}

// Returns a tailer object for specified file
func (p4m *P4MonitorMetrics) getTailer(cfgInput *logConfig) (fswatcher.FileTailer, error) {

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
			tail, err = fswatcher.RunFileTailer(parsedGlobs, cfgInput.Readall, cfgInput.FailOnMissingLogfile, p4m.logger)
		} else {
			tail, err = fswatcher.RunPollingFileTailer(parsedGlobs, cfgInput.Readall, cfgInput.FailOnMissingLogfile, cfgInput.PollInterval, p4m.logger)
		}
		return tail, err
	case cfgInput.Type == "stdin":
		tail = tailer.RunStdinTailer()
	default:
		return nil, fmt.Errorf("config error: Input type '%v' unknown", cfgInput.Type)
	}
	return tail, nil
}

func (p4m *P4MonitorMetrics) parseErrorLine(line string) {
	fields := strings.Split(line, ",")
	if len(fields) < p4m.indErrSeverity || len(fields) < p4m.indErrSubsys {
		p4m.logger.Debugf("Failed to parse %q", line)
		return
	}
	var m ErrorMetric
	m.Severity = fields[p4m.indErrSeverity]
	if subsys, err := strconv.Atoi(fields[p4m.indErrSubsys]); err == nil {
		if subsys < len(subsystems) {
			m.Subsystem = subsystems[ErrorSubsystem(subsys)]
		} else {
			m.Subsystem = "unknown"
		}
	}
	p4m.logger.Debugf("Incrementing error count %v", m)
	p4m.errorMetrics[m] += 1
	p4m.logger.Debugf("All error count %v", p4m.errorMetrics)
}

func (p4m *P4MonitorMetrics) writeErrorMetrics() {
	p4m.startMonitor("monitorErrors", "p4_errors")
	for m, count := range p4m.errorMetrics {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_error_count",
				help:  "P4D error count by subsystem and level",
				mtype: "counter",
				value: fmt.Sprintf("%d", count),
				labels: []labelStruct{{name: "subsys", value: m.Subsystem},
					{name: "severity", value: m.Severity},
				}})
	}
	p4m.writeMetricsFile()
}

// Loop reading tailing error log and writing metrics when appropriate
func (p4m *P4MonitorMetrics) runLogTailer(logger *logrus.Logger, logcfg *logConfig) {

	tailer, err := p4m.getTailer(logcfg)
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

	ticker := time.NewTicker(p4m.config.UpdateInterval)

	for {
		select {
		case <-ticker.C:
			p4m.logger.Debug("Writing error metrics")
			p4m.writeErrorMetrics()
		case line, ok := <-tailer.Lines():
			if ok {
				p4m.logger.Debugf("Error line %q", line.Line)
				p4m.parseErrorLine(line.Line)
			} else {
				p4m.logger.Debug("Tail error")
				return
			}
		case err := <-tailer.Errors():
			if err != nil {
				if os.IsNotExist(err.Cause()) {
					p4m.logger.Errorf("error reading errors.csv lines: %v: use 'fail_on_missing_logfile: false' in the input configuration if you want p4metrics to start even though the logfile is missing", err)
					os.Exit(-3)
				}
				p4m.logger.Errorf("error reading errors.csv lines: %v", err)
				return
			}
			p4m.logger.Debug("Finishing logTailer")
			return
		}
	}
}

func (p4m *P4MonitorMetrics) monitorErrors() {
	// Parse the errors.csv file
	p4m.startMonitor("monitorErrors", "p4_errors")
	p4cmd, errbuf, p := p4m.newP4CmdPipe("logschema -a")
	schema, err := p.Exec(p4cmd).Slice()
	if err != nil {
		p4m.logger.Errorf("Error running %s: %v, err:%q", p4cmd, err, errbuf.String())
		return
	}
	if len(schema) == 0 {
		p4m.logger.Debug("No logschema found!")
		return
	}
	p4m.setupErrorParsing(schema)
	logcfg := &logConfig{
		Type:                 "file",
		Path:                 "/p4/1/logs/errors.csv",
		PollInterval:         time.Second * 30,
		Readall:              false,
		FailOnMissingLogfile: false,
	}
	p4m.runLogTailer(p4m.logger, logcfg)
}

// monitor_errors () {
//     # Metric for error counts - but only if structured error log exists
//     fname="$metrics_root/p4_errors${sdpinst_suffix}-${SERVER_ID}.prom"
//     tmpfname="$fname.$$"

//     rm -f "$tmpfname"
//     echo "#HELP p4_error_count Server errors by id" >> "$tmpfname"
//     echo "#TYPE p4_error_count counter" >> "$tmpfname"
//     while read count level ss_id error_id
//     do
//         if [[ ! -z ${ss_id:-} ]]; then
//             subsystem=${subsystems[$ss_id]}
//             [[ -z "$subsystem" ]] && subsystem=$ss_id
//             echo "p4_error_count{${serverid_label}${sdpinst_label},subsystem=\"$subsystem\",error_id=\"$error_id\",level=\"$level\"} $count" >> "$tmpfname"
//         fi
//     done < <(awk -F, -v indID="$indID" -v indSS="$indSS" -v indSeverity="$indSeverity" '{printf "%s %s %s\n", $indID, $indSS, $indSeverity}' "$errors_file" | sort | uniq -c)

//     chmod 644 "$tmpfname"
//     mv "$tmpfname" "$fname"
// }
