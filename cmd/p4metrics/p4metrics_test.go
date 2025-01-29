// Test packa for p4metrics
package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/perforce/p4prometheus/cmd/p4metrics/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var (
	tlogger = &logrus.Logger{Out: os.Stderr,
		Formatter: &logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true},
		// Level:     logrus.DebugLevel}
		Level: logrus.InfoLevel}
)

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
		if metric.label.name != "" {
			k = fmt.Sprintf("%s/%s/%s", k, metric.label.name, metric.label.value)
		}
		actualMap[k] = metricValue{key: k, name: metric.name, value: metric.value,
			labelName: metric.label.name, labelValue: metric.label.value}
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
	logrus.SetFormatter(&logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true})
	tlogger.SetReportCaller(true)
	// logger.Debugf("Function: %s", funcName())
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, &logger)
	p4m.p4license = map[string]string{
		"userCount":            "893",
		"userLimit":            "1000",
		"licenseExpires":       "1677628800",
		"licenseTimeRemaining": "34431485",
		"supportExpires":       "1677628800"}
	p4m.parseLicense()
	assert.Equal(t, 5, len(p4m.metrics))
	tlogger.Infof("Metrics: %q", p4m.metrics)

	p4m.metrics = make([]metricStruct, 0)
	p4m.p4license = map[string]string{
		"userCount":      "893",
		"userLimit":      "1000",
		"supportExpires": "1772323200"}
	p4m.p4info["Server license"] = "Perforce Software, Inc. 999 users (support ends 2025/02/28)"
	p4m.p4info["Server date"] = "2025/01/28 03:29:58 -0800 PST"
	p4m.parseLicense()
	assert.Equal(t, 5, len(p4m.metrics))
	tlogger.Infof("Metrics: %q", p4m.metrics)
	expected := metricValues{
		{name: "p4_licensed_user_count", value: "893"},
		{name: "p4_licensed_user_limit", value: "1000"},
		{name: "p4_license_time_remaining", value: "34259402"},
		{name: "p4_license_support_expires", value: "1772323200"},
		{name: "p4_license_info", value: "1", labelName: "licenseInfo", labelValue: "Perforce Software, Inc. 999 users"},
	}
	compareMetricValues(t, expected, p4m.metrics)
}

func TestP4MetricsFilesys(t *testing.T) {
	cfg := config.Config{}
	logrus.SetFormatter(&logrus.TextFormatter{TimestampFormat: "15:04:05.000", FullTimestamp: true})
	tlogger.SetReportCaller(true)
	env := map[string]string{}
	p4m := newP4MonitorMetrics(&cfg, &env, &logger)
	p4m.parseFilesys([]string{"filesys.P4ROOT.min=5G (configure)",
		"filesys.P4ROOT.min=250M (default)"})
	expected := metricValues{
		{name: "p4_filesys_min", value: "5368709120", labelName: "filesys", labelValue: "P4ROOT"},
	}
	tlogger.Infof("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)

	p4m = newP4MonitorMetrics(&cfg, &env, &logger)
	p4m.parseFilesys([]string{"filesys.P4ROOT.min=250M (default)"})
	expected = metricValues{
		{name: "p4_filesys_min", value: "262144000", labelName: "filesys", labelValue: "P4ROOT"},
	}
	tlogger.Infof("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)

	p4m = newP4MonitorMetrics(&cfg, &env, &logger)
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
	tlogger.Infof("Metrics: %q", p4m.metrics)
	compareMetricValues(t, expected, p4m.metrics)
}
