package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type JournalMetric struct {
	Table  string
	Record string
}

func extractJournalField(token string) string {
	if len(token) < 2 {
		return ""
	}
	if token[0] == '@' && token[len(token)-1] == '@' {
		return token[1 : len(token)-1]
	}
	return ""
}

func (p4m *P4MonitorMetrics) parseJournalLine(line string) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}

	recordType := extractJournalField(fields[0])
	switch recordType {
	case "rv", "pv", "dv":
	default:
		return
	}

	table := extractJournalField(fields[2])
	if !strings.HasPrefix(table, "db.") {
		return
	}
	table = strings.TrimPrefix(table, "db.")
	if table == "" {
		return
	}

	m := JournalMetric{Table: table, Record: recordType}
	p4m.journalLock.Lock() // Accessed from tailer goroutine and monitor loop.
	p4m.journalMetrics[m] += 1
	p4m.journalLock.Unlock()
}

// Loop reading tailing journal log and recording per-table counts for rv/pv/dv records.
func (p4m *P4MonitorMetrics) runJournalTailer(logger *logrus.Logger, logcfg *logConfig) {
	tailer, err := p4m.getTailer(logcfg)
	if err != nil {
		logger.Errorf("error starting to tail journal lines: %v", err)
		return
	}
	p4m.logger.Debug("runJournalTailer on journal")
	p4m.journalTailer = &tailer

	for {
		select {
		case line, ok := <-tailer.Lines():
			if ok {
				p4m.parseJournalLine(line.Line)
			} else {
				p4m.journalTailer = nil
				return
			}
		case err := <-tailer.Errors():
			if err != nil {
				if os.IsNotExist(err.Cause()) {
					p4m.logger.Errorf("error reading P4JOURNAL lines: %v: use 'fail_on_missing_logfile: false' in the input configuration if you want p4metrics to start even though the logfile is missing", err)
					p4m.journalTailer = nil
					return
				}
				p4m.logger.Errorf("error reading P4JOURNAL lines: %v", err)
				p4m.journalTailer = nil
				return
			}
			p4m.journalTailer = nil
			return
		}
	}
}

func (p4m *P4MonitorMetrics) setupJournalMonitoring() {
	p4m.logger.Debugf("setupJournalMonitoring starting")
	if p4m.p4journal == "" {
		p4m.logger.Debugf("setupJournalMonitoring exiting as no P4JOURNAL")
		return
	}
	if p4m.journalTailer != nil {
		p4m.logger.Debugf("setupJournalMonitoring exiting as already running")
		return
	}

	logcfg := &logConfig{
		Type:                 "file",
		Path:                 p4m.p4journal,
		PollInterval:         time.Second * 30,
		Readall:              false,
		FailOnMissingLogfile: false,
		MaxLineBytes:         100,
	}
	p4m.runJournalTailer(p4m.logger, logcfg)
}

func (p4m *P4MonitorMetrics) monitorJournalRecords() {
	p4m.startMonitor("monitorJournalRecords", "p4_journal_records")
	defer p4m.completeMonitor()

	p4m.journalLock.Lock()
	defer p4m.journalLock.Unlock()

	for m, count := range p4m.journalMetrics {
		p4m.metrics = append(p4m.metrics,
			metricStruct{name: "p4_journal_records_count",
				help:  "P4JOURNAL record count by table and type",
				mtype: "counter",
				value: strconv.Itoa(count),
				labels: []labelStruct{{name: "table", value: m.Table},
					{name: "record", value: m.Record},
				}})
	}
	p4m.writeMetricsFile()
}
