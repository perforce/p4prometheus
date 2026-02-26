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
				"12347 I charlie 00:00:15 sync",
			},
			expectedCmds: map[string]int{
				"sync": 2,
				"edit": 1,
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
				"12347 I charlie 00:00:15 shelve",
				"12348 R dave 00:00:20 unshelve",
			},
			monitorGroups: []config.MonitorGroup{
				{Commands: "sync|transmit", Label: "sync_transmit"},
				{Commands: "shelve|unshelve", Label: "shelf_ops"},
			},
			expectedCmds: map[string]int{
				"sync":     1,
				"transmit": 1,
				"shelve":   1,
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
				"sync_transmit": 2,
				"shelf_ops":     2,
			},
			expectedRuntime: map[string]int{
				"sync_transmit": 15, // 5 + 10
				"shelf_ops":     35, // 15 + 20
			},
			expectedMaxRuntime: map[string]int{
				"sync_transmit": 10, // max(5, 10)
				"shelf_ops":     20, // max(15, 20)
			},
			expectedMaxTime: 20,
		},
		{
			name: "with monitor ignore",
			monitorOutput: []string{
				"12345 R alice 00:00:05 sync",
				"12346 R svc 00:10:00 ldapsync",
				"12347 I charlie 00:00:15 shelve",
			},
			monitorIgnore: "ldapsync",
			monitorGroups: []config.MonitorGroup{
				{Commands: ".*", Label: "other"},
			},
			expectedCmds: map[string]int{
				"sync":     1,
				"ldapsync": 1,
				"shelve":   1,
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
				"other": 2, // sync and shelve, but not ldapsync (ignored)
			},
			expectedRuntime: map[string]int{
				"other": 20, // 5 + 15, ldapsync is ignored
			},
			expectedMaxRuntime: map[string]int{
				"other": 15, // max(5, 15), ldapsync is ignored
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
