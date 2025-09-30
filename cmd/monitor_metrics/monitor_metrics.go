// monitor_metrics.go: Go port of monitor_metrics.py
// Monitors Perforce locks and metrics for Prometheus
// Copyright info: see LICENSE in SDP

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	metricsRootDefault = "/p4/metrics"
	metricsFile        = "locks.prom"
	logDirDefault      = "/p4/1/logs"
)

// MonitorPid models a process from monitor output
// ...existing code...
type MonitorPid struct {
	Pid     string
	User    string
	Cmd     string
	Args    string
	Elapsed string
}

// Blocker models a blocking pid
// ...existing code...
type Blocker struct {
	Pid            string
	User           string
	Cmd            string
	Elapsed        string
	Table          string
	BlockedPids    []string
	DirectBlocked  int
	IndirectBlocked int
}

// MonitorMetrics holds all metrics
// ...existing code...
type MonitorMetrics struct {
	DbReadLocks           int
	DbWriteLocks          int
	ClientEntityReadLocks int
	ClientEntityWriteLocks int
	MetaReadLocks         int
	MetaWriteLocks        int
	BlockedCommands       int
	Msgs                  []string
	BlockingCommands      map[string]*Blocker
	MonitorCommands       map[string]*MonitorPid
}

func main() {
	// Flags
	p4port := flag.String("p4port", "", "Perforce server port")
	p4user := flag.String("p4user", "", "Perforce user")
	logFile := flag.String("log", filepath.Join(logDirDefault, "monitor_metrics.log"), "Log file")
	sdpInstance := flag.String("sdp-instance", "", "SDP instance")
	testFile := flag.String("test-file", "", "Test file")
	metricsRoot := flag.String("metrics-root", metricsRootDefault, "Metrics directory")
	verbosity := flag.String("verbosity", "DEBUG", "Verbosity level")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			logger.SetOutput(f)
		}
	}

	if *testFile != "" {
		parseTestFile(*testFile, logger, *metricsRoot)
		return
	}

	// Run commands
	lockData, err := runLslocks(logger)
	if err != nil {
		logger.Printf("Failed to run lslocks: %v", err)
		return
	}
	monData, err := runMonitorShow(*p4port, *p4user, logger)
	if err != nil {
		logger.Printf("Failed to run monitor show: %v", err)
		return
	}
	metrics := findLocks(lockData, monData, logger)
	writeLog(formatLog(metrics), *logFile)
	writeMetrics(formatMetrics(metrics), *metricsRoot)
}

// runLslocks executes lslocks and returns output
func runLslocks(logger *log.Logger) (string, error) {
	cmd := exec.Command("sudo", "lslocks", "-o", "+BLOCKER", "-J")
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("lslocks failed, retrying without sudo: %v", err)
		cmd = exec.Command("lslocks", "-o", "+BLOCKER", "-J")
		out, err = cmd.CombinedOutput()
		if err != nil {
			return "", err
		}
	}
	return string(out), nil
}

// runMonitorShow executes p4 monitor show -al
func runMonitorShow(p4port, p4user string, logger *log.Logger) (string, error) {
	p4bin := os.Getenv("P4BIN")
	if p4bin == "" {
		p4bin = "p4"
	}
	if p4port == "" {
		p4port = os.Getenv("P4PORT")
	}
	if p4user == "" {
		p4user = os.Getenv("P4USER")
	}
	args := []string{"-u", p4user, "-p", p4port, "-F", "%id% %runstate% %user% %elapsed% %function% %args%", "monitor", "show", "-al"}
	cmd := exec.Command(p4bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Printf("p4 monitor show failed: %v", err)
		return "", err
	}
	return string(out), nil
}

