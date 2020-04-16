package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/perforce/p4prometheus/config"
	p4dlog "github.com/rcowham/go-libp4dlog"
	metrics "github.com/rcowham/go-libp4dlog/metrics"
	"github.com/sirupsen/logrus"
)

var (
	eol    = regexp.MustCompile("\r\n|\n")
	logger = &logrus.Logger{Out: os.Stderr,
		Formatter: &logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true},
		// Level:     logrus.DebugLevel}
		Level: logrus.InfoLevel}
)

func getResult(output chan string) []string {
	lines := []string{}
	for line := range output {
		lines = append(lines, line)
	}
	return lines
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

// Assuming there are several outputs - this returns the latest one unless historical
func getOutput(testchan chan string, historical bool) []string {
	result := make([]string, 0)
	lastoutput := ""
	if historical {
		for output := range testchan {
			for _, line := range eol.Split(output, -1) {
				if len(line) > 0 && !strings.HasPrefix(line, "#") {
					result = append(result, line)
				}
			}
		}
	} else {
		for output := range testchan {
			lastoutput = output
		}
		for _, line := range eol.Split(lastoutput, -1) {
			if len(line) > 0 && !strings.HasPrefix(line, "#") {
				result = append(result, line)
			}
		}
	}
	sort.Strings(result)
	return result
}

func basicTest(t *testing.T, cfg *config.Config, input string, historical bool) []string {
	logrus.SetFormatter(&logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true})
	logger.SetReportCaller(true)
	logger.Infof("Function: %s", funcName())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fp := p4dlog.NewP4dFileParser(logger)
	fp.SetDebugMode()
	fp.SetDurations(10*time.Millisecond, 20*time.Millisecond)
	linesChan := make(chan string, 100)
	p4p := newP4Prometheus(cfg, logger, historical)
	p4p.fp = fp

	mconfig := &metrics.Config{
		ServerID:         "myserverid",
		UpdateInterval:   10 * time.Millisecond,
		OutputCmdsByUser: true}
	p4m := metrics.NewP4DMetricsLogParser(mconfig, logger, historical)

	_, metricsChan := p4m.ProcessEvents(ctx, linesChan)

	var wg sync.WaitGroup

	for _, l := range eol.Split(input, -1) {
		linesChan <- l
	}

	output := []string{}

	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		logger.Debugf("Waiting for metrics")
		output = getOutput(metricsChan, historical)
	}()

	wg.Add(1)
	time.Sleep(50 * time.Millisecond)
	close(linesChan)
	logger.Debugf("Waiting for finish")
	wg.Wait()
	logger.Debugf("Finished")
	return output
}

func TestP4PromBasic(t *testing.T) {
	cfg := &config.Config{
		ServerID:         "myserverid",
		UpdateInterval:   10 * time.Millisecond,
		OutputCmdsByUser: true}
	input := `
Perforce server info:
	2015/09/02 15:23:09 pid 1616 robert@robert-test 127.0.0.1 [p4/2016.2/LINUX26X86_64/1598668] 'user-sync //...'
Perforce server info:
	2015/09/02 15:23:09 pid 1616 compute end .031s
Perforce server info:
	2015/09/02 15:23:09 pid 1616 completed .031s
`
	cmdTime, _ := time.Parse(p4timeformat, "2015/09/02 15:23:09")
	historical := false
	output := basicTest(t, cfg, input, historical)

	assert.Equal(t, 10, len(output))
	expected := eol.Split(`p4_cmd_counter{serverid="myserverid",cmd="user-sync"} 1
p4_cmd_cumulative_seconds{serverid="myserverid",cmd="user-sync"} 0.031
p4_cmd_running{serverid="myserverid"} 0
p4_cmd_system_cpu_cumulative_seconds{serverid="myserverid",cmd="user-sync"} 0.000
p4_cmd_user_counter{serverid="myserverid",user="robert"} 1
p4_cmd_user_cpu_cumulative_seconds{serverid="myserverid",cmd="user-sync"} 0.000
p4_cmd_user_cumulative_seconds{serverid="myserverid",user="robert"} 0.031
p4_prom_cmds_pending{serverid="myserverid"} 0
p4_prom_cmds_processed{serverid="myserverid"} 1
p4_prom_log_lines_read{serverid="myserverid"} 8`, -1)
	assert.Equal(t, expected, output)

	historical = true
	output = basicTest(t, cfg, input, historical)

	assert.Equal(t, 10, len(output))
	// Cross check appropriate time is being produced for historical runs
	assert.Contains(t, output[0], fmt.Sprintf("%d", cmdTime.Unix()))
	expected = eol.Split(`p4_cmd_counter;serverid=myserverid;cmd=user-sync 1 1441207389
p4_cmd_cumulative_seconds;serverid=myserverid;cmd=user-sync 0.031 1441207389
p4_cmd_running;serverid=myserverid 0 1441207389
p4_cmd_system_cpu_cumulative_seconds;serverid=myserverid;cmd=user-sync 0.000 1441207389
p4_cmd_user_counter;serverid=myserverid;user=robert 1 1441207389
p4_cmd_user_cpu_cumulative_seconds;serverid=myserverid;cmd=user-sync 0.000 1441207389
p4_cmd_user_cumulative_seconds;serverid=myserverid;user=robert 0.031 1441207389
p4_prom_cmds_pending;serverid=myserverid 0 1441207389
p4_prom_cmds_processed;serverid=myserverid 1 1441207389
p4_prom_log_lines_read;serverid=myserverid 8 1441207389`, -1)
	assert.Equal(t, expected, output)

}
