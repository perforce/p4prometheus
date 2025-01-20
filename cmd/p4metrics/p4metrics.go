// This is a version of monitor_metrics.sh in Go as part of p4prometheus
// It is intended to be more reliable and cross platform than the original.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/bitfield/script"
	"github.com/perforce/p4prometheus/version"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

var logger logrus.Logger

func sourceSDPVars(sdpInstance string) map[string]string {
	// Source SDP vars and return a list
	cmd := exec.Command("bash", "-c", fmt.Sprintf("source /p4/common/bin/p4_vars %s && env", sdpInstance))

	// Get the current environment
	oldEnv := make(map[string]string)
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		oldEnv[pair[0]] = pair[1]
	}

	// Run the command and capture output
	results := make(map[string]string)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error running script: %v\n", err)
		return results
	}

	// Parse the new environment
	newEnv := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		pair := strings.SplitN(line, "=", 2)
		if len(pair) == 2 {
			newEnv[pair[0]] = pair[1]
		}
	}

	otherVars := []string{"LOGS"} // Other interesting env vars
	for k, v := range newEnv {
		if strings.HasPrefix(k, "P4") || strings.Contains(k, "SDP") {
			results[k] = v
		}
		for _, s := range otherVars {
			if k == s {
				results[k] = v
			}
		}
	}
	return results
}

func getVar(vars map[string]string, k string) string {
	if v, ok := vars[k]; ok {
		return v
	}
	return ""
}

// P4MonitorMetrics structure
type P4MonitorMetrics struct {
	// config *config.Config
	env               *map[string]string
	logger            *logrus.Logger
	serverID          string
	rootDir           string
	logsDir           string
	p4Cmd             string
	sdpInstance       string
	sdpInstanceLabel  string
	sdpInstanceSuffix string
	p4info            map[string]string
	logFile           string
	errorsFile        string
}

func newP4MonitorMetrics(envVars *map[string]string, logger *logrus.Logger) (p4m *P4MonitorMetrics) {
	return &P4MonitorMetrics{
		// config: config,
		env:    envVars,
		logger: logger,
		p4info: make(map[string]string),
	}
}

func (p4m *P4MonitorMetrics) initVars() {
	// Note that P4BIN is defined by SDP by sourcing above file, as are P4USER, P4PORT
	p4m.sdpInstance = getVar(*p4m.env, "SDP_INSTANCE")
	p4m.p4Cmd = fmt.Sprintf("%s -u %s -p \"%s\"", getVar(*p4m.env, "P4BIN"), getVar(*p4m.env, "P4USER"), getVar(*p4m.env, "P4PORT"))
	p4m.logger.Debugf("p4Cmd: %s", p4m.p4Cmd)
	i, err := script.Exec(fmt.Sprintf("%s %s", p4m.p4Cmd, "info -s")).Slice()
	if err != nil {
		p4m.logger.Fatalf("Can't connect to P4PORT: %s", getVar(*p4m.env, "P4PORT"))
	}
	for _, s := range i {
		parts := strings.Split(s, ": ")
		if len(parts) == 2 {
			p4m.p4info[parts[0]] = parts[1]
		}
	}
	p4m.logger.Debugf("p4info -s: %d %v\n%v", len(i), i, p4m.p4info)
	p4m.sdpInstanceLabel = fmt.Sprintf(",sdpinst=\"%s\"", p4m.sdpInstance)
	p4m.logger.Debugf("sdpInstanceLabel: %s", p4m.sdpInstanceLabel)
	p4m.sdpInstanceSuffix = fmt.Sprintf("-%s", p4m.sdpInstance)
	p4m.logger.Debugf("sdpInstanceSuffix: %s", p4m.sdpInstanceSuffix)
	p4m.logFile = getVar(*p4m.env, "P4LOG")
	p4m.logger.Debugf("logFile: %s", p4m.logFile)
	p4m.logsDir = getVar(*p4m.env, "LOGS")
	p4m.logger.Debugf("LOGS: %s", p4m.logsDir)
	p4m.errorsFile = path.Join(p4m.logsDir, "errors.csv")
	p4m.logger.Debugf("errorsFile: %s", p4m.errorsFile)
	// Get server id. Usually server.id files are a single line containing the
	// ServerID value. However, a server.id file will have a second line if a
	// 'p4 failover' was done containing an error message displayed to users
	// during the failover, and also preventing the service from starting
	// post-failover (to avoid split brain). For purposes of this check, we care
	// only about the ServerID value contained on the first line, so we use
	// 'head -1' on the server.id file.
	p4m.rootDir = getVar(*p4m.env, "P4ROOT")
	idFile := path.Join(p4m.rootDir, "server.id")
	if _, err := os.Stat(idFile); err == nil {
		s, err := script.File(idFile).Slice()
		if err == nil && len(s) > 0 {
			p4m.serverID = s[0]
			p4m.logger.Debugf("found server.id")
		} else {
			p4m.serverID = p4m.p4info["ServerID"]
		}
	}
	if p4m.serverID == "" {
		p4m.serverID = "UnsetServerID"
	}
	p4m.logger.Debugf("serverID: %s", p4m.serverID)
}

