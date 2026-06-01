// Test packa for p4metrics
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/perforce/p4prometheus/cmd/p4metrics/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// If you want to debug a particular test (and output debug info):
//
//	go test -run TestRemovedFromMonitorTable -args -debug
var testDebug = flag.Bool("debug", false, "Set for debug")

var (
	tlogger = &logrus.Logger{Out: os.Stderr,
		Formatter: &logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true},
		// Level:     logrus.DebugLevel}
		Level: logrus.InfoLevel}
)

func initLogger() {
	logrus.SetFormatter(&logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true})
	tlogger.SetReportCaller(true)
	if *testDebug {
		tlogger.Level = logrus.DebugLevel
	}
}

type metricValue struct {
	key        string
	name       string
	value      string
	labelName  string
	labelValue string
}
type metricValues []metricValue

func compareMetricValues(t *testing.T, expected metricValues, actual []metricStruct) {
	t.Helper()

	expMap := make(map[string]metricValue)
	for _, exp := range expected {
		k := exp.name
		exp.key = k
		if exp.labelName != "" {
			k = fmt.Sprintf("%s/%s/%s", k, exp.labelName, exp.labelValue)
			exp.key = k
		}
		expMap[k] = exp
	}
	actualMap := make(map[string]metricValue)
	for _, metric := range actual {
		k := metric.name
		if len(metric.labels) > 0 {
			if metric.labels[0].name != "" {
				k = fmt.Sprintf("%s/%s/%s", k, metric.labels[0].name, metric.labels[0].value)
			}
			actualMap[k] = metricValue{key: k, name: metric.name, value: metric.value,
				labelName: metric.labels[0].name, labelValue: metric.labels[0].value}
		} else {
			actualMap[k] = metricValue{key: k, name: metric.name, value: metric.value}
		}
	}
	if len(actual) != len(expected) {
		t.Errorf("metric count mismatch: got %d metrics, want %d",
			len(actual), len(expected))
	}
	for _, exp := range expMap {
		actualMetric, exists := actualMap[exp.key]
		if !exists {
			t.Errorf("missing metric %q", exp.key)
			continue
		}
		if actualMetric.value != exp.value {
			t.Errorf("metric %q: got value %q, want %q",
				exp.key, actualMetric.value, exp.value)
		}
	}
	for _, am := range actualMap {
		found := false
		for _, exp := range expMap {
			if am.key == exp.key {
				found = true
				if exp.value != am.value {
					t.Errorf("metric %q: got value %q, want %q",
						exp.key, am.value, exp.value)
				}
				break
			}
		}
		if !found {
			t.Errorf("unexpected metric %q", am.key)
		}
	}
}

func TestP4MetricsLicense(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)
	p4m.p4license = map[string]string{
		"userCount":            "893",
		"userLimit":            "1000",
		"licenseExpires":       "1677628800",
		"licenseTimeRemaining": "34431485",
		"supportExpires":       "1677628800"}
	p4m.parseLicense()
	assert.Equal(t, 5, len(p4m.metrics))
	tlogger.Debugf("Metrics: %q", p4m.metrics)

	p4m.metrics = make([]metricStruct, 0)
	p4m.p4license = map[string]string{
		"userCount":      "893",
		"userLimit":      "1000",
		"supportExpires": "1772323200"}
	p4m.p4info["Server license"] = "Perforce Software, Inc. 999 users (support ends 2025/02/28)"
	p4m.p4info["Server date"] = "2025/01/28 03:29:58 -0800 PST"
	p4m.parseLicense()
	expected := metricValues{
		{name: "p4_licensed_user_count", value: "893"},
		{name: "p4_licensed_user_limit", value: "1000"},
		{name: "p4_license_time_remaining", value: "34259402"},
		{name: "p4_license_support_expires", value: "1772323200"},
		{name: "p4_license_info", value: "1", labelName: "licenseInfo", labelValue: "Perforce Software, Inc. 999 users"},
	}
	assert.Equal(t, len(expected), len(p4m.metrics))
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)

	p4m.metrics = make([]metricStruct, 0)
	p4m.p4license = map[string]string{
		"isLicensed":      "no",
		"userCount":       "1",
		"userLimit":       "unlimited",
		"userSoftLimit":   "5",
		"clientCount":     "0",
		"clientLimit":     "unlimited",
		"clientSoftLimit": "20",
		"fileCount":       "0",
		"fileLimit":       "unlimited",
		"fileSoftLimit":   "1000",
		"repoCount":       "0",
		"repoLimit":       "3",
		"repoSoftLimit":   "3"}
	p4m.p4info["Server license"] = "none"
	p4m.p4info["Server date"] = "2025/01/28 03:29:58 -0800 PST"
	p4m.parseLicense()
	expected = metricValues{
		{name: "p4_licensed_user_count", value: "1"},
		// {name: "p4_licensed_user_limit", value: "unlimited"},
		{name: "p4_license_info", value: "1", labelName: "licenseInfo", labelValue: "none"},
	}
	assert.Equal(t, len(expected), len(p4m.metrics))
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)

	p4m.metrics = make([]metricStruct, 0)
	p4m.p4license = map[string]string{
		"isLicensed":     "yes",
		"userCount":      "84",
		"userLimit":      "472",
		"clientCount":    "-",
		"clientLimit":    "unlimited",
		"fileCount":      "-",
		"fileLimit":      "unlimited",
		"repoCount":      "-",
		"repoLimit":      "unlimited",
		"supportExpires": "1764892800", // --- THIS IS December 5, 2025 (GMT)
	}
	p4m.p4info["Server license"] = "none"
	p4m.p4info["Server date"] = "2025/04/28 03:29:58 -0800 PST"
	p4m.parseLicense()
	expected = metricValues{
		{name: "p4_licensed_user_count", value: "84"},
		{name: "p4_licensed_user_limit", value: "472"},
		{name: "p4_license_time_remaining", value: "19053002"},
		{name: "p4_license_support_expires", value: "1764892800"},
		{name: "p4_license_info", value: "1", labelName: "licenseInfo", labelValue: "none"},
	}
	assert.Equal(t, len(expected), len(p4m.metrics))
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)

}

