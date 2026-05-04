package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(metricsBody))
		case "/config/apps/http/servers":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"main": map[string]any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
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
	err := runStatus(context.Background(), &buf, f, srv.URL, 10*time.Millisecond, false)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Caddy OK")
}

func TestRunStatus_Unreachable(t *testing.T) {
	// Server that returns errors for everything
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)

	var buf bytes.Buffer
	err := runStatus(context.Background(), &buf, f, srv.URL, 10*time.Millisecond, false)

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
	err := runStatus(ctx, &buf, f, srv.URL, 10*time.Second, false)

	require.Error(t, err)
}

func TestRunStatus_JSON(t *testing.T) {
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
	err := runStatus(context.Background(), &buf, f, srv.URL, 10*time.Millisecond, true)

	require.NoError(t, err)

	var result statusJSON
	require.NoError(t, json.NewDecoder(&buf).Decode(&result))
	assert.Equal(t, "ok", result.Status)
	assert.Positive(t, result.Hosts)
	assert.Nil(t, result.FrankenPHP)
}

func TestRunStatus_JSON_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)

	var buf bytes.Buffer
	err := runStatus(context.Background(), &buf, f, srv.URL, 10*time.Millisecond, true)

	require.Error(t, err)

	var result statusJSON
	require.NoError(t, json.NewDecoder(&buf).Decode(&result))
	assert.Equal(t, "unreachable", result.Status)
	assert.Equal(t, srv.URL, result.Addr)
}

func TestBuildStatusJSON_Basic(t *testing.T) {
	s := testState()
	s.Derived.RPS = 450

	result := buildStatusJSON(&s, false)

	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, 1, result.Hosts)
	assert.Equal(t, 450.0, result.RPS)
	assert.Equal(t, 3.2, result.CPUPercent)
	assert.Equal(t, uint64(48*1024*1024), result.RSSBytes)
	assert.Equal(t, "3d 2h", result.UptimeHuman)
	assert.Nil(t, result.P99)
	assert.Nil(t, result.FrankenPHP)
}

func TestBuildStatusJSON_WithFrankenPHP(t *testing.T) {
	s := testState(func(snap *fetcher.Snapshot) {
		snap.Metrics.Workers = map[string]*fetcher.WorkerMetrics{
			"/app/worker.php": {Total: 10},
		}
	})
	s.Derived.TotalBusy = 5
	s.Derived.TotalIdle = 15

	result := buildStatusJSON(&s, true)

	require.NotNil(t, result.FrankenPHP)
	assert.Equal(t, 5, result.FrankenPHP.Busy)
	assert.Equal(t, 20, result.FrankenPHP.Total)
	assert.Equal(t, 1, result.FrankenPHP.Workers)
}

