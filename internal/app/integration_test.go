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

func waitForServer(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server %s not ready within %s", url, timeout)
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

	waitForServer(t, fmt.Sprintf("http://%s/healthz", expose), 2*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// poll twice to populate derived metrics
	var state model.State
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	holder.Store(state.CopyForExport())

	time.Sleep(200 * time.Millisecond)

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

	waitForServer(t, fmt.Sprintf("http://%s/healthz", expose), 2*time.Second)

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

func TestIntegration_Wait_Unreachable(t *testing.T) {
	closedPort := freePort(t)

	f := fetcher.NewHTTPFetcher("http://"+closedPort, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runWait(ctx, &buf, f, "http://"+closedPort, 200*time.Millisecond)

	require.Error(t, err, "runWait should fail when Caddy is unreachable")
}

func TestIntegration_Daemon_BasicAuth(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	expose := freePort(t)
	holder := &exporter.StateHolder{}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var state model.State
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	holder.Store(state.CopyForExport())

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder))
	mux.HandleFunc("/healthz", exporter.HealthHandler(holder, 1*time.Second))
	srv := &http.Server{Addr: expose, Handler: exporter.BasicAuth(mux, "admin", "secret")}

	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()

	waitForServer(t, fmt.Sprintf("http://admin:secret@%s/healthz", expose), 2*time.Second)

	baseURL := fmt.Sprintf("http://%s", expose)

	// no credentials: 401
	resp, err := http.Get(baseURL + "/metrics")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// wrong password: 401
	req, _ := http.NewRequest("GET", baseURL+"/metrics", nil)
	req.SetBasicAuth("admin", "wrong")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// correct credentials: 200
	req, _ = http.NewRequest("GET", baseURL+"/metrics", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// healthz also protected
	resp, err = http.Get(baseURL + "/healthz")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req, _ = http.NewRequest("GET", baseURL+"/healthz", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestIntegration_Daemon_MetricsPrefix(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	expose := freePort(t)
	holder := &exporter.StateHolder{}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var state model.State
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	holder.Store(state.CopyForExport())

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder, "myapp"))
	srv := &http.Server{Addr: expose, Handler: mux}

	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()

	waitForServer(t, fmt.Sprintf("http://%s/metrics", expose), 2*time.Second)

	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", expose))
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	metricsBody := string(body)

	assert.Contains(t, metricsBody, "myapp_process_cpu_percent")
	assert.Contains(t, metricsBody, "myapp_process_rss_bytes")
	assert.NotContains(t, metricsBody, "\nprocess_cpu_percent", "unprefixed metrics should not appear")
}

func TestIntegration_MultipleFetchCycles(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var state model.State

	// 1st fetch: no previous snapshot, RPS should be 0
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	assert.Equal(t, 0.0, state.Derived.RPS, "RPS should be 0 after first fetch")

	time.Sleep(200 * time.Millisecond)

	// 2nd fetch: derived metrics should be populated
	snap, err = f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	assert.GreaterOrEqual(t, state.Derived.RPS, 0.0, "RPS should be non-negative after second fetch")

	time.Sleep(200 * time.Millisecond)

	// 3rd fetch: state should remain consistent
	snap, err = f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)
	assert.NotNil(t, state.Current, "Current snapshot should not be nil")
	assert.NotNil(t, state.Previous, "Previous snapshot should not be nil after 3 fetches")
}

func TestIntegration_StatusJSON(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runStatus(ctx, &buf, f, addr, 500*time.Millisecond, true)

	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed), "status output should be valid JSON")
	assert.Contains(t, parsed, "status")
	assert.Contains(t, parsed, "hosts")
}

func TestIntegration_EnableMetrics_Idempotent(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := f.EnableMetrics(ctx)
	require.NoError(t, err, "first EnableMetrics call should succeed")

	err = f.EnableMetrics(ctx)
	require.NoError(t, err, "second EnableMetrics call should succeed (idempotent)")

	enabled, err := f.CheckMetricsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled, "metrics should still be enabled after double call")
}