func TestP4ConfigParsing(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)
	p4m.parseConfigShow([]string{
		"P4ROOT=/p4/1/root (-r)",
		"P4PORT=ssl:1999 (-p)",
		"P4JOURNAL=/p4/1/logs/journal (-J)",
		"P4LOG=/p4/1/logs/log (-L)",
		"P4TICKETS=/p4/1/.p4tickets",
		"P4TRUST=/p4/1/.p4trust",
		"security=4 (configure)",
		"monitor=2 (configure)",
		"journalPrefix=/p4/1/checkpoints/p4_1 (configure)",
		"filesys.P4ROOT.min=5G (configure)",
		"filesys.P4JOURNAL.min=5G (configure)",
		"filesys.depot.min=5G (configure)",
		"server.depot.root=/p4/1/depots (configure)",
		"server.extensions.dir=/p4/1/logs/p4-extensions (configure)",
		"serverlog.file.1=/p4/1/logs/auth.csv (configure)",
		"serverlog.file.3=/p4/1/logs/errors.csv (configure)",
		"serverlog.file.7=/p4/1/logs/events.csv (configure)",
		"serverlog.file.8=/p4/1/logs/integrity.csv (configure)",
		"serverlog.file.11=/p4/1/logs/triggers.csv (configure)",
		"serverlog.retain.1=21 (configure)",
		"serverlog.retain.3=21 (configure)",
		"serverlog.retain.7=21 (configure)",
		"serverlog.retain.8=21 (configure)",
		"serverlog.retain.11=21 (configure)"})
	assert.Equal(t, "/p4/1/logs/log", p4m.p4log)
	assert.Equal(t, "/p4/1/logs/journal", p4m.p4journal)
	assert.Equal(t, "/p4/1/logs/errors.csv", p4m.p4errorsCSV)
	assert.Equal(t, "/p4/1/checkpoints/p4_1", p4m.journalPrefix)
}

func TestP4MetricsFilesys(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)
	p4m.parseFilesys([]string{"filesys.P4ROOT.min=5G (configure)",
		"filesys.P4ROOT.min=250M (default)"})
	expected := metricValues{
		{name: "p4_filesys_min", value: "5368709120", labelName: "filesys", labelValue: "P4ROOT"},
	}
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)

	p4m = newP4MonitorMetrics(&cfg, &env, tlogger)
	p4m.parseFilesys([]string{"filesys.P4ROOT.min=250M (default)"})
	expected = metricValues{
		{name: "p4_filesys_min", value: "262144000", labelName: "filesys", labelValue: "P4ROOT"},
	}
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)

	p4m = newP4MonitorMetrics(&cfg, &env, tlogger)
	p4m.parseFilesys([]string{
		"filesys.P4ROOT.min=200M (configure)",
		"filesys.depot.min=10G (configure)",
		"filesys.P4JOURNAL.min=1G (configure)",
		"filesys.P4LOG.min=2G (configure)",
		"filesys.TEMP.min=500M (configure)",
	})
	expected = metricValues{
		{name: "p4_filesys_min", value: "209715200", labelName: "filesys", labelValue: "P4ROOT"},
		{name: "p4_filesys_min", value: "10737418240", labelName: "filesys", labelValue: "depot"},
		{name: "p4_filesys_min", value: "1073741824", labelName: "filesys", labelValue: "P4JOURNAL"},
		{name: "p4_filesys_min", value: "2147483648", labelName: "filesys", labelValue: "P4LOG"},
		{name: "p4_filesys_min", value: "524288000", labelName: "filesys", labelValue: "TEMP"},
	}
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)
	expString := `# HELP p4_filesys_min Minimum space for filesystem
# TYPE p4_filesys_min gauge
p4_filesys_min{filesys="depot"} 10737418240
p4_filesys_min{filesys="P4ROOT"} 209715200
p4_filesys_min{filesys="P4JOURNAL"} 1073741824
p4_filesys_min{filesys="P4LOG"} 2147483648
p4_filesys_min{filesys="TEMP"} 524288000
`
	buf := p4m.getCumulativeMetrics()
	tlogger.Debugf("Metrics: %q", buf)
	assert.Equal(t, expString, buf)
}

func TestP4MetricsSchemaParsing(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)

	schemaLines := `
... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 0
... f_name f_eventtype

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 1
... f_name f_timestamp

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 2
... f_name f_timestamp2

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 3
... f_name f_date

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 4
... f_name f_pid

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 5
... f_name f_cmdident

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 6
... f_name f_serverid

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 7
... f_name f_cmdno

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 8
... f_name f_user

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 9
... f_name f_client

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 10
... f_name f_func

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 11
... f_name f_host

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 12
... f_name f_prog

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 13
... f_name f_version

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 14
... f_name f_args

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 15
... f_name f_cmdgroup

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 16
... f_name f_severity

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 17
... f_name f_subsys

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 18
... f_name f_subcode

... f_recordType 4
... f_recordVersion 58
... f_recordName Error
... f_field 19
... f_name f_text
`
	lines := strings.Split(schemaLines, "\n")
	p4m.setupErrorParsing(lines)
	assert.Equal(t, 16, p4m.indErrSeverity)
	assert.Equal(t, 17, p4m.indErrSubsys)
}

func TestP4MonitorParsing(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)

	monitorLines := `662053 I swarm      00:03:41 IDLE none
21104 R fred 01:03:41 sync
16578 I svc_p4d_edge_CL1 00:04:26 IDLE none
16647 I svc_p4d_fs_brk 00:04:26 IDLE none
21104 I svc_p4d_fs_brk 00:04:40 IDLE none
 5505 I svc_p4d_edge_CL1 00:04:49 IDLE none
170076 I svc_p4d_ha_chi 01:15:07 IDLE none
 2303 B svc_master-1666 270:49:23 ldapsync -g -i 1800
 2304 B svc_master-1666 270:49:23 admin resource-monitor
`
	lines := strings.Split(monitorLines, "\n")
	assert.Equal(t, 3821, p4m.getMaxNonSvcCmdTime(lines))
}

func TestP4PullParsing(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)

	pullLines := `... replicaTransfersActive 0
... replicaTransfersTotal 169
... replicaBytesActive 0
... replicaBytesTotal 460828016
... replicaOldestChange 0`

	lines := strings.Split(pullLines, "\n")
	transfersTotal, bytesTotal := p4m.getPullTransfersAndBytes(lines)
	assert.Equal(t, int64(169), transfersTotal)
	assert.Equal(t, int64(460828016), bytesTotal)

	pullLines = `... replicaTransfersActive 0
... replicaTransfersTotal 0
... replicaBytesActive 0
... replicaBytesTotal 0
... replicaOldestChange 0`

	lines = strings.Split(pullLines, "\n")
	transfersTotal, bytesTotal = p4m.getPullTransfersAndBytes(lines)
	assert.Equal(t, int64(0), transfersTotal)
	assert.Equal(t, int64(0), bytesTotal)
}

