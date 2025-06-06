// This is created from p4d error defintions and is used to parse structured log errors.csv
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rcowham/go-libtail/tailer"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/go-libtail/tailer/glob"
	"github.com/sirupsen/logrus"
)

type ErrorSeverity int

const (
	E_EMPTY  ErrorSeverity = 0 // nothing yet
	E_INFO   ErrorSeverity = 1 // something good happened
	E_WARN   ErrorSeverity = 2 // something not good happened
	E_FAILED ErrorSeverity = 3 // user did somthing wrong
	E_FATAL  ErrorSeverity = 4 // system broken -- nothing can continue
)

type ErrorGeneric int

const (
	EV_NONE ErrorGeneric = 0 // misc
	// The fault of the user
	EV_USAGE   ErrorGeneric = 0x01 // request not consistent with dox
	EV_UNKNOWN ErrorGeneric = 0x02 // using unknown entity
	EV_CONTEXT ErrorGeneric = 0x03 // using entity in wrong context
	EV_ILLEGAL ErrorGeneric = 0x04 // trying to do something you can't
	EV_NOTYET  ErrorGeneric = 0x05 // something must be corrected first
	EV_PROTECT ErrorGeneric = 0x06 // protections prevented operation
	// No fault at all
	EV_EMPTY ErrorGeneric = 0x11 // action returned empty results
	// not the fault of the user
	EV_FAULT   ErrorGeneric = 0x21 // inexplicable program fault
	EV_CLIENT  ErrorGeneric = 0x22 // client side program errors
	EV_ADMIN   ErrorGeneric = 0x23 // server administrative action required
	EV_CONFIG  ErrorGeneric = 0x24 // client configuration inadequate
	EV_UPGRADE ErrorGeneric = 0x25 // client or server too old to interact
	EV_COMM    ErrorGeneric = 0x26 // communications error
	EV_TOOBIG  ErrorGeneric = 0x27 // not ever Perforce can handle this much
)

type ErrorSubsystem int

const (
	ES_OS       ErrorSubsystem = 0  // OS error
	ES_SUPP     ErrorSubsystem = 1  // Misc support
	ES_LBR      ErrorSubsystem = 2  // librarian
	ES_RPC      ErrorSubsystem = 3  // messaging
	ES_DB       ErrorSubsystem = 4  // database
	ES_DBSUPP   ErrorSubsystem = 5  // database support
	ES_DM       ErrorSubsystem = 6  // data manager
	ES_SERVER   ErrorSubsystem = 7  // top level of server
	ES_CLIENT   ErrorSubsystem = 8  // top level of client
	ES_INFO     ErrorSubsystem = 9  // pseudo subsystem for information messages
	ES_HELP     ErrorSubsystem = 10 // pseudo subsystem for help messages
	ES_SPEC     ErrorSubsystem = 11 // pseudo subsystem for spec/comment messages
	ES_FTPD     ErrorSubsystem = 12 // P4FTP server
	ES_BROKER   ErrorSubsystem = 13 // Perforce Broker
	ES_P4QT     ErrorSubsystem = 14 // P4V and other Qt based clients
	ES_X3SERVER ErrorSubsystem = 15 // P4X3 server
	ES_GRAPH    ErrorSubsystem = 16 // graph depot messages
	ES_SCRIPT   ErrorSubsystem = 17 // scripting
	ES_SERVER2  ErrorSubsystem = 18 // server overflow
	ES_DM2      ErrorSubsystem = 19 // dm overflow
	ES_CONFIG   ErrorSubsystem = 20 // help for configurables
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
	p4m.errLock.Lock() // Because accessing on a different thread
	p4m.errorMetrics[m] += 1
	p4m.errLock.Unlock()
	p4m.logger.Debugf("All error count %v", p4m.errorMetrics)
}

// Loop reading tailing error log and writing metrics when appropriate
func (p4m *P4MonitorMetrics) runLogTailer(logger *logrus.Logger, logcfg *logConfig) {

	tailer, err := p4m.getTailer(logcfg)
	if err != nil {
		logger.Errorf("error starting to tail log lines: %v", err)
		return
	}
	p4m.logger.Debug("runLogTailer on errors")
	p4m.errTailer = &tailer

	for {
		select {
		case line, ok := <-tailer.Lines():
			if ok {
				p4m.logger.Debugf("Error line %q", line.Line)
				p4m.parseErrorLine(line.Line)
			} else {
				p4m.logger.Debug("Tail error")
				p4m.errTailer = nil
				return
			}
		case err := <-tailer.Errors():
			if err != nil {
				if os.IsNotExist(err.Cause()) {
					p4m.logger.Errorf("error reading errors.csv lines: %v: use 'fail_on_missing_logfile: false' in the input configuration if you want p4metrics to start even though the logfile is missing", err)
					p4m.errTailer = nil
					return
				}
				p4m.logger.Errorf("error reading errors.csv lines: %v", err)
				p4m.errTailer = nil
				return
			}
			p4m.logger.Debug("Finishing logTailer")
			p4m.errTailer = nil
			return
		}
	}
}

func (p4m *P4MonitorMetrics) setupErrorMonitoring() {
	p4m.logger.Debugf("setupErrorMonitoring starting")
	// Parse the errors.csv file
	if p4m.p4errorsCSV == "" {
		p4m.logger.Debugf("setupErrorMonitoring exiting as no errors.csv")
		return
	}
	if p4m.errTailer != nil {
		p4m.logger.Debugf("setupErrorMonitoring exiting as already running")
		return
	}

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
		Path:                 p4m.p4errorsCSV,
		PollInterval:         time.Second * 30,
		Readall:              false,
		FailOnMissingLogfile: false,
	}
	p4m.runLogTailer(p4m.logger, logcfg)
}