func TestIntegration_Status_Unreachable(t *testing.T) {
	closedPort := freePort(t)
	addr := "http://" + closedPort

	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runStatus(ctx, &buf, f, addr, 200*time.Millisecond, false)

	require.Error(t, err, "status should fail when Caddy is unreachable")
	assert.Contains(t, err.Error(), "unreachable")
	assert.Contains(t, buf.String(), "UNREACHABLE")
}

func TestIntegration_Status_Unreachable_JSON(t *testing.T) {
	closedPort := freePort(t)
	addr := "http://" + closedPort

	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := runStatus(ctx, &buf, f, addr, 200*time.Millisecond, true)

	require.Error(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed), "unreachable status should still output valid JSON")
	assert.Equal(t, "unreachable", parsed["status"])
}

func TestIntegration_FetchConfig(t *testing.T) {
	addr := caddyAddr()
	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	raw, err := f.FetchConfig(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed))
	assert.Contains(t, parsed, "apps", "Caddy config should contain an apps key")
}

func TestIntegration_HostMetrics(t *testing.T) {
	addr := caddyAddr()

	// generate traffic against the :8080 vhost so Caddy records per-host metrics
	for range 20 {
		resp, err := http.Get("http://localhost:8080/")
		if err == nil {
			resp.Body.Close()
		}
	}

	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	f.FetchServerNames(ctx)

	var state model.State
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)

	time.Sleep(300 * time.Millisecond)

	snap, err = f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)

	assert.NotEmpty(t, state.HostDerived, "should have per-host metrics after traffic")
}

func TestIntegration_PrometheusRoundTrip_WithHosts(t *testing.T) {
	addr := caddyAddr()

	for range 10 {
		resp, err := http.Get("http://localhost:8080/")
		if err == nil {
			resp.Body.Close()
		}
	}

	f := fetcher.NewHTTPFetcher(addr, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	f.FetchServerNames(ctx)

	var state model.State
	snap, err := f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)

	time.Sleep(300 * time.Millisecond)

	snap, err = f.Fetch(ctx)
	require.NoError(t, err)
	state.Update(snap)

	expose := freePort(t)
	holder := &exporter.StateHolder{}
	holder.Store(state.CopyForExport())

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", exporter.Handler(holder))
	srv := &http.Server{Addr: expose, Handler: mux}

	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()

	waitForServer(t, fmt.Sprintf("http://%s/metrics", expose), 2*time.Second)

	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", expose))
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	metricsBody := string(body)

	assert.Contains(t, metricsBody, "ember_host_rps")
	assert.Contains(t, metricsBody, "ember_host_latency_avg_milliseconds")
	assert.Contains(t, metricsBody, "process_cpu_percent")
}

func TestIntegration_Diff_FileNotFound(t *testing.T) {
	var buf bytes.Buffer
	err := runDiff(&buf, "/tmp/does-not-exist-ember-test.json", "/tmp/also-missing.json")

	require.Error(t, err, "diff with missing files should fail")
	assert.Contains(t, err.Error(), "does-not-exist")
}

func TestIntegration_Diff_InvalidJSON(t *testing.T) {
	bad, _ := os.CreateTemp(t.TempDir(), "bad-*.json")
	_, _ = bad.WriteString("not json at all {{{")
	bad.Close()

	good, _ := os.CreateTemp(t.TempDir(), "good-*.json")
	_, _ = good.WriteString("{}")
	good.Close()

	var buf bytes.Buffer
	err := runDiff(&buf, bad.Name(), good.Name())

	require.Error(t, err, "diff with invalid JSON should fail")
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestIntegration_Diff_EmptyFile(t *testing.T) {
	empty, _ := os.CreateTemp(t.TempDir(), "empty-*.json")
	empty.Close()

	good, _ := os.CreateTemp(t.TempDir(), "good-*.json")
	_, _ = good.WriteString("{}")
	good.Close()

	var buf bytes.Buffer
	err := runDiff(&buf, empty.Name(), good.Name())

	require.Error(t, err, "diff with empty file should fail")
	assert.Contains(t, err.Error(), "empty")
}
