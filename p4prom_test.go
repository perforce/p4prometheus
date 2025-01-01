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

func hasPrefix(prefixes []string, line string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func compareOutput(t *testing.T, expected, actual []string) {
	nExpected := make([]string, 0)
	nActual := make([]string, 0)
	// Ignore these elements as the contents varies per test run
	ignorePrefixes := []string{"p4_prom_cmds_pending", "p4_prom_cpu_user", "p4_prom_cpu_system",
		"p4_cmd_cpu_", "p4_prom_memory"}
	for _, line := range expected {
		if !hasPrefix(ignorePrefixes, line) {
			nExpected = append(nExpected, line)
		}
	}
	for _, line := range actual {
		if !hasPrefix(ignorePrefixes, line) {
			nActual = append(nActual, line)
		}
	}
	sort.Strings(nActual)
	sort.Strings(nExpected)
	assert.Equal(t, nExpected, nActual)
}

func basicTest(t *testing.T, cfg *config.Config, input string, historical bool) []string {
	logrus.SetFormatter(&logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true})
	logger.SetReportCaller(true)
	logger.Debugf("Function: %s", funcName())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	linesChan := make(chan string, 100)

	version := &metrics.P4DMetricsVersion{
		Revision:  "testrevision",
		GoVersion: runtime.Version(),
		Version:   "test",
	}
	mconfig := &metrics.Config{
		ServerID:              "myserverid",
		UpdateInterval:        10 * time.Millisecond,
		OutputCmdsByUser:      cfg.OutputCmdsByUser,
		OutputCmdsByUserRegex: cfg.OutputCmdsByUserRegex,
		OutputCmdsByIP:        cfg.OutputCmdsByIP}
	p4m := metrics.NewP4DMetricsLogParser(mconfig, version, logger, historical)

	_, metricsChan := p4m.ProcessEvents(ctx, linesChan, false)

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
		ServerID:              "myserverid",
		UpdateInterval:        10 * time.Millisecond,
		OutputCmdsByUser:      false,
		OutputCmdsByUserRegex: "", // No match so don't output
		OutputCmdsByIP:        false,
	}
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
	baseExpected := eol.Split(`p4_cmd_counter{serverid="myserverid",cmd="user-sync"} 1
p4_cmd_cumulative_seconds{serverid="myserverid",cmd="user-sync"} 0.031
p4_cmd_program_counter{serverid="myserverid",program="p4/2016.2/LINUX26X86_64/1598668"} 1
p4_cmd_program_cumulative_seconds{serverid="myserverid",program="p4/2016.2/LINUX26X86_64/1598668"} 0.031
p4_cmd_running{serverid="myserverid"} 1
p4_prom_cmds_pending{serverid="myserverid"} 0
p4_prom_cmds_processed{serverid="myserverid"} 1
p4_prom_log_lines_read{serverid="myserverid"} 8
p4_sync_bytes_added{serverid="myserverid"} 0
p4_sync_bytes_updated{serverid="myserverid"} 0
p4_sync_files_added{serverid="myserverid"} 0
p4_sync_files_deleted{serverid="myserverid"} 0
p4_sync_files_updated{serverid="myserverid"} 0
p4_cmd_mem_mb{serverid="myserverid"} 0
p4_cmd_mem_peak_mb{serverid="myserverid"} 0
p4_cmds_paused_cumulative{serverid="myserverid"} 0.000
p4_cmds_paused_errors{serverid="myserverid"} 0
p4_cmds_paused_max{serverid="myserverid"} 0
p4_cmds_paused{serverid="myserverid"} 0
p4_cmds_running_max{serverid="myserverid"} 0
p4_cmds_running{serverid="myserverid"} 1
p4_lbr_binary_checkins{serverid="myserverid"} 0
p4_lbr_binary_closes{serverid="myserverid"} 0
p4_lbr_binary_copies{serverid="myserverid"} 0
p4_lbr_binary_digests{serverid="myserverid"} 0
p4_lbr_binary_exists{serverid="myserverid"} 0
p4_lbr_binary_filesizes{serverid="myserverid"} 0
p4_lbr_binary_modtimes{serverid="myserverid"} 0
p4_lbr_binary_opens{serverid="myserverid"} 0
p4_lbr_binary_readbytes{serverid="myserverid"} 0
p4_lbr_binary_reads{serverid="myserverid"} 0
p4_lbr_binary_writebytes{serverid="myserverid"} 0
p4_lbr_binary_writes{serverid="myserverid"} 0
p4_lbr_compress_checkins{serverid="myserverid"} 0
p4_lbr_compress_closes{serverid="myserverid"} 0
p4_lbr_compress_copies{serverid="myserverid"} 0
p4_lbr_compress_digests{serverid="myserverid"} 0
p4_lbr_compress_exists{serverid="myserverid"} 0
p4_lbr_compress_filesizes{serverid="myserverid"} 0
p4_lbr_compress_modtimes{serverid="myserverid"} 0
p4_lbr_compress_opens{serverid="myserverid"} 0
p4_lbr_compress_readbytes{serverid="myserverid"} 0
p4_lbr_compress_reads{serverid="myserverid"} 0
p4_lbr_compress_writebytes{serverid="myserverid"} 0
p4_lbr_compress_writes{serverid="myserverid"} 0
p4_lbr_rcs_checkins{serverid="myserverid"} 0
p4_lbr_rcs_closes{serverid="myserverid"} 0
p4_lbr_rcs_copies{serverid="myserverid"} 0
p4_lbr_rcs_digests{serverid="myserverid"} 0
p4_lbr_rcs_exists{serverid="myserverid"} 0
p4_lbr_rcs_filesizes{serverid="myserverid"} 0
p4_lbr_rcs_modtimes{serverid="myserverid"} 0
p4_lbr_rcs_opens{serverid="myserverid"} 0
p4_lbr_rcs_readbytes{serverid="myserverid"} 0
p4_lbr_rcs_reads{serverid="myserverid"} 0
p4_lbr_rcs_writebytes{serverid="myserverid"} 0
p4_lbr_rcs_writes{serverid="myserverid"} 0
p4_lbr_uncompress_checkins{serverid="myserverid"} 0
p4_lbr_uncompress_closes{serverid="myserverid"} 0
p4_lbr_uncompress_copies{serverid="myserverid"} 0
p4_lbr_uncompress_digests{serverid="myserverid"} 0
p4_lbr_uncompress_exists{serverid="myserverid"} 0
p4_lbr_uncompress_filesizes{serverid="myserverid"} 0
p4_lbr_uncompress_modtimes{serverid="myserverid"} 0
p4_lbr_uncompress_opens{serverid="myserverid"} 0
p4_lbr_uncompress_readbytes{serverid="myserverid"} 0
p4_lbr_uncompress_reads{serverid="myserverid"} 0
p4_lbr_uncompress_writebytes{serverid="myserverid"} 0
p4_lbr_uncompress_writes{serverid="myserverid"} 0
p4_pause_rate_cpu{serverid="myserverid"} 0
p4_pause_rate_mem{serverid="myserverid"} 0
p4_pause_state_cpu{serverid="myserverid"} 0
p4_pause_state_mem{serverid="myserverid"} 0
p4_prom_build_info{serverid="myserverid",goversion="go1.21.5",revision="testrevision",version="test"} 1
p4_prom_memory{serverid="myserverid"} 8283400
p4_prom_svr_events_processed{serverid="myserverid"} 0`, -1)
	compareOutput(t, baseExpected, output)

	historical = true
	output = basicTest(t, cfg, input, historical)
	// Cross check appropriate time is being produced for historical runs
	assert.Contains(t, output[0], fmt.Sprintf("%d", cmdTime.Unix()))
	baseExpectedHistorical := eol.Split(`p4_cmd_counter;serverid=myserverid;cmd=user-sync 1 1441207389
p4_cmd_cumulative_seconds;serverid=myserverid;cmd=user-sync 0.031 1441207389
p4_cmd_program_counter;serverid=myserverid;program=p4/2016.2/LINUX26X86_64/1598668 1 1441207389
p4_cmd_program_cumulative_seconds;serverid=myserverid;program=p4/2016.2/LINUX26X86_64/1598668 0.031 1441207389
p4_cmd_running;serverid=myserverid 1 1441207389
p4_prom_cmds_pending;serverid=myserverid 0 1441207389
p4_prom_cmds_processed;serverid=myserverid 1 1441207389
p4_prom_log_lines_read;serverid=myserverid 8 1441207389
p4_sync_bytes_added;serverid=myserverid 0 1441207389
p4_sync_bytes_updated;serverid=myserverid 0 1441207389
p4_sync_files_added;serverid=myserverid 0 1441207389
p4_sync_files_deleted;serverid=myserverid 0 1441207389
p4_sync_files_updated;serverid=myserverid 0 1441207389
p4_cmd_mem_mb;serverid=myserverid 0 1441207389
p4_cmd_mem_peak_mb;serverid=myserverid 0 1441207389
p4_cmds_paused;serverid=myserverid 0 1441207389
p4_cmds_paused_cumulative;serverid=myserverid 0.000 1441207389
p4_cmds_paused_errors;serverid=myserverid 0 1441207389
p4_cmds_paused_max;serverid=myserverid 0 1441207389
p4_cmds_running;serverid=myserverid 1 1441207389
p4_cmds_running_max;serverid=myserverid 0 1441207389
p4_lbr_binary_checkins;serverid=myserverid 0 1441207389
p4_lbr_binary_closes;serverid=myserverid 0 1441207389
p4_lbr_binary_copies;serverid=myserverid 0 1441207389
p4_lbr_binary_digests;serverid=myserverid 0 1441207389
p4_lbr_binary_exists;serverid=myserverid 0 1441207389
p4_lbr_binary_filesizes;serverid=myserverid 0 1441207389
p4_lbr_binary_modtimes;serverid=myserverid 0 1441207389
p4_lbr_binary_opens;serverid=myserverid 0 1441207389
p4_lbr_binary_readbytes;serverid=myserverid 0 1441207389
p4_lbr_binary_reads;serverid=myserverid 0 1441207389
p4_lbr_binary_writebytes;serverid=myserverid 0 1441207389
p4_lbr_binary_writes;serverid=myserverid 0 1441207389
p4_lbr_compress_checkins;serverid=myserverid 0 1441207389
p4_lbr_compress_closes;serverid=myserverid 0 1441207389
p4_lbr_compress_copies;serverid=myserverid 0 1441207389
p4_lbr_compress_digests;serverid=myserverid 0 1441207389
p4_lbr_compress_exists;serverid=myserverid 0 1441207389
p4_lbr_compress_filesizes;serverid=myserverid 0 1441207389
p4_lbr_compress_modtimes;serverid=myserverid 0 1441207389
p4_lbr_compress_opens;serverid=myserverid 0 1441207389
p4_lbr_compress_readbytes;serverid=myserverid 0 1441207389
p4_lbr_compress_reads;serverid=myserverid 0 1441207389
p4_lbr_compress_writebytes;serverid=myserverid 0 1441207389
p4_lbr_compress_writes;serverid=myserverid 0 1441207389
p4_lbr_rcs_checkins;serverid=myserverid 0 1441207389
p4_lbr_rcs_closes;serverid=myserverid 0 1441207389
p4_lbr_rcs_copies;serverid=myserverid 0 1441207389
p4_lbr_rcs_digests;serverid=myserverid 0 1441207389
p4_lbr_rcs_exists;serverid=myserverid 0 1441207389
p4_lbr_rcs_filesizes;serverid=myserverid 0 1441207389
p4_lbr_rcs_modtimes;serverid=myserverid 0 1441207389
p4_lbr_rcs_opens;serverid=myserverid 0 1441207389
p4_lbr_rcs_readbytes;serverid=myserverid 0 1441207389
p4_lbr_rcs_reads;serverid=myserverid 0 1441207389
p4_lbr_rcs_writebytes;serverid=myserverid 0 1441207389
p4_lbr_rcs_writes;serverid=myserverid 0 1441207389
p4_lbr_uncompress_checkins;serverid=myserverid 0 1441207389
p4_lbr_uncompress_closes;serverid=myserverid 0 1441207389
p4_lbr_uncompress_copies;serverid=myserverid 0 1441207389
p4_lbr_uncompress_digests;serverid=myserverid 0 1441207389
p4_lbr_uncompress_exists;serverid=myserverid 0 1441207389
p4_lbr_uncompress_filesizes;serverid=myserverid 0 1441207389
p4_lbr_uncompress_modtimes;serverid=myserverid 0 1441207389
p4_lbr_uncompress_opens;serverid=myserverid 0 1441207389
p4_lbr_uncompress_readbytes;serverid=myserverid 0 1441207389
p4_lbr_uncompress_reads;serverid=myserverid 0 1441207389
p4_lbr_uncompress_writebytes;serverid=myserverid 0 1441207389
p4_lbr_uncompress_writes;serverid=myserverid 0 1441207389
p4_pause_rate_cpu;serverid=myserverid 0 1441207389
p4_pause_rate_mem;serverid=myserverid 0 1441207389
p4_pause_state_cpu;serverid=myserverid 0 1441207389
p4_pause_state_mem;serverid=myserverid 0 1441207389
p4_prom_build_info;serverid=myserverid;goversion=go1.21.5;revision=testrevision;version=test 1 1441207389
p4_prom_memory;serverid=myserverid 9405704 1441207389
p4_prom_svr_events_processed;serverid=myserverid 0 1441207389`, -1)
	compareOutput(t, baseExpectedHistorical, output)

	// Now change config and expect some extra metrics to be output
	cfg = &config.Config{
		ServerID:              "myserverid",
		UpdateInterval:        10 * time.Millisecond,
		OutputCmdsByUser:      true,
		OutputCmdsByUserRegex: ".*", // all users
		OutputCmdsByIP:        true,
	}

	expected := eol.Split(`p4_cmd_ip_counter{serverid="myserverid",ip="127.0.0.1"} 1
p4_cmd_ip_cumulative_seconds{serverid="myserverid",ip="127.0.0.1"} 0.031
p4_cmd_user_counter{serverid="myserverid",user="robert"} 1
p4_cmd_user_cumulative_seconds{serverid="myserverid",user="robert"} 0.031
p4_cmd_user_detail_counter{serverid="myserverid",user="robert",cmd="user-sync"} 1
p4_cmd_user_detail_cumulative_seconds{serverid="myserverid",user="robert",cmd="user-sync"} 0.031`, -1)
	for _, l := range baseExpected {
		expected = append(expected, l)
	}
	sort.Strings(expected)
	historical = false
	output = basicTest(t, cfg, input, historical)
	compareOutput(t, expected, output)

	expected = eol.Split(`p4_cmd_ip_counter;serverid=myserverid;ip=127.0.0.1 1 1441207389
p4_cmd_ip_cumulative_seconds;serverid=myserverid;ip=127.0.0.1 0.031 1441207389
p4_cmd_user_counter;serverid=myserverid;user=robert 1 1441207389
p4_cmd_user_cumulative_seconds;serverid=myserverid;user=robert 0.031 1441207389
p4_cmd_user_detail_counter;serverid=myserverid;user=robert;cmd=user-sync 1 1441207389
p4_cmd_user_detail_cumulative_seconds;serverid=myserverid;user=robert;cmd=user-sync 0.031 1441207389`, -1)
	for _, l := range baseExpectedHistorical {
		expected = append(expected, l)
	}
	sort.Strings(expected)
	historical = true
	output = basicTest(t, cfg, input, historical)
	compareOutput(t, expected, output)

}
