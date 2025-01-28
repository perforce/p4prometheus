// Test packa for p4metrics
package main

import (
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

type metricValues []struct {
	name  string
	value string
}

func compareMetricValues(t *testing.T, expected metricValues, actual []metricStruct) {
	t.Helper()

	actualMap := make(map[string]string)
	for _, metric := range actual {
		actualMap[metric.name] = metric.value
	}
	if len(actual) != len(expected) {
		t.Errorf("metric count mismatch: got %d metrics, want %d",
			len(actual), len(expected))
	}
	for _, exp := range expected {
		actualValue, exists := actualMap[exp.name]
		if !exists {
			t.Errorf("missing metric %q", exp.name)
			continue
		}
		if actualValue != exp.value {
			t.Errorf("metric %q: got value %q, want %q",
				exp.name, actualValue, exp.value)
		}
	}
	for name := range actualMap {
		found := false
		for _, exp := range expected {
			if name == exp.name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected metric %q", name)
		}
	}
}

func TestP4PromBasic(t *testing.T) {
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
		{name: "p4_license_info", value: "1"},
	}
	compareMetricValues(t, expected, p4m.metrics)
}