func TestVerifyParsing(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)

	verifyLines := `
Summary of Errors by Type:
   Submitted File Errors:          4
   Spec Depot Errors:              3
   Unload Depot Errors:            2
   Total Non-Shelve Errors:        9 (sum of error types listed above)

   Shelved Changes with Errors:    1

A total of 1688 'p4 verify' commands were executed.
`

	lines := strings.Split(verifyLines, "\n")
	p4m.parseVerifyLog(lines)
	assert.Equal(t, int64(4), p4m.verifyErrsSubmitted)
	assert.Equal(t, int64(3), p4m.verifyErrsSpec)
	assert.Equal(t, int64(2), p4m.verifyErrsUnload)
	assert.Equal(t, int64(1), p4m.verifyErrsShelved)

	verifyLines = `
Status: OK: All scanned depots verified OK.
	
	A total of 1688 'p4 verify' commands were executed.
	`

	lines = strings.Split(verifyLines, "\n")
	p4m.parseVerifyLog(lines)
	assert.Equal(t, int64(0), p4m.verifyErrsSubmitted)
	assert.Equal(t, int64(0), p4m.verifyErrsSpec)
	assert.Equal(t, int64(0), p4m.verifyErrsUnload)
	assert.Equal(t, int64(0), p4m.verifyErrsShelved)

	verifyLines = `
Status: OK: All scanned depots verified OK.

Time: Completed verifications at Tue Apr  8 11:56:23 UTC 2025, taking 1 hours 2 minutes 3 seconds.
`
	lines = strings.Split(verifyLines, "\n")
	p4m.parseVerifyLog(lines)
	assert.Equal(t, int64(0), p4m.verifyErrsSubmitted)
	assert.Equal(t, int64(0), p4m.verifyErrsSpec)
	assert.Equal(t, int64(0), p4m.verifyErrsUnload)
	assert.Equal(t, int64(0), p4m.verifyErrsShelved)
	assert.Equal(t, 1*3600+2*60+3, p4m.verifyDuration)

}

func TestDiskspaceParsing(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)

	dsLines := `P4ROOT (type ext4 mounted on /hxmetadata) : 100.3G free, 273.2G used, 35G total (73% full)
P4JOURNAL (type ext4 mounted on /hxlogs) : 48.6G free, 26G used, 78.6G total (34% full)
P4LOG (type ext4 mounted on /hxlogs) : 48.6G free, 26G used, 78.6G total (34% full)
TEMP (type ext4 mounted on /hxlogs) : 48.6G free, 26G used, 78.6G total (34% full)
journalPrefix (type xfs mounted on /hxdepots) : 795.5G free, 7T used, 7.8T total (90% full)
serverlog.file.1 (type ext4 mounted on /hxlogs) : 2.3M free, 56K used, 1G total (34% full)
`

	lines := strings.Split(dsLines, "\n")
	vols, err := p4m.parseDiskspace(lines)
	assert.NoError(t, err)

	var keys []string
	for k := range vols {
		keys = append(keys, k)
	}
	expected := []string{"P4ROOT", "P4JOURNAL", "P4LOG", "TEMP", "journalPrefix", "serverlog.file.1"}
	sort.Strings(keys)
	sort.Strings(expected)
	if !reflect.DeepEqual(keys, expected) {
		t.Errorf("Expected keys %v, but got %v", expected, keys)
	}

	assert.Equal(t, int64(107696304947), vols["P4ROOT"].Free)
	assert.Equal(t, int64(293346266316), vols["P4ROOT"].Used)
	assert.Equal(t, int64(37580963840), vols["P4ROOT"].Total)
	assert.Equal(t, int(73), vols["P4ROOT"].PercentFull)

	assert.Equal(t, int64(2411724), vols["serverlog.file.1"].Free)
	assert.Equal(t, int64(57344), vols["serverlog.file.1"].Used)
	assert.Equal(t, int64(1073741824), vols["serverlog.file.1"].Total)
	assert.Equal(t, int(34), vols["serverlog.file.1"].PercentFull)
}

type SwarmTest struct {
	statusCode      int
	taskResponse    *SwarmTaskResponse
	versionResponse *SwarmVersionResponse
}

func TestSwarmProcessing(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)

	testCases := []SwarmTest{
		{
			statusCode:   http.StatusOK,
			taskResponse: &SwarmTaskResponse{Tasks: 1, FutureTasks: 2, Workers: 3, MaxWorkers: 4},
		},
		{
			statusCode:      http.StatusOK,
			versionResponse: &SwarmVersionResponse{Version: `SWARM\/2024.6\/2701191 (2025\/01\/07)`},
		},
	}

	ind := 0
	{
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return the result as requested
			tc := testCases[ind]
			ind += 1
			w.WriteHeader(tc.statusCode)
			// If it's a successful case, return the mock response
			if tc.statusCode == http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				// Assume one or the other!
				if tc.taskResponse != nil {
					json.NewEncoder(w).Encode(tc.taskResponse)
				} else {
					json.NewEncoder(w).Encode(tc.versionResponse)
				}
			}
		}))
		defer server.Close()

		p4m.getSwarmMetrics(server.URL, "user", "ticket") // Dummy call
	}
	expected := metricValues{
		{name: "p4_swarm_future_tasks", value: "2"},
		{name: "p4_swarm_workers", value: "3"},
		{name: "p4_swarm_max_workers", value: "4"},
		{name: "p4_swarm_error", value: "0"},
		{name: "p4_swarm_authorized", value: "1"},
		{name: "p4_swarm_tasks", value: "1"},
		{name: "p4_swarm_version", value: "1", labelName: "version", labelValue: "SWARM/2024.6/2701191 (2025/01/07)"},
	}
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)
}

func TestSwarmProcessingError(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)
	testCases := []SwarmTest{
		{
			statusCode:   http.StatusGatewayTimeout,
			taskResponse: &SwarmTaskResponse{Tasks: 1, FutureTasks: 2, Workers: 3, MaxWorkers: 4},
		},
		{
			statusCode:      http.StatusGatewayTimeout,
			versionResponse: &SwarmVersionResponse{Version: `SWARM\/2024.6\/2701191 (2025\/01\/07)`},
		},
	}

	ind := 0
	{
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return the result as requested
			tc := testCases[ind]
			ind += 1
			w.WriteHeader(tc.statusCode)
			// If it's a successful case, return the mock response
			if tc.statusCode == http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				// Assume one or the other!
				if tc.taskResponse != nil {
					json.NewEncoder(w).Encode(tc.taskResponse)
				} else {
					json.NewEncoder(w).Encode(tc.versionResponse)
				}
			}
		}))
		defer server.Close()

		p4m.getSwarmMetrics(server.URL, "user", "ticket") // Dummy call
	}
	expected := metricValues{
		{name: "p4_swarm_error", value: "1"},
		{name: "p4_swarm_authorized", value: "0"},
		{name: "p4_swarm_future_tasks", value: "0"},
		{name: "p4_swarm_workers", value: "0"},
		{name: "p4_swarm_max_workers", value: "0"},
		{name: "p4_swarm_tasks", value: "0"},
	}
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)
}