// findLocks parses lock and monitor data
func findLocks(lockData, monData string, logger *log.Logger) *MonitorMetrics {
	metrics := &MonitorMetrics{
		BlockingCommands: make(map[string]*Blocker),
		MonitorCommands:  make(map[string]*MonitorPid),
	}
	metrics.MonitorCommands = parseMonitorData(monData)
	var jlock struct {
		Locks []struct {
			Command string `json:"command"`
			Pid     string `json:"pid"`
			Type    string `json:"type"`
			Size    string `json:"size"`
			Mode    string `json:"mode"`
			Path    string `json:"path"`
			Blocker string `json:"blocker"`
		}
	}
	if err := json.Unmarshal([]byte(lockData), &jlock); err != nil {
		logger.Printf("Failed to parse lock JSON: %v", err)
		return metrics
	}
	for _, j := range jlock.Locks {
		if !strings.Contains(j.Command, "p4d") || j.Path == "" {
			continue
		}
		pid := j.Pid
		if strings.Contains(j.Path, "clientEntity") {
			if j.Mode == "READ" {
				metrics.ClientEntityReadLocks++
			} else if j.Mode == "WRITE" {
				metrics.ClientEntityWriteLocks++
			}
		}
		if strings.Contains(j.Path, "server.locks/meta") {
			if j.Mode == "READ" {
				metrics.MetaReadLocks++
			} else if j.Mode == "WRITE" {
				metrics.MetaWriteLocks++
			}
		}
		dbPath := dbFileInPath(j.Path)
		if dbPath != "" {
			if j.Mode == "READ" {
				metrics.DbReadLocks++
			}
			if j.Mode == "WRITE" {
				metrics.DbWriteLocks++
			}
		}
		if j.Blocker != "" {
			bpid := j.Blocker
			buser, bcmd, bargs, belapsed := "unknown", "unknown", "unknown", "unknown"
			if mp, ok := metrics.MonitorCommands[bpid]; ok {
				buser = mp.User
				bcmd = mp.Cmd
				bargs = mp.Args
				belapsed = mp.Elapsed
			}
			msg := fmt.Sprintf("pid %s, user %s, cmd %s, table %s, blocked by pid %s, user %s, cmd %s, args %s", pid, buser, bcmd, dbPath, bpid, buser, bcmd, bargs)
			if _, ok := metrics.BlockingCommands[bpid]; !ok {
				metrics.BlockingCommands[bpid] = &Blocker{Pid: bpid, User: buser, Cmd: bcmd, Elapsed: belapsed, Table: dbPath}
			}
			if !contains(metrics.BlockingCommands[bpid].BlockedPids, pid) {
				metrics.BlockedCommands++
				metrics.BlockingCommands[bpid].BlockedPids = append(metrics.BlockingCommands[bpid].BlockedPids, pid)
				metrics.Msgs = append(metrics.Msgs, msg)
			}
		}
	}
	return metrics
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func dbFileInPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	p := parts[len(parts)-1]
	if strings.HasPrefix(p, "db.") || p == "rdb.lbr" || strings.HasPrefix(p, "storage") {
		return p
	}
	for _, s := range []string{"/clients/", "/clientEntity/", "/meta/"} {
		if strings.Contains(path, s) {
			return strings.ReplaceAll(s, "/", "") + "Lock"
		}
	}
	return ""
}

func parseMonitorData(monData string) map[string]*MonitorPid {
	re := regexp.MustCompile(`(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s*(.*)$`)
	pids := make(map[string]*MonitorPid)
	scanner := bufio.NewScanner(strings.NewReader(monData))
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindStringSubmatch(line)
		if m != nil {
			pid := m[1]
			user := m[3]
			elapsed := m[4]
			cmd := m[5]
			args := ""
			if len(m) == 7 {
				args = m[6]
			}
			pids[pid] = &MonitorPid{Pid: pid, User: user, Cmd: cmd, Args: args, Elapsed: elapsed}
		}
	}
	return pids
}

func formatLog(metrics *MonitorMetrics) []string {
	prefix := time.Now().Format("2006-01-02 15:04:05")
	lines := []string{}
	if len(metrics.Msgs) == 0 {
		lines = append(lines, fmt.Sprintf("%s no blocked commands", prefix))
	} else {
		for _, m := range metrics.Msgs {
			lines = append(lines, fmt.Sprintf("%s %s", prefix, m))
		}
	}
	return lines
}

func writeLog(lines []string, logFile string) {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, line := range lines {
		f.WriteString(line + "\n")
	}
}

func formatMetrics(metrics *MonitorMetrics) []string {
	lines := []string{}
	name := "p4_locks_db_read"
	lines = append(lines, fmt.Sprintf("# HELP %s Database read locks", name))
	lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	lines = append(lines, fmt.Sprintf("%s %d", name, metrics.DbReadLocks))
	name = "p4_locks_db_write"
	lines = append(lines, fmt.Sprintf("# HELP %s Database write locks", name))
	lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	lines = append(lines, fmt.Sprintf("%s %d", name, metrics.DbWriteLocks))
	name = "p4_locks_cliententity_read"
	lines = append(lines, fmt.Sprintf("# HELP %s clientEntity read locks", name))
	lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	lines = append(lines, fmt.Sprintf("%s %d", name, metrics.ClientEntityReadLocks))
	name = "p4_locks_cliententity_write"
	lines = append(lines, fmt.Sprintf("# HELP %s clientEntity write locks", name))
	lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	lines = append(lines, fmt.Sprintf("%s %d", name, metrics.ClientEntityWriteLocks))
	name = "p4_locks_meta_read"
	lines = append(lines, fmt.Sprintf("# HELP %s meta db read locks", name))
	lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	lines = append(lines, fmt.Sprintf("%s %d", name, metrics.MetaReadLocks))
	name = "p4_locks_meta_write"
	lines = append(lines, fmt.Sprintf("# HELP %s meta db write locks", name))
	lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	lines = append(lines, fmt.Sprintf("%s %d", name, metrics.MetaWriteLocks))
	name = "p4_locks_cmds_blocked"
	lines = append(lines, fmt.Sprintf("# HELP %s cmds blocked by locks", name))
	lines = append(lines, fmt.Sprintf("# TYPE %s gauge", name))
	lines = append(lines, fmt.Sprintf("%s %d", name, metrics.BlockedCommands))
	return lines
}

func writeMetrics(lines []string, metricsRoot string) {
	fname := filepath.Join(metricsRoot, metricsFile)
	tmpfname := fname + ".tmp"
	_ = ioutil.WriteFile(tmpfname, []byte(strings.Join(lines, "\n")+"\n"), 0644)
	_ = os.Rename(tmpfname, fname)
}

// parseTestFile: stub for test file parsing
func parseTestFile(testFile string, logger *log.Logger, metricsRoot string) {
	logger.Printf("Test file parsing not implemented in Go port yet.")
}