func TestBuildStatusJSON_WithPercentiles(t *testing.T) {
	s := testState()
	s.Derived.HasPercentiles = true
	s.Derived.P99 = 85.3

	result := buildStatusJSON(&s, false)

	require.NotNil(t, result.P99)
	assert.Equal(t, 85.3, *result.P99)
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

// statusFakeCaddy returns a server suitable for runStatusMulti. Counters
// advance on each /metrics scrape so the second fetch sees a non-zero delta
// and derived RPS becomes non-zero.
func statusFakeCaddy(t *testing.T, host string, step int) *httptest.Server {
	t.Helper()
	var n int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			mu.Lock()
			n += step
			cur := n
			mu.Unlock()
			fmt.Fprintf(w, `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="srv0",host=%q,code="200"} %d
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="srv0",host=%q,le="+Inf"} %d
caddy_http_request_duration_seconds_sum{server="srv0",host=%q} %f
caddy_http_request_duration_seconds_count{server="srv0",host=%q} %d
`, host, cur, host, cur, host, float64(cur)*0.01, host, cur)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newMultiCfg(t *testing.T, addrs []addrSpec) *config {
	t.Helper()
	return &config{
		addrs:    addrs,
		interval: 50 * time.Millisecond,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestRunStatusMulti_AllOk_Text(t *testing.T) {
	web1 := statusFakeCaddy(t, "web1.example", 100)
	web2 := statusFakeCaddy(t, "web2.example", 250)

	cfg := newMultiCfg(t, []addrSpec{
		{name: "web2", url: web2.URL},
		{name: "web1", url: web1.URL},
	})
	var buf bytes.Buffer
	require.NoError(t, runStatusMulti(context.Background(), &buf, cfg, false))

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	// Output must be sorted alphabetically by name regardless of cfg.addrs order.
	assert.True(t, strings.HasPrefix(lines[0], "[web1] Caddy OK"))
	assert.True(t, strings.HasPrefix(lines[1], "[web2] Caddy OK"))
	assert.Contains(t, lines[0], "1 hosts")
	assert.Contains(t, lines[1], "1 hosts")
}

func TestRunStatusMulti_AllOk_JSON(t *testing.T) {
	web1 := statusFakeCaddy(t, "web1.example", 100)
	web2 := statusFakeCaddy(t, "web2.example", 250)

	cfg := newMultiCfg(t, []addrSpec{
		{name: "web1", url: web1.URL},
		{name: "web2", url: web2.URL},
	})
	var buf bytes.Buffer
	require.NoError(t, runStatusMulti(context.Background(), &buf, cfg, true))

	var body statusMultiJSON
	require.NoError(t, json.NewDecoder(&buf).Decode(&body))
	assert.Equal(t, "ok", body.Status)
	require.Len(t, body.Instances, 2)
	assert.Equal(t, "web1", body.Instances[0].Name)
	assert.Equal(t, "ok", body.Instances[0].Status)
	assert.Equal(t, web1.URL, body.Instances[0].Addr)
	assert.Equal(t, "web2", body.Instances[1].Name)
	assert.Equal(t, "ok", body.Instances[1].Status)
}

func TestRunStatusMulti_OneDown_Text(t *testing.T) {
	web1 := statusFakeCaddy(t, "web1.example", 100)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)

	cfg := newMultiCfg(t, []addrSpec{
		{name: "web1", url: web1.URL},
		{name: "dead", url: dead.URL},
	})
	var buf bytes.Buffer
	err := runStatusMulti(context.Background(), &buf, cfg, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dead")

	out := buf.String()
	assert.Contains(t, out, "[dead] Caddy UNREACHABLE")
	assert.Contains(t, out, "[web1] Caddy OK")
}

func TestRunStatusMulti_OneDown_JSON(t *testing.T) {
	web1 := statusFakeCaddy(t, "web1.example", 100)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)

	cfg := newMultiCfg(t, []addrSpec{
		{name: "web1", url: web1.URL},
		{name: "dead", url: dead.URL},
	})
	var buf bytes.Buffer
	require.Error(t, runStatusMulti(context.Background(), &buf, cfg, true))

	var body statusMultiJSON
	require.NoError(t, json.NewDecoder(&buf).Decode(&body))
	assert.Equal(t, "degraded", body.Status, "mixed reachable/unreachable should yield degraded")
	require.Len(t, body.Instances, 2)
	assert.Equal(t, "dead", body.Instances[0].Name)
	assert.Equal(t, "unreachable", body.Instances[0].Status)
	assert.Equal(t, "web1", body.Instances[1].Name)
	assert.Equal(t, "ok", body.Instances[1].Status)
}

func TestRunStatusMulti_AllDown_JSON(t *testing.T) {
	deadOne := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(deadOne.Close)
	deadTwo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(deadTwo.Close)

	cfg := newMultiCfg(t, []addrSpec{
		{name: "alpha", url: deadOne.URL},
		{name: "beta", url: deadTwo.URL},
	})
	var buf bytes.Buffer
	require.Error(t, runStatusMulti(context.Background(), &buf, cfg, true))

	var body statusMultiJSON
	require.NoError(t, json.NewDecoder(&buf).Decode(&body))
	assert.Equal(t, "unreachable", body.Status, "every instance unreachable should yield unreachable")
	require.Len(t, body.Instances, 2)
	for _, inst := range body.Instances {
		assert.Equal(t, "unreachable", inst.Status)
	}
}

func TestRunStatusMulti_AllOk_ExitCodeZero(t *testing.T) {
	web1 := statusFakeCaddy(t, "web1.example", 100)
	web2 := statusFakeCaddy(t, "web2.example", 100)

	cfg := newMultiCfg(t, []addrSpec{
		{name: "web1", url: web1.URL},
		{name: "web2", url: web2.URL},
	})
	var buf bytes.Buffer
	assert.NoError(t, runStatusMulti(context.Background(), &buf, cfg, false))
}

func TestRunStatusMulti_DeterministicOrder(t *testing.T) {
	web1 := statusFakeCaddy(t, "web1.example", 50)
	web2 := statusFakeCaddy(t, "web2.example", 50)

	// Pass instances in inverse alphabetical order to verify the sort step.
	cfg := newMultiCfg(t, []addrSpec{
		{name: "zzz", url: web2.URL},
		{name: "aaa", url: web1.URL},
	})
	var buf1, buf2 bytes.Buffer
	require.NoError(t, runStatusMulti(context.Background(), &buf1, cfg, true))

	cfg2 := newMultiCfg(t, []addrSpec{
		{name: "aaa", url: web1.URL},
		{name: "zzz", url: web2.URL},
	})
	require.NoError(t, runStatusMulti(context.Background(), &buf2, cfg2, true))

	var body1, body2 statusMultiJSON
	require.NoError(t, json.Unmarshal(buf1.Bytes(), &body1))
	require.NoError(t, json.Unmarshal(buf2.Bytes(), &body2))

	require.Len(t, body1.Instances, 2)
	require.Len(t, body2.Instances, 2)
	assert.Equal(t, "aaa", body1.Instances[0].Name)
	assert.Equal(t, "zzz", body1.Instances[1].Name)
	assert.Equal(t, body1.Instances[0].Name, body2.Instances[0].Name, "ordering must be the same regardless of input order")
}