func TestSwarmProcessingUnauth(t *testing.T) {
	cfg := config.Config{}
	initLogger()
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, tlogger)
	testCases := []SwarmTest{
		{
			statusCode:   http.StatusUnauthorized,
			taskResponse: &SwarmTaskResponse{},
		},
		{
			statusCode:      http.StatusOK,
			versionResponse: &SwarmVersionResponse{Version: `SWARM\/2024.6\/2701191 (2025\/01\/07)`},
		},
	}

	ind := 0
	{
		// Create a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Return the result as requested
			tc := testCases[ind]
			ind += 1
			w.WriteHeader(tc.statusCode)
			// If it's a successful case, return the mock response
			if tc.statusCode == http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				// Assume one or the other!
				if tc.taskResponse != nil {
					json.NewEncoder(w).Encode(tc.taskResponse)
				} else {
					json.NewEncoder(w).Encode(tc.versionResponse)
				}
			}
		}))
		defer server.Close()

		p4m.getSwarmMetrics(server.URL, "user", "ticket") // Dummy call
	}
	expected := metricValues{
		{name: "p4_swarm_error", value: "0"},
		{name: "p4_swarm_authorized", value: "0"},
		{name: "p4_swarm_future_tasks", value: "0"},
		{name: "p4_swarm_workers", value: "0"},
		{name: "p4_swarm_max_workers", value: "0"},
		{name: "p4_swarm_tasks", value: "0"},
		{name: "p4_swarm_version", value: "1", labelName: "version", labelValue: "SWARM/2024.6/2701191 (2025/01/07)"},
	}
	tlogger.Debugf("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)
}

