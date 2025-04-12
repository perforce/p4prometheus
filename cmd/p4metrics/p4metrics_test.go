// Test packa for p4metrics
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
