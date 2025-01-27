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
}