func TestParseMonitorShow(t *testing.T) {
	initLogger()

	testCases := []struct {
		name               string
		monitorOutput      []string
		monitorGroups      []config.MonitorGroup
		monitorIgnore      string
		expectedCmds       map[string]int
		expectedUsers      map[string]int
		expectedStates     map[string]int
		expectedGroups     map[string]int
		expectedRuntime    map[string]int
		expectedMaxRuntime map[string]int
		expectedMaxTime    int
	}{
		{
			name: "basic parsing",
			monitorOutput: []string{
				"12345 R alice 00:00:05 sync",
				"12346 R bob 00:00:10 edit",
				"12347 I charlie 00:00:15 IDLE none",
			},
			expectedCmds: map[string]int{
				"sync": 1,
				"edit": 1,
				"IDLE": 1,
			},
			expectedUsers: map[string]int{
				"alice":   1,
				"bob":     1,
				"charlie": 1,
			},
			expectedStates: map[string]int{
				"R": 2,
				"I": 1,
			},
			expectedGroups:     map[string]int{},
			expectedRuntime:    map[string]int{},
			expectedMaxRuntime: map[string]int{},
			expectedMaxTime:    15,
		},
		{
			name: "with monitor groups",
			monitorOutput: []string{
				"12345 R alice 00:00:05 sync",
				"12346 R bob 00:00:10 transmit",
				"12347 I charlie 00:00:15 IDLE none",
				"12348 R dave 00:00:20 unshelve",
			},
			monitorGroups: []config.MonitorGroup{
				{Commands: "sync|transmit", Label: "sync_transmit"},
				{Commands: "shelve|unshelve", Label: "shelf_ops"},
			},
			expectedCmds: map[string]int{
				"sync":     1,
				"transmit": 1,
				"IDLE":     1,
				"unshelve": 1,
			},
			expectedUsers: map[string]int{
				"alice":   1,
				"bob":     1,
				"charlie": 1,
				"dave":    1,
			},
			expectedStates: map[string]int{
				"R": 3,
				"I": 1,
			},
			expectedGroups: map[string]int{
				"sync_transmit": 2, // sync R + transmit R
				"shelf_ops":     1, // only unshelve R (I not counted)
			},
			expectedRuntime: map[string]int{
				"sync_transmit": 15, // 5 + 10
				"shelf_ops":     20, // only unshelve (20), I not counted
			},
			expectedMaxRuntime: map[string]int{
				"sync_transmit": 10, // max(5, 10)
				"shelf_ops":     20, // only unshelve
			},
			expectedMaxTime: 20,
		},
		{
			name: "with monitor ignore",
			monitorOutput: []string{
				"12345 R alice 00:00:05 sync",
				"12346 R svc 00:10:00 ldapsync",
				"12347 I charlie 00:00:15 IDLE none",
			},
			monitorIgnore: "ldapsync",
			monitorGroups: []config.MonitorGroup{
				{Commands: ".*", Label: "other"},
			},
			expectedCmds: map[string]int{
				"sync":     1,
				"ldapsync": 1,
				"IDLE":     1,
			},
			expectedUsers: map[string]int{
				"alice":   1,
				"svc":     1,
				"charlie": 1,
			},
			expectedStates: map[string]int{
				"R": 2,
				"I": 1,
			},
			expectedGroups: map[string]int{
				"other": 1, // only sync (R), IDLE is I, ldapsync is ignored
			},
			expectedRuntime: map[string]int{
				"other": 5, // only sync (5), IDLE is I, ldapsync is ignored
			},
			expectedMaxRuntime: map[string]int{
				"other": 5, // only sync, shelve is I, ldapsync is ignored
			},
			expectedMaxTime: 600,
		},
		{
			name:               "empty output",
			monitorOutput:      []string{},
			expectedCmds:       map[string]int{},
			expectedUsers:      map[string]int{},
			expectedStates:     map[string]int{},
			expectedGroups:     map[string]int{},
			expectedRuntime:    map[string]int{},
			expectedMaxRuntime: map[string]int{},
			expectedMaxTime:    0,
		},
		{
			name: "malformed lines ignored",
			monitorOutput: []string{
				"12345 R alice 00:00:05 sync",
				"incomplete line",
				"12346 R bob",
				"12347 R charlie 00:00:10 edit",
			},
			expectedCmds: map[string]int{
				"sync": 1,
				"edit": 1,
			},
			expectedUsers: map[string]int{
				"alice":   1,
				"charlie": 1,
			},
			expectedStates: map[string]int{
				"R": 2,
			},
			expectedGroups:     map[string]int{},
			expectedRuntime:    map[string]int{},
			expectedMaxRuntime: map[string]int{},
			expectedMaxTime:    10,
		},
		{
			name: "state filtering - only R processes grouped",
			monitorOutput: []string{
				"12345 R alice 00:00:05 sync",
				"12346 B bob 00:00:10 sync",          // Background - not counted
				"12347 I charlie 00:00:15 IDLE none", // Idle - not counted
				"12348 R dave 00:00:20 edit",
				"12349 B eve 00:00:25 edit", // Background - not counted
			},
			monitorGroups: []config.MonitorGroup{
				{Commands: "sync", Label: "sync_group"},
				{Commands: "edit", Label: "edit_group"},
			},
			expectedCmds: map[string]int{
				"sync": 2,
				"edit": 2,
				"IDLE": 1,
			},
			expectedUsers: map[string]int{
				"alice":   1,
				"bob":     1,
				"charlie": 1,
				"dave":    1,
				"eve":     1,
			},
			expectedStates: map[string]int{
				"R": 2,
				"B": 2,
				"I": 1,
			},
			expectedGroups: map[string]int{
				"sync_group": 1, // only alice's sync (R)
				"edit_group": 1, // only dave's edit (R)
			},
			expectedRuntime: map[string]int{
				"sync_group": 5,  // only alice's sync
				"edit_group": 20, // only dave's edit
			},
			expectedMaxRuntime: map[string]int{
				"sync_group": 5,  // only alice's sync
				"edit_group": 20, // only dave's edit
			},
			expectedMaxTime: 25, // eve's blocked edit is max
		},
		{
			name: "larger output with various states and groups",
			monitorOutput: []string{
				"208188 R p4sdp      00:00:00 monitor show -al",
				"208182 R ecagent    00:00:01 client -d cmdr-sources-186564700-99226",
				"1152895 R svc_p4d_ha_chi 00:00:01 rmt-Journal",
				"208137 R qtdev      00:00:03 transmit -t208015 -b8 -s524288",
				"208015 R qtdev      00:00:04 sync -f -q //...",
				"208026 R qtdev      00:00:04 transmit -t208015 -b8 -s524288",
				"208029 R qtdev      00:00:04 transmit -t208015 -b8 -s524288",
				"208030 R qtdev      00:00:04 transmit -t208015 -b8 -s524288",
				"4055366 R fred      100:47:20 dm-SubmitChange",
			},
			monitorGroups: []config.MonitorGroup{
				{Commands: "^rmt.*", Label: "rmt_group"},
				{Commands: "sync|transmit", Label: "sync_group"},
				{Commands: ".*", Label: "other_group"},
			},
			expectedCmds: map[string]int{
				"client":          1,
				"dm-SubmitChange": 1,
				"monitor":         1,
				"rmt-Journal":     1,
				"sync":            1,
				"transmit":        4,
			},
			expectedUsers: map[string]int{
				"ecagent":        1,
				"fred":           1,
				"p4sdp":          1,
				"qtdev":          5,
				"svc_p4d_ha_chi": 1,
			},
			expectedStates: map[string]int{
				"R": 9,
			},
			expectedGroups: map[string]int{
				"other_group": 3,
				"rmt_group":   1,
				"sync_group":  5,
			},
			expectedRuntime: map[string]int{
				"other_group": 362841, // monitor (0) + client (1) + dm-SubmitChange (100 * 3600 + 47 * 60 + 20 = 362840)
				"rmt_group":   1,
				"sync_group":  19,
			},
			expectedMaxRuntime: map[string]int{
				"other_group": 362840,
				"rmt_group":   1,
				"sync_group":  4,
			},
			expectedMaxTime: 362840, // dm-SubmitChange is max
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				MonitorGroups: tc.monitorGroups,
			}

			// Compile monitor ignore regex if specified
			if tc.monitorIgnore != "" {
				cfg.MonitorIgnore = tc.monitorIgnore
				re, err := regexp.Compile(tc.monitorIgnore)
				assert.NoError(t, err)
				cfg.MonitorIgnoreRe = re
			}

			// Compile monitor group regexes
			for i := range cfg.MonitorGroups {
				re, err := regexp.Compile(cfg.MonitorGroups[i].Commands)
				assert.NoError(t, err)
				cfg.MonitorGroups[i].ReCommands = re
			}

			env := map[string]string{}
			p4m := newP4MonitorMetrics(cfg, &env, tlogger)

			result := p4m.parseMonitorShow(tc.monitorOutput)

			// Verify command counts
			assert.Equal(t, tc.expectedCmds, result.cmdCounts, "cmdCounts mismatch")

			// Verify user counts
			assert.Equal(t, tc.expectedUsers, result.userCounts, "userCounts mismatch")

			// Verify state counts
			assert.Equal(t, tc.expectedStates, result.stateCounts, "stateCounts mismatch")

			// Verify group counts
			assert.Equal(t, tc.expectedGroups, result.groupCounts, "groupCounts mismatch")

			// Verify group runtime
			assert.Equal(t, tc.expectedRuntime, result.groupRuntime, "groupRuntime mismatch")

			// Verify group max runtime
			assert.Equal(t, tc.expectedMaxRuntime, result.groupMaxRuntime, "groupMaxRuntime mismatch")

			// Verify max time
			assert.Equal(t, tc.expectedMaxTime, result.maxNonSvcTime, "maxNonSvcTime mismatch")
		})
	}
}

// FakeMemReader is a mock implementation of MemReader for testing
type FakeMemReader struct {
	RSSByPid    map[int]int64
	TotalMemory int64
}

func (f *FakeMemReader) GetPIDRSSBytes(pid int) (int64, error) {
	if rss, ok := f.RSSByPid[pid]; ok {
		return rss, nil
	}
	return 0, fmt.Errorf("PID %d not in fake reader", pid)
}

func (f *FakeMemReader) GetMemTotalBytes() (int64, error) {
	if f.TotalMemory > 0 {
		return f.TotalMemory, nil
	}
	return 0, fmt.Errorf("total memory not set in fake reader")
}

