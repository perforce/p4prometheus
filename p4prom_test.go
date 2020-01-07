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
	eol := regexp.MustCompile("\r\n|\n")
	for output := range testchan {
		for _, line := range eol.Split(output, -1) {
			if len(line) > 0 && !strings.HasPrefix(line, "#") {
				result = append(result, line)
			}
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
		logger.Infof("Starting to process events")
		result := p4p.ProcessEvents(10*time.Millisecond, tailer, done)
		logger.Infof("Finished process events")
		assert.Equal(t, 0, result)
	}()
	time.Sleep(20 * time.Millisecond)
	logger.Infof("Sending done")
	done <- 1
	logger.Infof("Getting output")
	lines := getOutput(testchan)
	logger.Infof("Got output")
	assert.Equal(t, 3, len(lines))
	eol := regexp.MustCompile("\r\n|\n")
	expected := eol.Split(`p4_prom_log_lines_read{serverid=""} 0
p4_prom_cmds_processed{serverid=""} 0
p4_prom_cmds_pending{serverid=""} 0`, -1)
	assert.Equal(t, expected, lines)

	// 	testInput := `
	// Perforce server info:
	// 	2015/09/02 15:23:09 pid 1616 robert@robert-test 127.0.0.1 [Microsoft Visual Studio 2013/12.0.21005.1] 'user-sync //...'
	// Perforce server info:
	// 	2015/09/02 15:23:09 pid 1616 compute end .031s
	// Perforce server info:
	// 	2015/09/02 15:23:09 pid 1616 completed .031s`

	// go fp.P4LogParseFile(*opts, outchan)
	// output := getResult(outchan)
	// assert.Equal(t, `{"processKey":"4d4e5096f7b732e4ce95230ef085bf51","cmd":"user-sync","pid":1616,"lineNo":2,"user":"robert","workspace":"robert-test","computeLapse":0.031,"completedLapse":0.031,"ip":"127.0.0.1","app":"Microsoft Visual Studio 2013/12.0.21005.1","args":"//...","startTime":"2015/09/02 15:23:09","endTime":"2015/09/02 15:23:09","running":0,"uCpu":0,"sCpu":0,"diskIn":0,"diskOut":0,"ipcIn":0,"ipcOut":0,"maxRss":0,"pageFaults":0,"rpcMsgsIn":0,"rpcMsgsOut":0,"rpcSizeIn":0,"rpcSizeOut":0,"rpcHimarkFwd":0,"rpcHimarkRev":0,"rpcSnd":0,"rpcRcv":0,"cmdError":false,"tables":[]}`,
	// 	output[0])

}