// $ p4 info -s
// User name: perforce
// Client name: 84e26b1e03ba
// Client host: 84e26b1e03ba
// Current directory: /home/perforce
// Peer address: 127.0.0.1:54110
// Client address: 127.0.0.1
// Server address: localhost:1999
// Server root: /p4/1/root
// Server date: 2025/01/20 17:14:47 +0000 UTC
// Server uptime: 73:17:53
// Server version: P4D/LINUX26AARCH64/2024.2/2697822 (2024/12/18)
// Server encryption: encrypted
// Server cert expires: Jan 15 15:56:45 2035 GMT
// ServerID: master.1
// Server services: standard
// Server license: none
// Case Handling: sensitive

// monitor_uptime () {
//     # Server uptime as a simple seconds parameter - parsed from p4 info:
//     # Server uptime: 168:39:20
//     fname="$metrics_root/p4_uptime${sdpinst_suffix}-${SERVER_ID}.prom"
//     tmpfname="$fname.$$"
//     uptime=$(grep uptime "$tmp_info_data" | awk '{print $3}')
//     [[ -z "$uptime" ]] && uptime="0:0:0"
//     uptime=${uptime//:/ }
//     arr=($uptime)
//     hours=${arr[0]}
//     mins=${arr[1]}
//     secs=${arr[2]}
//     #echo $hours $mins $secs
//     # Ensure base 10 arithmetic used to avoid overflow errors
//     uptime_secs=$(((10#$hours * 3600) + (10#$mins * 60) + 10#$secs))
//     rm -f "$tmpfname"
//     echo "# HELP p4_server_uptime P4D Server uptime (seconds)" >> "$tmpfname"
//     echo "# TYPE p4_server_uptime counter" >> "$tmpfname"
//     echo "p4_server_uptime{${serverid_label}${sdpinst_label}} $uptime_secs" >> "$tmpfname"
//     chmod 644 "$tmpfname"
//     mv "$tmpfname" "$fname"
// }

func main() {
	var (
		sdpInstance = kingpin.Flag(
			"sdpInstance",
			"SDP Instance, typically 1 or alphanumeric.",
		).Default("1").String()
		debug = kingpin.Flag(
			"debug",
			"Enable debugging.",
		).Bool()
	)

	kingpin.Version(version.Print("p4metrics"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := logrus.New()
	logger.Level = logrus.InfoLevel
	if *debug {
		logger.Level = logrus.DebugLevel
	}

	env := sourceSDPVars(*sdpInstance)
	p4m := newP4MonitorMetrics(&env, logger)
	p4m.initVars()
}