// TestEvaluateMemLimits tests the evaluateMemLimits function with various scenarios
func TestEvaluateMemLimits(t *testing.T) {
	initLogger()

	type testCase struct {
		name         string
		processes    []MonitorProcess
		memLimits    *config.MemLimits
		memReader    MemReader
		expectKills  int
		expectCmds   map[string]bool
		expectUsers  map[string]bool
	}

	tests := []testCase{
		{
			name:      "no_memlimits_configured",
			processes: []MonitorProcess{},
			memLimits: nil,
			memReader: &FakeMemReader{TotalMemory: 1000 * 1024 * 1024},
			expectKills: 0,
		},
		{
			name:      "no_running_processes",
			processes: []MonitorProcess{},
			memLimits: &config.MemLimits{
				Enabled: true,
				Groups: []config.MemLimitGroup{
					{
						Description: "test_group",
						Users:       ".*",
						ReUsers:     regexp.MustCompile(".*"),
						CmdMaxPercentageInt: 10,
					},
				},
			},
			memReader: &FakeMemReader{TotalMemory: 1000 * 1024 * 1024},
			expectKills: 0,
		},
		{
			name: "single_process_under_limit",
			processes: []MonitorProcess{
				{Pid: 100, State: "R", User: "alice", Cmd: "sync"},
			},
			memLimits: &config.MemLimits{
				Enabled: true,
				ReCandidateCmds: regexp.MustCompile(".*"),
				Groups: []config.MemLimitGroup{
					{
						Description: "test_group",
						Users:       ".*",
						ReUsers:     regexp.MustCompile(".*"),
						CmdMaxPercentageInt: 10,
					},
				},
			},
			memReader: &FakeMemReader{
				RSSByPid:    map[int]int64{100: 50 * 1024 * 1024},
				TotalMemory: 1000 * 1024 * 1024, // 5% usage - under 10% limit
			},
			expectKills: 0,
		},
		{
			name: "single_process_exceeds_cmd_percentage_limit",
			processes: []MonitorProcess{
				{Pid: 100, State: "R", User: "alice", Cmd: "sync"},
			},
			memLimits: &config.MemLimits{
				Enabled: true,
				ReCandidateCmds: regexp.MustCompile(".*"),
				Groups: []config.MemLimitGroup{
					{
						Description: "test_group",
						Users:       ".*",
						ReUsers:     regexp.MustCompile(".*"),
						CmdMaxPercentageInt: 10,
					},
				},
			},
			memReader: &FakeMemReader{
				RSSByPid:    map[int]int64{100: 150 * 1024 * 1024},
				TotalMemory: 1000 * 1024 * 1024, // 15% usage - exceeds 10% limit
			},
			expectKills: 1,
			expectCmds:  map[string]bool{"sync": true},
		},
		{
			name: "single_process_exceeds_cmd_value_limit",
			processes: []MonitorProcess{
				{Pid: 100, State: "R", User: "alice", Cmd: "dbdump"},
			},
			memLimits: &config.MemLimits{
				Enabled: true,
				ReCandidateCmds: regexp.MustCompile(".*"),
				Groups: []config.MemLimitGroup{
					{
						Description: "test_group",
						Users:       ".*",
						ReUsers:     regexp.MustCompile(".*"),
						CmdMaxValueInt: 100 * 1024 * 1024, // 100 MB limit
					},
				},
			},
			memReader: &FakeMemReader{
				RSSByPid:    map[int]int64{100: 150 * 1024 * 1024}, // 150 MB usage
				TotalMemory: 1000 * 1024 * 1024,
			},
			expectKills: 1,
			expectCmds:  map[string]bool{"dbdump": true},
		},
		{
			name: "multiple_processes_user_cumulative_limit",
			processes: []MonitorProcess{
				{Pid: 100, State: "R", User: "alice", Cmd: "sync"},
				{Pid: 101, State: "R", User: "alice", Cmd: "edit"},
				{Pid: 102, State: "R", User: "bob", Cmd: "sync"},
			},
			memLimits: &config.MemLimits{
				Enabled: true,
				ReCandidateCmds: regexp.MustCompile(".*"),
				Groups: []config.MemLimitGroup{
					{
						Description: "test_group",
						Users:       "alice",
						ReUsers:     regexp.MustCompile("alice"),
						UserCumulativeMaxPercentageInt: 15, // 15% limit for alice
					},
				},
			},
			memReader: &FakeMemReader{
				RSSByPid: map[int]int64{
					100: 80 * 1024 * 1024,  // alice: 80+90=170 MB (17%)
					101: 90 * 1024 * 1024,
					102: 50 * 1024 * 1024,  // bob: 50 MB (5%)
				},
				TotalMemory: 1000 * 1024 * 1024,
			},
			expectKills: 2, // Both alice's processes should be killed
			expectUsers: map[string]bool{"alice": true},
		},
		{
			name: "process_not_running",
			processes: []MonitorProcess{
				{Pid: 100, State: "S", User: "alice", Cmd: "sync"}, // Sleeping, not running
			},
			memLimits: &config.MemLimits{
				Enabled: true,
				ReCandidateCmds: regexp.MustCompile(".*"),
				Groups: []config.MemLimitGroup{
					{
						Description: "test_group",
						Users:       ".*",
						ReUsers:     regexp.MustCompile(".*"),
						CmdMaxPercentageInt: 1, // Very low limit
					},
				},
			},
			memReader: &FakeMemReader{
				RSSByPid:    map[int]int64{100: 500 * 1024 * 1024},
				TotalMemory: 1000 * 1024 * 1024,
			},
			expectKills: 0, // Should not kill sleeping processes
		},
		{
			name: "candidate_cmd_filter",
			processes: []MonitorProcess{
				{Pid: 100, State: "R", User: "alice", Cmd: "sync"},
				{Pid: 101, State: "R", User: "alice", Cmd: "edit"},
			},
			memLimits: &config.MemLimits{
				Enabled: true,
				CandidateCmds: "sync",          // Only sync
				ReCandidateCmds: regexp.MustCompile("^sync$"),
				Groups: []config.MemLimitGroup{
					{
						Description: "test_group",
						Users:       ".*",
						ReUsers:     regexp.MustCompile(".*"),
						CmdMaxPercentageInt: 5, // 5% limit
					},
				},
			},
			memReader: &FakeMemReader{
				RSSByPid: map[int]int64{
					100: 150 * 1024 * 1024, // sync: 15% - exceeds limit
					101: 150 * 1024 * 1024, // edit: 15% - but not a candidate
				},
				TotalMemory: 1000 * 1024 * 1024,
			},
			expectKills: 1, // Only sync should be killed
			expectCmds:  map[string]bool{"sync": true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eval, err := evaluateMemLimits(tc.processes, tc.memLimits, tc.memReader, tlogger)
			assert.NoError(t, err)
			assert.NotNil(t, eval)

			assert.Equal(t, tc.expectKills, len(eval.KillCandidates), "kill count mismatch")

			if tc.expectCmds != nil {
				for _, kill := range eval.KillCandidates {
					assert.True(t, tc.expectCmds[kill.Cmd], "unexpected cmd killed: %s", kill.Cmd)
				}
			}

			if tc.expectUsers != nil {
				for _, kill := range eval.KillCandidates {
					assert.True(t, tc.expectUsers[kill.User], "unexpected user killed: %s", kill.User)
				}
			}
		})
	}
}

