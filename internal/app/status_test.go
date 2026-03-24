package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testState(opts ...func(*fetcher.Snapshot)) model.State {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers:        map[string]*fetcher.WorkerMetrics{},
			Hosts:          map[string]*fetcher.HostMetrics{"example.com": {Host: "example.com"}},
			HasHTTPMetrics: true,
		},
		Process: fetcher.ProcessMetrics{
			CPUPercent: 3.2,
			RSS:        48 * 1024 * 1024,
			Uptime:     74 * time.Hour,
		},
	}
	for _, o := range opts {
		o(snap)
	}
	var s model.State
	s.Update(snap)
	return s
}

func TestFormatStatusLine_Basic(t *testing.T) {
	s := testState()
	s.Derived.RPS = 450

	line := formatStatusLine(&s, false)

	assert.Contains(t, line, "Caddy OK")
	assert.Contains(t, line, "1 hosts")
	assert.Contains(t, line, "450 rps")
	assert.Contains(t, line, "CPU 3.2%")
	assert.Contains(t, line, "RSS 48MB")
	assert.Contains(t, line, "up 3d 2h")
	assert.NotContains(t, line, "FrankenPHP")
	assert.NotContains(t, line, "P99")
}

func TestFormatStatusLine_WithPercentiles(t *testing.T) {
	s := testState()
	s.Derived.HasPercentiles = true
	s.Derived.P99 = 85.3

	line := formatStatusLine(&s, false)

	assert.Contains(t, line, "P99 85ms")
}

func TestFormatStatusLine_WithFrankenPHP(t *testing.T) {
	s := testState(func(snap *fetcher.Snapshot) {
		snap.Metrics.Workers = map[string]*fetcher.WorkerMetrics{
			"/app/worker.php": {Total: 10},
			"/app/api.php":    {Total: 5},
		}
	})
	s.Derived.TotalBusy = 8
	s.Derived.TotalIdle = 12

	line := formatStatusLine(&s, true)

	assert.Contains(t, line, "FrankenPHP 8/20 busy")
	assert.Contains(t, line, "2 workers")
}

func TestFormatStatusLine_NoHosts(t *testing.T) {
	s := testState(func(snap *fetcher.Snapshot) {
		snap.Metrics.Hosts = map[string]*fetcher.HostMetrics{}
	})

	line := formatStatusLine(&s, false)

	assert.NotContains(t, line, "hosts")
}

func TestFormatStatusLine_NoUptime(t *testing.T) {
	s := testState(func(snap *fetcher.Snapshot) {
		snap.Process.Uptime = 0
	})

	line := formatStatusLine(&s, false)

	assert.NotContains(t, line, "up ")
}

func TestFormatStatusLine_ZeroRPS(t *testing.T) {
	s := testState()

	line := formatStatusLine(&s, false)

	assert.Contains(t, line, "0 rps")
}

// --- formatRSS unit tests ---

func TestFormatRSS_Megabytes(t *testing.T) {
	assert.Equal(t, "48MB", formatRSS(48*1024*1024))
}

func TestFormatRSS_Gigabytes(t *testing.T) {
	assert.Equal(t, "2.0GB", formatRSS(2*1024*1024*1024))
}

func TestFormatRSS_Zero(t *testing.T) {
	assert.Equal(t, "0MB", formatRSS(0))
}

// --- isReachable unit tests ---

func TestIsReachable_Nil(t *testing.T) {
	assert.False(t, isReachable(nil))
}

func TestIsReachable_HasHTTPMetrics(t *testing.T) {
	snap := &fetcher.Snapshot{Metrics: fetcher.MetricsSnapshot{HasHTTPMetrics: true}}
	assert.True(t, isReachable(snap))
}

func TestIsReachable_HasThreads(t *testing.T) {
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0}},
		},
	}
	assert.True(t, isReachable(snap))
}

func TestIsReachable_HasProcessRSS(t *testing.T) {
	snap := &fetcher.Snapshot{Process: fetcher.ProcessMetrics{RSS: 1024}}
	assert.True(t, isReachable(snap))
}

func TestIsReachable_HasPrometheusRSS(t *testing.T) {
	snap := &fetcher.Snapshot{Metrics: fetcher.MetricsSnapshot{ProcessRSSBytes: 1024}}
	assert.True(t, isReachable(snap))
}

func TestIsReachable_EmptySnapshot(t *testing.T) {
	snap := &fetcher.Snapshot{}
	assert.False(t, isReachable(snap))
}

// --- runStatus integration tests ---

func newStatusTestServer(metricsBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(200)
			w.Write([]byte(metricsBody))
		case "/config/apps/http/servers":
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]any{"main": map[string]any{}})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestRunStatus_OK(t *testing.T) {
	metrics := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="test.com",code="200"} 100
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{host="test.com",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{host="test.com"} 5.0
caddy_http_request_duration_seconds_count{host="test.com"} 100
`
	srv := newStatusTestServer(metrics)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)

	var buf bytes.Buffer
	err := runStatus(context.Background(), &buf, f, srv.URL, 10*time.Millisecond)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Caddy OK")
}

func TestRunStatus_Unreachable(t *testing.T) {
	// Server that returns errors for everything
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)

	var buf bytes.Buffer
	err := runStatus(context.Background(), &buf, f, srv.URL, 10*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, buf.String(), "UNREACHABLE")
	assert.Contains(t, err.Error(), "unreachable")
}

func TestRunStatus_ContextCanceled(t *testing.T) {
	metrics := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="test.com",code="200"} 100
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{host="test.com",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{host="test.com"} 5.0
caddy_http_request_duration_seconds_count{host="test.com"} 100
`
	srv := newStatusTestServer(metrics)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)

	ctx, cancel := context.WithCancel(context.Background())
	// cancel immediately so the inter-fetch wait is interrupted
	cancel()

	var buf bytes.Buffer
	err := runStatus(ctx, &buf, f, srv.URL, 10*time.Second)

	require.Error(t, err)
}

func TestRun_StatusHelp(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"status", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "two fetches")
	assert.Contains(t, out, "Exit code 0")
}

func TestRun_StatusInheritsAddr(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"status", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "--addr")
	assert.Contains(t, buf.String(), "--interval")
}
