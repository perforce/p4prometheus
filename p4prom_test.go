package main

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	p4dlog "github.com/rcowham/go-libp4dlog"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/p4prometheus/config"
	"github.com/sirupsen/logrus"
)

func getResult(output chan string) []string {
	lines := []string{}
	for line := range output {
		lines = append(lines, line)
	}
	return lines
}

// Implementation of tailer for testing
type mockTailer struct {
	linechan  chan *fswatcher.Line
	errorchan chan *fswatcher.Error
}

func (t *mockTailer) Lines() chan *fswatcher.Line {
	return t.linechan
}
func (t *mockTailer) Close() {
}
func (t *mockTailer) Errors() chan fswatcher.Error {
	return nil
}

func newMockTailer() *mockTailer {
	return &mockTailer{
		linechan:  make(chan *fswatcher.Line, 100),
		errorchan: make(chan *fswatcher.Error, 1),
	}
}
func (t *mockTailer) addLines(lines []string) {
	for _, l := range lines {
		// t.lines = append(t.lines, l)
		t.linechan <- &fswatcher.Line{Line: l, File: ""}
	}
}

func getOutput(testchan chan string) []string {
	result := make([]string, 0)
	lastoutput := ""
	for output := range testchan {
		lastoutput = output
	}
	eol := regexp.MustCompile("\r\n|\n")
	for _, line := range eol.Split(lastoutput, -1) {
		if len(line) > 0 && !strings.HasPrefix(line, "#") {
			result = append(result, line)
		}
	}
	return result
}

// func assertArraysEqual(t *testing.T, expected []string, val []string){
// }

func TestP4Prom(t *testing.T) {
	cfg := &config.Config{}
	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	logger.SetReportCaller(true)

	tailer := newMockTailer()
	fp := p4dlog.NewP4dFileParser()
	testchan := make(chan string)
	p4p := newP4Prometheus(cfg, logger, testchan)
	p4p.fp = fp
	p4p.logger = logger
	done := make(chan int, 1)
	go func() {
		logger.Debugf("Starting to process events")
		result := p4p.ProcessEvents(10*time.Millisecond, tailer, done)
		logger.Debugf("Finished process events")
		assert.Equal(t, 0, result)
	}()
	time.Sleep(20 * time.Millisecond)
	logger.Debugf("Sending done")
	done <- 1
	logger.Debugf("Getting output")
	lines := getOutput(testchan)
	logger.Debugf("Got output")
	assert.Equal(t, 3, len(lines))
	eol := regexp.MustCompile("\r\n|\n")
	expected := eol.Split(`p4_prom_log_lines_read{serverid=""} 0
p4_prom_cmds_processed{serverid=""} 0
p4_prom_cmds_pending{serverid=""} 0`, -1)
	assert.Equal(t, expected, lines)

}