// TestLinuxProcMemReader tests the real Linux /proc-based reader (if running on Linux)
func TestLinuxProcMemReader(t *testing.T) {
	initLogger()

	// Skip on non-Linux systems
	if runtime.GOOS != "linux" {
		t.Skipf("Test only runs on Linux (GOOS=%s)", runtime.GOOS)
		return
	}

	// Test with the current process
	reader := &LinuxProcMemReader{}

	// Get our own RSS
	rss, err := reader.GetPIDRSSBytes(os.Getpid())
	assert.NoError(t, err, "Should be able to read RSS")
	assert.Greater(t, rss, int64(0), "RSS should be > 0")
	assert.Less(t, rss, int64(10*1024*1024*1024), "RSS should be < 10GB for test process")

	// Get total memory
	total, err := reader.GetMemTotalBytes()
	assert.NoError(t, err, "Should be able to read total memory")
	assert.Greater(t, total, int64(0), "Total memory should be > 0")
	assert.Less(t, rss, total, "Process RSS should be less than total memory")
}

// TestMonitorProcessesMemLimitsIntegration tests that monitorProcesses emits memory metrics when MemLimits configured
func TestMonitorProcessesMemLimitsIntegration(t *testing.T) {
	initLogger()

	// Create config with MemLimits enabled
	cfg := &config.Config{
		MemLimits: &config.MemLimits{
			Enabled: true,
			CandidateCmds: "sync|edit",
			ReCandidateCmds: regexp.MustCompile("sync|edit"),
			Groups: []config.MemLimitGroup{
				{
					Description: "standard_group",
					Users:       ".*",
					ReUsers:     regexp.MustCompile(".*"),
					CmdMaxPercentageInt: 10,
				},
			},
		},
	}

	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)

	// Mock memory reader
	p4m.memReader = &FakeMemReader{
		RSSByPid: map[int]int64{
			1000: 50 * 1024 * 1024,  // sync: 5%
			1001: 80 * 1024 * 1024,  // edit: 8%
		},
		TotalMemory: 1000 * 1024 * 1024,
	}

	// Simulate monitor show output with parsed processes
	monitorOutput := []string{
		"1000 R alice 00:00:05 sync",
		"1001 R bob 00:00:10 edit",
	}

	// Parse monitor output
	result := p4m.parseMonitorShow(monitorOutput)

	// Call evaluateMemLimits
	eval, err := evaluateMemLimits(result.processes, p4m.config.MemLimits, p4m.memReader, p4m.logger)
	assert.NoError(t, err)
	assert.NotNil(t, eval)

	// Verify memory percentages are calculated
	assert.Equal(t, 2, len(eval.MemoryPctByCmd), "should have 2 commands")
	assert.Equal(t, 2, len(eval.MemoryPctByUser), "should have 2 users")

	// Check that metrics are within expected range
	syncPct := eval.MemoryPctByCmd["sync"]
	editPct := eval.MemoryPctByCmd["edit"]
	assert.Greater(t, syncPct, 4.0)
	assert.Less(t, syncPct, 6.0)
	assert.Greater(t, editPct, 7.0)
	assert.Less(t, editPct, 9.0)

	// Verify per-user metrics
	alicePct := eval.MemoryPctByUser["alice"]
	bobPct := eval.MemoryPctByUser["bob"]
	assert.Greater(t, alicePct, 4.0)
	assert.Less(t, alicePct, 6.0)
	assert.Greater(t, bobPct, 7.0)
	assert.Less(t, bobPct, 9.0)

	// Verify no kill candidates (under limits)
	assert.Equal(t, 0, len(eval.KillCandidates))
}

// TestMonitorProcessesMemLimitsWithKillCandidates tests that kill candidates are tracked
func TestMonitorProcessesMemLimitsWithKillCandidates(t *testing.T) {
	initLogger()

	// Create config with strict MemLimits
	cfg := &config.Config{
		MemLimits: &config.MemLimits{
			Enabled: true,
			CandidateCmds: ".*",
			ReCandidateCmds: regexp.MustCompile(".*"),
			Groups: []config.MemLimitGroup{
				{
					Description: "strict_group",
					Users:       ".*",
					ReUsers:     regexp.MustCompile(".*"),
					CmdMaxPercentageInt: 5, // 5% limit - strict
				},
			},
		},
	}

	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)

	// Mock memory reader with high memory usage
	p4m.memReader = &FakeMemReader{
		RSSByPid: map[int]int64{
			1000: 100 * 1024 * 1024, // 10% - exceeds 5% limit
			1001: 50 * 1024 * 1024,  // 5% - exactly at limit
		},
		TotalMemory: 1000 * 1024 * 1024,
	}

	// Simulate monitor show output
	monitorOutput := []string{
		"1000 R alice 00:00:05 sync",
		"1001 R bob 00:00:10 edit",
	}

	// Parse and evaluate
	result := p4m.parseMonitorShow(monitorOutput)
	eval, err := evaluateMemLimits(result.processes, p4m.config.MemLimits, p4m.memReader, p4m.logger)
	assert.NoError(t, err)
	assert.NotNil(t, eval)

	// Should have 1 kill candidate (sync from alice at 10%)
	assert.Equal(t, 1, len(eval.KillCandidates))
	assert.Equal(t, 1000, eval.KillCandidates[0].Pid)
	assert.Equal(t, "alice", eval.KillCandidates[0].User)
	assert.Equal(t, "sync", eval.KillCandidates[0].Cmd)
	assert.Equal(t, "cmd_max_percentage", eval.KillCandidates[0].ReasonType)
}

// FakeTerminator is a mock implementation of ProcessTerminator for testing
type FakeTerminator struct {
	TerminatedPIDs []int         // PIDs that were terminated
	ShouldFail     bool          // If true, return error for all terminate calls
	DryRunMode     bool          // Track if operating in dry-run mode
}

func (f *FakeTerminator) TerminateProcess(pid int, user, cmd string) (bool, error) {
	if f.ShouldFail {
		return false, fmt.Errorf("simulated termination failure for PID %d", pid)
	}
	f.TerminatedPIDs = append(f.TerminatedPIDs, pid)
	return true, nil
}

