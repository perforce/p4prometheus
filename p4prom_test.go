package main

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	p4dlog "github.com/rcowham/go-libp4dlog"
	"github.com/rcowham/go-libtail/tailer/fswatcher"
	"github.com/rcowham/p4prometheus/config"
	"github.com/sirupsen/logrus"
)

var (
	eol    = regexp.MustCompile("\r\n|\n")
	logger = &logrus.Logger{Out: os.Stderr,
		Formatter: &logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true},
		Level:     logrus.DebugLevel}
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
	close(t.linechan)
}
func (t *mockTailer) Errors() chan fswatcher.Error {
	return nil
}

func newMockTailer() *mockTailer {
	return &mockTailer{
		linechan:  make(chan *fswatcher.Line, 1000),
		errorchan: make(chan *fswatcher.Error, 10),
	}
}
func (t *mockTailer) addLines(lines []string) {
	for _, l := range lines {
		// t.lines = append(t.lines, l)
		t.linechan <- &fswatcher.Line{Line: l, File: ""}
	}
}

func funcName() string {
	fpcs := make([]uintptr, 1)
	// Skip 2 levels to get the caller
	n := runtime.Callers(2, fpcs)
	if n == 0 {
		return ""
	}
	caller := runtime.FuncForPC(fpcs[0] - 1)
	if caller == nil {
		return ""
	}
	return caller.Name()
}

// Assuming there are several outputs - this returns the latest one
func getOutput(testchan chan string) []string {
	result := make([]string, 0)
	lastoutput := ""
	for output := range testchan {
		lastoutput = output
	}
	for _, line := range eol.Split(lastoutput, -1) {
		if len(line) > 0 && !strings.HasPrefix(line, "#") {
			result = append(result, line)
		}
	}
	return result
}

// func basicTest(cfg *config.Config, input string) []string {

// }

func TestP4PromBasic(t *testing.T) {
	cfg := &config.Config{
		ServerID:         "myserverid",
		UpdateInterval:   10 * time.Millisecond,
		OutputCmdsByUser: true}
	logrus.SetFormatter(&logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true})
	logger.SetReportCaller(true)
	logger.Infof("Function: %s", funcName())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fp := p4dlog.NewP4dFileParser(logger)
	fp.SetDebugMode()
	fp.SetDurations(10*time.Millisecond, 20*time.Millisecond)
	lines := make(chan []byte, 100)
	metrics := make(chan string, 100)
	p4p := newP4Prometheus(cfg, logger)
	p4p.fp = fp

	var wg sync.WaitGroup

	go func() {
		defer wg.Done()
		logger.Debugf("Starting  LogParser")
		fp.LogParser(ctx, p4p.lines, p4p.cmdchan)
		logger.Debugf("Finished LogParser")
	}()

	go func() {
		defer wg.Done()
		logger.Debugf("Starting to process events")
		result := p4p.ProcessEvents(ctx, lines, metrics)
		logger.Debugf("Finished process events")
		assert.Equal(t, 0, result)
	}()

	input := eol.Split(`
Perforce server info:
	2015/09/02 15:23:09 pid 1616 robert@robert-test 127.0.0.1 [p4/2016.2/LINUX26X86_64/1598668] 'user-sync //...'
Perforce server info:
	2015/09/02 15:23:09 pid 1616 compute end .031s
Perforce server info:
	2015/09/02 15:23:09 pid 1616 completed .031s
`, -1)
	for _, l := range input {
		lines <- []byte(l)
	}

	output := []string{}

	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		logger.Debugf("Waiting for metrics")
		output = getOutput(metrics)
	}()

	wg.Add(3)
	time.Sleep(50 * time.Millisecond)
	close(lines)
	logger.Debugf("Waiting for finish")
	wg.Wait()
	logger.Debugf("Finished")
	assert.Equal(t, 7, len(output))
	expected := eol.Split(`p4_prom_log_lines_read{serverid="myserverid"} 8
p4_prom_cmds_processed{serverid="myserverid"} 1
p4_prom_cmds_pending{serverid="myserverid"} 0
p4_cmd_counter{cmd="user-sync",serverid="myserverid"} 1
p4_cmd_cumulative_seconds{cmd="user-sync",serverid="myserverid"} 0.031
p4_cmd_user_counter{user="robert",serverid="myserverid"} 1
p4_cmd_user_cumulative_seconds{user="robert",serverid="myserverid"} 0.031`, -1)
	assert.Equal(t, expected, output)

}

