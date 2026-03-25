//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func caddyAddr() string {
	if addr := os.Getenv("EMBER_TEST_CADDY_ADDR"); addr != "" {
		return addr
	}
	return "http://localhost:2019"
}

func TestIntegration_Wait(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runWait(ctx, &buf, f, addr, 500*time.Millisecond)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ready")
}

func TestIntegration_WaitQuiet(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := runWait(ctx, io.Discard, f, addr, 500*time.Millisecond)
	require.NoError(t, err)
}

func TestIntegration_Init(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runInit(ctx, &buf, strings.NewReader("y\n"), f, addr, true)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Admin API reachable")
	assert.Contains(t, out, "HTTP metrics enabled")
	assert.Contains(t, out, "Ember is ready")
}

func TestIntegration_InitEnablesMetrics(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	enabled, err := f.CheckMetricsEnabled(ctx)
	require.NoError(t, err)

	if !enabled {
		err = f.EnableMetrics(ctx)
		require.NoError(t, err)
	}

	enabled, err = f.CheckMetricsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled, "metrics should be enabled after init")
}

func TestIntegration_Status(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runStatus(ctx, &buf, f, addr, 500*time.Millisecond, false)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Caddy OK")
}

func TestIntegration_JSONOnce(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	require.NotNil(t, snap)

	var state model.State
	state.Update(snap)
	out := buildJSONOutput(snap, &state)

	data, err := json.Marshal(out)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err, "JSON output must be valid")
	assert.Contains(t, parsed, "threads")
	assert.Contains(t, parsed, "metrics")
	assert.Contains(t, parsed, "process")
	assert.Contains(t, parsed, "fetchedAt")

	threads, ok := parsed["threads"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, threads, "threadDebugStates", "thread fields should be camelCase")
	assert.Contains(t, threads, "reservedThreadCount", "thread fields should be camelCase")
}

func TestIntegration_Diff(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	var state model.State
	state.Update(snap)
	out := buildJSONOutput(snap, &state)

	before, err := os.CreateTemp(t.TempDir(), "before-*.json")
	require.NoError(t, err)
	after, err := os.CreateTemp(t.TempDir(), "after-*.json")
	require.NoError(t, err)

	require.NoError(t, json.NewEncoder(before).Encode(out))
	require.NoError(t, json.NewEncoder(after).Encode(out))
	before.Close()
	after.Close()

	var buf bytes.Buffer
	err = runDiff(&buf, before.Name(), after.Name())

	require.NoError(t, err, "same-snapshot diff should not report regressions")
	assert.Contains(t, buf.String(), "No regressions detected")
}

func TestIntegration_FetchServerNames(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	names := f.FetchServerNames(ctx)
	assert.NotEmpty(t, names, "Caddy should have at least one server configured")
}

func TestIntegration_AdminAPI(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := f.CheckAdminAPI(ctx)
	require.NoError(t, err, "admin API should be reachable")
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestIntegration_Daemon_Metrics(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	expose := freePort(t)
	holder := &exporter.StateHolder{}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder))
	mux.HandleFunc("/healthz", exporter.HealthHandler(holder, 1*time.Second))
	srv := &http.Server{Addr: expose, Handler: mux}

	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// poll twice to populate derived metrics
	var state model.State
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	holder.Store(state.CopyForExport())

	time.Sleep(500 * time.Millisecond)

	snap, err = f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	holder.Store(state.CopyForExport())

	metricsURL := fmt.Sprintf("http://%s/metrics", expose)
	resp, err := http.Get(metricsURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	metricsBody := string(body)

	assert.Contains(t, metricsBody, "process_cpu_percent")
	assert.Contains(t, metricsBody, "process_rss_bytes")
	assert.Contains(t, metricsBody, "# TYPE")
	assert.Contains(t, metricsBody, "# HELP")
}

func TestIntegration_Daemon_Healthz(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	expose := freePort(t)
	holder := &exporter.StateHolder{}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder))
	mux.HandleFunc("/healthz", exporter.HealthHandler(holder, 1*time.Second))
	srv := &http.Server{Addr: expose, Handler: mux}

	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	healthURL := fmt.Sprintf("http://%s/healthz", expose)

	// no data yet: 503
	resp, err := http.Get(healthURL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	// feed a fresh snapshot
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var state model.State
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	holder.Store(state.CopyForExport())

	// now healthy: 200
	resp, err = http.Get(healthURL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var healthBody map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&healthBody))
	assert.Equal(t, "ok", healthBody["status"])
	assert.Contains(t, healthBody, "last_fetch")
	assert.Contains(t, healthBody, "age_seconds")
}