// TestTerminateMemLimitViolators tests the termination logic
func TestTerminateMemLimitViolators(t *testing.T) {
	initLogger()

	cfg := &config.Config{
		MemLimits: &config.MemLimits{
			Enabled:       true,
			EnforceKills:  true,
			ReCandidateCmds: regexp.MustCompile(".*"),
			Groups: []config.MemLimitGroup{
				{
					Description: "test_group",
					Users:       ".*",
					ReUsers:     regexp.MustCompile(".*"),
					CmdMaxPercentageInt: 5,
				},
			},
		},
	}

	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)

	// Create evaluation with kill candidates
	eval := &MemLimitEvaluation{
		MemoryPctByCmd:  make(map[string]float64),
		MemoryPctByUser: make(map[string]float64),
		KillCandidates: []KillAction{
			{Pid: 100, User: "alice", Cmd: "sync", RSSBytes: 100*1024*1024, MemPercentage: 10.0, ReasonType: "cmd_max_percentage", MatchedGroup: "test_group", ThresholdValue: "5%"},
			{Pid: 101, User: "bob", Cmd: "edit", RSSBytes: 80*1024*1024, MemPercentage: 8.0, ReasonType: "cmd_max_percentage", MatchedGroup: "test_group", ThresholdValue: "5%"},
		},
	}

	// Test with FakeTerminator
	terminator := &FakeTerminator{}
	killed := p4m.terminateMemLimitViolators(eval, terminator)

	assert.Equal(t, 2, killed, "should have killed 2 processes")
	assert.Equal(t, 2, p4m.memlimitKillCount, "kill counter should be incremented")
	assert.Equal(t, []int{100, 101}, terminator.TerminatedPIDs, "correct PIDs should be terminated")
}

// TestTerminateMemLimitViolatorsWithFailure tests termination failure handling
func TestTerminateMemLimitViolatorsWithFailure(t *testing.T) {
	initLogger()

	cfg := &config.Config{
		MemLimits: &config.MemLimits{
			Enabled:       true,
			EnforceKills:  true,
			ReCandidateCmds: regexp.MustCompile(".*"),
			Groups: []config.MemLimitGroup{
				{
					Description: "test_group",
					Users:       ".*",
					ReUsers:     regexp.MustCompile(".*"),
					CmdMaxPercentageInt: 5,
				},
			},
		},
	}

	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)

	// Create evaluation with kill candidates
	eval := &MemLimitEvaluation{
		MemoryPctByCmd:  make(map[string]float64),
		MemoryPctByUser: make(map[string]float64),
		KillCandidates: []KillAction{
			{Pid: 100, User: "alice", Cmd: "sync", RSSBytes: 100*1024*1024, MemPercentage: 10.0, ReasonType: "cmd_max_percentage", MatchedGroup: "test_group", ThresholdValue: "5%"},
			{Pid: 101, User: "bob", Cmd: "edit", RSSBytes: 80*1024*1024, MemPercentage: 8.0, ReasonType: "cmd_max_percentage", MatchedGroup: "test_group", ThresholdValue: "5%"},
		},
	}

	// Test with failing FakeTerminator
	terminator := &FakeTerminator{ShouldFail: true}
	killed := p4m.terminateMemLimitViolators(eval, terminator)

	assert.Equal(t, 0, killed, "should have killed 0 processes when terminator fails")
	assert.Equal(t, 0, p4m.memlimitKillCount, "kill counter should remain 0 on failure")
}

// TestTerminateMemLimitViolatorsEmpty tests with no kill candidates
func TestTerminateMemLimitViolatorsEmpty(t *testing.T) {
	initLogger()

	cfg := &config.Config{}
	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)

	eval := &MemLimitEvaluation{
		MemoryPctByCmd:  make(map[string]float64),
		MemoryPctByUser: make(map[string]float64),
		KillCandidates:  []KillAction{},
	}

	terminator := &FakeTerminator{}
	killed := p4m.terminateMemLimitViolators(eval, terminator)

	assert.Equal(t, 0, killed, "should kill no processes when none are candidates")
	assert.Equal(t, 0, len(terminator.TerminatedPIDs), "terminator should not be called")
}

// TestP4ProcessTerminatorDryRun tests dry-run mode
func TestP4ProcessTerminatorDryRun(t *testing.T) {
	initLogger()

	cfg := &config.Config{}
	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)
	p4m.dryrun = true // Set dry-run mode

	terminator := &P4ProcessTerminator{
		p4m:    p4m,
		logger: tlogger,
	}

	// In dry-run mode, should not fail and should not actually execute
	success, err := terminator.TerminateProcess(999, "testuser", "testtcmd")
	assert.NoError(t, err, "dry-run should not error")
	assert.True(t, success, "dry-run should return success")
}

// TestMemLimitsIntegrationWithEnforcement tests full flow from evaluation to termination
func TestMemLimitsIntegrationWithEnforcement(t *testing.T) {
	initLogger()

	cfg := &config.Config{
		MemLimits: &config.MemLimits{
			Enabled:         true,
			EnforceKills:    true,
			CandidateCmds:   ".*",
			ReCandidateCmds: regexp.MustCompile(".*"),
			Groups: []config.MemLimitGroup{
				{
					Description: "strict_group",
					Users:       ".*",
					ReUsers:     regexp.MustCompile(".*"),
					CmdMaxPercentageInt: 5,
				},
			},
		},
	}

	env := map[string]string{}
	p4m := newP4MonitorMetrics(cfg, &env, tlogger)

	// Replace terminator with fake for testing
	fakeTerminator := &FakeTerminator{}
	p4m.terminator = fakeTerminator

	// Simulate monitor output with high memory usage
	monitorOutput := []string{
		"1000 R alice 00:00:05 sync",
		"1001 R bob 00:00:10 edit",
	}

	// Parse monitor output
	result := p4m.parseMonitorShow(monitorOutput)

	// Mock memory reader with high usage
	fakeMemReader := &FakeMemReader{
		RSSByPid: map[int]int64{
			1000: 100 * 1024 * 1024, // 10% - exceeds 5% limit
			1001: 80 * 1024 * 1024,  // 8% - exceeds 5% limit
		},
		TotalMemory: 1000 * 1024 * 1024,
	}
	p4m.memReader = fakeMemReader

	// Evaluate memory limits
	eval, err := evaluateMemLimits(result.processes, p4m.config.MemLimits, p4m.memReader, p4m.logger)
	assert.NoError(t, err)
	assert.NotNil(t, eval)
	assert.Equal(t, 2, len(eval.KillCandidates), "should have 2 kill candidates")

	// Terminate violators
	killed := p4m.terminateMemLimitViolators(eval, p4m.terminator)
	assert.Equal(t, 2, killed, "should have killed 2 processes")
	assert.Equal(t, 2, p4m.memlimitKillCount, "kill counter should be 2")
	assert.Equal(t, []int{1000, 1001}, fakeTerminator.TerminatedPIDs, "correct PIDs terminated")
}
