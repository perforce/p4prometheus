package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/perforce/p4prometheus/cmd/p4proxyprometheus/config"
	"github.com/rcowham/go-libp4dlog/p4plog"
	"github.com/stretchr/testify/require"
)

func TestMetricStateRender(t *testing.T) {
	state := metricState{}
	state.observe(p4plog.ProxyCommand{
		CompletedLapse:        1.25,
		EndTime:               time.Unix(1_730_000_000, 0),
		ProxyTotalsSvr:        3,
		ProxyTotalsCache:      5,
		ProxyTotalsSvrBytes:   1024,
		ProxyTotalsCacheBytes: 2048,
	})
	state.observe(p4plog.ProxyCommand{
		CompletedLapse:        2.5,
		EndTime:               time.Unix(1_730_000_100, 0),
		ProxyTotalsSvr:        7,
		ProxyTotalsCache:      11,
		ProxyTotalsSvrBytes:   4096,
		ProxyTotalsCacheBytes: 8192,
	})

	metrics := string(state.render())
	require.Contains(t, metrics, "p4_proxy_commands_completed_total 2")
	require.Contains(t, metrics, "p4_proxy_command_duration_seconds_total 3.750000")
	require.Contains(t, metrics, "p4_proxy_files_total{source=\"server\"} 10")
	require.Contains(t, metrics, "p4_proxy_files_total{source=\"cache\"} 16")
	require.Contains(t, metrics, "p4_proxy_bytes_total{source=\"server\"} 5120")
	require.Contains(t, metrics, "p4_proxy_bytes_total{source=\"cache\"} 10240")
	require.Contains(t, metrics, "p4_proxy_last_command_completion_timestamp_seconds 1730000100")
}

func TestWriteMetricsFileAtomically(t *testing.T) {
	output := filepath.Join(t.TempDir(), "p4_proxy.prom")
	require.NoError(t, writeMetricsFile(output, []byte("metric 1\n")))

	content, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, "metric 1\n", string(content))

	entries, err := os.ReadDir(filepath.Dir(output))
	require.NoError(t, err)
	for _, entry := range entries {
		require.False(t, strings.Contains(entry.Name(), ".tmp-"))
	}
}

func TestMetricsOutputFilename(t *testing.T) {
	for _, output := range []string{"/p4/metrics/p4_proxy", "/p4/metrics/p4_proxy.prom"} {
		t.Run(output, func(t *testing.T) {
			cfg := config.Config{P4PLog: "/p4p.log", MetricsOutput: output, SDPInstance: "1"}
			require.NoError(t, cfg.Validate())
			require.Equal(t, "/p4/metrics/p4_proxy-1.prom", metricsOutputFilename(cfg.MetricsOutput, cfg.SDPInstance))
		})
	}
}

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := config.LoadConfigFile("p4proxyprometheus.yaml")
	require.NoError(t, err)
	require.Equal(t, "/p4/metrics/p4_proxy-1.prom", metricsOutputFilename(cfg.MetricsOutput, cfg.SDPInstance))
}