// func TestP4PromBasic2(t *testing.T) {
// 	cfg := &config.Config{SserverID: "myserverid"}
// 	logger := logrus.New()
// 	logger.Level = logrus.DebugLevel
// 	logger.SetReportCaller(true)

// 	tailer := newMockTailer()
// 	fp := p4dlogrus.NewP4dFileParser()
// 	fp.SetDebugMode()
// 	testchan := make(chan string)
// 	p4p := newP4Prometheus(cfg, logger, testchan)
// 	p4p.fp = fp
// 	p4p.logger = logger
// 	done := make(chan int, 1)
// 	go fp.LogParser(p4p.lines, p4p.events, nil)
// 	go func() {
// 		logger.Debugf("Starting to process events")
// 		result := p4p.ProcessEvents(10*time.Millisecond, tailer, done)
// 		logger.Debugf("Finished process events")
// 		assert.Equal(t, 0, result)
// 	}()
// 	input := eol.Split(`
// Perforce server info:
// 	2017/12/07 15:00:21 pid 148469 Fred@LONWS 10.40.16.14/10.40.48.29 [3DSMax/1.0.0.0] 'user-change -i' trigger swarm.changesave
// lapse .044s
// Perforce server info:
// 	2017/12/07 15:00:21 pid 148469 completed .413s 7+4us 0+584io 0+0net 4580k 0pf
// Perforce server info:
// 	2017/12/07 15:00:21 pid 148469 Fred@LONWS 10.40.16.14/10.40.48.29 [3DSMax/1.0.0.0] 'user-change -i'
// --- lapse .413s
// --- usage 10+11us 12+13io 14+15net 4088k 22pf
// --- rpc msgs/size in+out 20+21/22mb+23mb himarks 318788/318789 snd/rcv .001s/.002s
// --- db.counters
// ---   pages in+out+cached 6+3+2
// ---   locks read/write 0/2 rows get+pos+scan put+del 2+0+0 1+0

// Perforce server info:
// 	2018/06/10 23:30:08 pid 25568 fred@lon_ws 10.1.2.3 [p4/2016.2/LINUX26X86_64/1598668] 'dm-CommitSubmit'

// Perforce server info:
// 	2018/06/10 23:30:08 pid 25568 fred@lon_ws 10.1.2.3 [p4/2016.2/LINUX26X86_64/1598668] 'dm-CommitSubmit'
// --- meta/commit(W)
// ---   total lock wait+held read/write 0ms+0ms/0ms+795ms

// Perforce server info:
// 	2018/06/10 23:30:08 pid 25568 fred@lon_ws 10.1.2.3 [p4/2016.2/LINUX26X86_64/1598668] 'dm-CommitSubmit'
// --- clients/MCM_client_184%2E51%2E33%2E29_prod_prefix1(W)
// ---   total lock wait+held read/write 0ms+0ms/0ms+1367ms

// Perforce server info:
// 	2018/06/10 23:30:09 pid 25568 completed 1.38s 34+61us 59680+59904io 0+0net 127728k 1pf
// Perforce server info:
// 	2018/06/10 23:30:08 pid 25568 fred@lon_ws 10.1.2.3 [p4/2016.2/LINUX26X86_64/1598668] 'dm-CommitSubmit'
// --- db.integed
// ---   total lock wait+held read/write 0ms+0ms/0ms+795ms
// --- db.archmap
// ---   total lock wait+held read/write 0ms+0ms/0ms+780ms`, -1)
// 	tailer.addLines(input)
// 	time.Sleep(40 * time.Millisecond)
// 	// time.Sleep(4 * time.Second)
// 	logger.Debugf("Sending done")
// 	done <- 1
// 	time.Sleep(10 * time.Millisecond)
// 	logger.Debugf("Getting output")
// 	lines := getOutput(testchan)
// 	logger.Debugf("Got output")
// 	assert.Equal(t, 3, len(lines))
// 	expected := eol.Split(`p4_prom_log_lines_read{serverid="myserverid"} 36
// p4_prom_cmds_processed{serverid="myserverid"} 0
// p4_prom_cmds_pending{serverid="myserverid"} 0`, -1)
// 	assert.Equal(t, expected, lines)

// }
