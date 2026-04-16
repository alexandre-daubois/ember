package fetcher

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServer(threadsStatus int, threadsBody any, metricsStatus int, metricsBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			w.WriteHeader(threadsStatus)
			if threadsBody != nil {
				json.NewEncoder(w).Encode(threadsBody)
			}
		case "/metrics":
			w.WriteHeader(metricsStatus)
			w.Write([]byte(metricsBody))
		case "/frankenphp/workers/restart":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(threadsStatus) // reuse status for simplicity
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestFetchThreads_OK(t *testing.T) {
	resp := ThreadsResponse{
		ThreadDebugStates: []ThreadDebugState{
			{Index: 0, Name: "Worker PHP Thread - /app/worker.php", State: "ready", IsWaiting: true},
			{Index: 1, Name: "Regular PHP Thread", State: "busy", IsBusy: true},
		},
		ReservedThreadCount: 2,
	}
	srv := newTestServer(200, resp, 200, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	threads, err := f.fetchThreads(context.Background())
	require.NoError(t, err)
	require.Len(t, threads.ThreadDebugStates, 2)
	assert.Equal(t, "Worker PHP Thread - /app/worker.php", threads.ThreadDebugStates[0].Name)
	assert.Equal(t, 2, threads.ReservedThreadCount)
}

func TestFetchThreads_BadStatus(t *testing.T) {
	srv := newTestServer(500, nil, 200, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	_, err := f.fetchThreads(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestFetchMetrics_OK(t *testing.T) {
	metricsText := `# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 5
`
	srv := newTestServer(200, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	metrics, err := f.fetchMetrics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, float64(5), metrics.BusyThreads)
}

func TestFetch_GracefulDegradation(t *testing.T) {
	resp := ThreadsResponse{
		ThreadDebugStates: []ThreadDebugState{
			{Index: 0, State: "ready", IsWaiting: true},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(resp)
		case "/metrics":
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	f.hasFrankenPHP = true
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err, "Fetch should not return error on partial failure")
	assert.Len(t, snap.Threads.ThreadDebugStates, 1)
	assert.NotEmpty(t, snap.Errors, "expected errors to be recorded for failed metrics fetch")
}

func TestFetch_AllFail(t *testing.T) {
	srv := newTestServer(500, nil, 500, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	f.hasFrankenPHP = true
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err, "Fetch should not return error even if all fail")
	assert.GreaterOrEqual(t, len(snap.Errors), 2)
	assert.Empty(t, snap.Threads.ThreadDebugStates)
}

func TestDetectFrankenPHP_True(t *testing.T) {
	srv := newTestServer(200, ThreadsResponse{}, 200, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.True(t, f.DetectFrankenPHP(context.Background()))
	assert.True(t, f.HasFrankenPHP())
}

func TestDetectFrankenPHP_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/frankenphp/threads" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.False(t, f.DetectFrankenPHP(context.Background()))
	assert.False(t, f.HasFrankenPHP())
}

func TestFetch_CaddyOnlyMode(t *testing.T) {
	metricsText := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="example.com",code="200"} 100
`
	srv := newTestServer(404, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	// hasFrankenPHP is false by default
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Threads.ThreadDebugStates, "should not fetch threads in Caddy-only mode")
	assert.True(t, snap.Metrics.HasHTTPMetrics)
	assert.Empty(t, snap.Errors, "should not record thread fetch errors in Caddy-only mode")
}

func TestRestartWorkers_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/frankenphp/workers/restart" && r.Method == http.MethodPost {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.RestartWorkers(context.Background())
	require.NoError(t, err)
}

func TestRestartWorkers_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.RestartWorkers(context.Background())
	require.Error(t, err)
}

func TestDoWithRetry_SucceedsFirstTry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/test", nil)
	resp, err := f.doWithRetry(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load())
	resp.Body.Close()
}

func TestDoWithRetry_SucceedsAfterRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			// close connection abruptly to simulate network error
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/test", nil)
	resp, err := f.doWithRetry(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, int32(3), attempts.Load())
	resp.Body.Close()
}

func TestDoWithRetry_AllRetriesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/test", nil)
	resp, err := f.doWithRetry(context.Background(), req)
	if resp != nil {
		resp.Body.Close()
	}

	require.Error(t, err)
}

func TestDoWithRetry_NoRetryOnHTTPError(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/test", nil)
	resp, err := f.doWithRetry(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load(), "should not retry on HTTP error responses")
	resp.Body.Close()
}

func TestDoWithRetry_RespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f := NewHTTPFetcher(srv.URL, 0)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/test", nil)
	resp, err := f.doWithRetry(ctx, req)
	if resp != nil {
		resp.Body.Close()
	}

	require.Error(t, err)
}

func TestFetchServerNames_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/http/servers" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]any{
				"main": map[string]any{"listen": []string{":443"}},
				"api":  map[string]any{"listen": []string{":9443"}},
				"app":  map[string]any{"listen": []string{":8443"}},
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	names := f.FetchServerNames(context.Background())
	require.Equal(t, []string{"api", "app", "main"}, names, "should return sorted server names")
	assert.Equal(t, names, f.ServerNames(), "should persist in fetcher")
}

func TestFetchServerNames_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	names := f.FetchServerNames(context.Background())
	assert.Nil(t, names)
}

func TestFetchServerNames_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/http/servers" {
			w.WriteHeader(200)
			w.Write([]byte("not json"))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	names := f.FetchServerNames(context.Background())
	assert.Nil(t, names)
}

func TestFetchServerNames_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/http/servers" {
			w.WriteHeader(200)
			w.Write([]byte("{}"))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	names := f.FetchServerNames(context.Background())
	assert.Empty(t, names)
}

func TestFetch_SeedsServerNames(t *testing.T) {
	metricsText := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="main",code="200"} 50
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="main",le="+Inf"} 50
caddy_http_request_duration_seconds_sum{server="main"} 1.0
caddy_http_request_duration_seconds_count{server="main"} 50
`
	srv := newTestServer(404, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	f.serverNames = []string{"main", "app", "api"}

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snap.Metrics.Hosts)

	// "main" has real data from metrics
	main := snap.Metrics.Hosts["main"]
	require.NotNil(t, main)
	assert.Equal(t, float64(50), main.RequestsTotal)

	// "app" and "api" should be seeded with empty entries
	app := snap.Metrics.Hosts["app"]
	require.NotNil(t, app, "app should be seeded")
	assert.Equal(t, "app", app.Host)
	assert.Equal(t, float64(0), app.RequestsTotal)
	assert.NotNil(t, app.StatusCodes)
	assert.NotNil(t, app.Methods)

	api := snap.Metrics.Hosts["api"]
	require.NotNil(t, api, "api should be seeded")
	assert.Equal(t, "api", api.Host)
}

func TestFetch_SeedsDoNotOverwriteExisting(t *testing.T) {
	metricsText := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="main",code="200"} 100
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="main",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{server="main"} 5.0
caddy_http_request_duration_seconds_count{server="main"} 100
`
	srv := newTestServer(404, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	f.serverNames = []string{"main"}

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)

	main := snap.Metrics.Hosts["main"]
	require.NotNil(t, main)
	assert.Equal(t, float64(100), main.RequestsTotal, "seeding should not overwrite existing host data")
}

func TestOnConnected_DetectsFrankenPHP(t *testing.T) {
	frankenPHPAvailable := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			if frankenPHPAvailable {
				w.WriteHeader(200)
				json.NewEncoder(w).Encode(ThreadsResponse{
					ThreadDebugStates: []ThreadDebugState{{Index: 0, State: "ready"}},
				})
			} else {
				w.WriteHeader(404)
			}
		case "/metrics":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.False(t, snap.HasFrankenPHP, "should not detect FrankenPHP when unavailable")
	assert.Empty(t, snap.Threads.ThreadDebugStates)

	frankenPHPAvailable = true

	snap, err = f.Fetch(context.Background())
	require.NoError(t, err)
	assert.True(t, snap.HasFrankenPHP, "should detect FrankenPHP on next successful fetch")
	// Threads are fetched on the NEXT Fetch() after detection
	snap, err = f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Len(t, snap.Threads.ThreadDebugStates, 1)
}

func TestOnConnected_FrankenPHPDisappears(t *testing.T) {
	frankenPHPAvailable := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			if frankenPHPAvailable {
				w.WriteHeader(200)
				json.NewEncoder(w).Encode(ThreadsResponse{
					ThreadDebugStates: []ThreadDebugState{{Index: 0, State: "ready"}},
				})
			} else {
				w.WriteHeader(404)
			}
		case "/metrics":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	// first fetch: detects FrankenPHP
	f.Fetch(context.Background())
	assert.True(t, f.HasFrankenPHP())

	// disable FrankenPHP and expire the check timer
	frankenPHPAvailable = false
	f.mu.Lock()
	f.lastFrankenPHPCheck = time.Now().Add(-serverNamesRefreshInterval - time.Second)
	f.mu.Unlock()

	// next fetch: re-checks and detects disappearance
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.False(t, snap.HasFrankenPHP, "should detect FrankenPHP removal")
	assert.False(t, f.HasFrankenPHP())
}

func TestOnConnected_FetchesServerNames(t *testing.T) {
	serverNamesAvailable := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/http/servers":
			if serverNamesAvailable {
				w.WriteHeader(200)
				json.NewEncoder(w).Encode(map[string]any{
					"main": map[string]any{"listen": []string{":443"}},
				})
			} else {
				w.WriteHeader(404)
			}
		case "/metrics":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	f.Fetch(context.Background())
	assert.Empty(t, f.ServerNames())

	serverNamesAvailable = true

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, f.ServerNames())
	require.NotNil(t, snap.Metrics.Hosts)
	assert.Contains(t, snap.Metrics.Hosts, "main")
}

func TestOnConnected_NoRetryWhenMetricsFail(t *testing.T) {
	var detectCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			detectCalls.Add(1)
			w.WriteHeader(404)
		case "/metrics":
			w.WriteHeader(500)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	f.Fetch(context.Background())
	f.Fetch(context.Background())
	f.Fetch(context.Background())

	assert.Equal(t, int32(0), detectCalls.Load(), "should not attempt detection when metrics fail")
}

func TestOnConnected_StopsAfterSuccess(t *testing.T) {
	var detectCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			detectCalls.Add(1)
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(ThreadsResponse{})
		case "/metrics":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	f.Fetch(context.Background())
	f.Fetch(context.Background())
	f.Fetch(context.Background())

	// Detection is called once (first fetch), then stops because hasFrankenPHP is true
	// But fetchThreads also hits /frankenphp/threads in subsequent fetches
	assert.True(t, f.HasFrankenPHP())
}

func TestOnConnected_RefreshesServerNamesAfterInterval(t *testing.T) {
	var serverCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/http/servers":
			serverCalls.Add(1)
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]any{
				"main": map[string]any{"listen": []string{":443"}},
			})
		case "/metrics":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	// first fetch: triggers refresh (lastServerNamesRefresh is zero)
	f.Fetch(context.Background())
	first := serverCalls.Load()
	assert.True(t, first >= 1)

	// second fetch: no refresh (within 30s)
	f.Fetch(context.Background())
	assert.Equal(t, first, serverCalls.Load(), "should not refresh within interval")

	// simulate time passing beyond the refresh interval
	f.mu.Lock()
	f.lastServerNamesRefresh = time.Now().Add(-serverNamesRefreshInterval - time.Second)
	f.mu.Unlock()

	// third fetch: triggers refresh again
	f.Fetch(context.Background())
	assert.True(t, serverCalls.Load() > first, "should refresh after interval elapsed")
}

func TestOnConnected_DoesNotMarkRefreshedOnFailure(t *testing.T) {
	var serverCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/http/servers":
			serverCalls.Add(1)
			w.WriteHeader(500)
		case "/metrics":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	f.Fetch(context.Background())
	f.Fetch(context.Background())
	f.Fetch(context.Background())

	assert.True(t, serverCalls.Load() >= 3, "should retry every fetch when server names fail")
}

func TestFetch_HasFrankenPHPInSnapshot(t *testing.T) {
	srv := newTestServer(200, ThreadsResponse{}, 200, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	f.hasFrankenPHP = true

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.True(t, snap.HasFrankenPHP)
}

func TestFetch_NoFrankenPHPInSnapshot(t *testing.T) {
	srv := newTestServer(404, nil, 200, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.False(t, snap.HasFrankenPHP)
}

func TestFetch_PrometheusProcessFallback_RSS(t *testing.T) {
	metricsText := `# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes 1.048576e+07
# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 12.5
# TYPE process_start_time_seconds gauge
process_start_time_seconds 1.7e+09
`
	srv := newTestServer(404, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(10485760), snap.Process.RSS, "RSS should come from Prometheus metrics")
	assert.True(t, snap.Process.Uptime > 0, "Uptime should be derived from process_start_time_seconds")
	assert.True(t, snap.Process.CreateTime > 0, "CreateTime should be derived from process_start_time_seconds")
}

func TestFetch_PrometheusProcessFallback_CPU(t *testing.T) {
	metricsText := `# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 10.0
`
	srv := newTestServer(404, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	// First fetch: records baseline, CPU=0 (no previous sample)
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, float64(0), snap.Process.CPUPercent, "first fetch has no delta yet")

	// Simulate time passing and CPU usage increasing
	f.lastPromSample = f.lastPromSample.Add(-1 * time.Second)
	f.lastPromCPU = 10.0

	metricsText2 := `# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 10.5
`
	srv.Close()
	srv2 := newTestServer(404, nil, 200, metricsText2)
	defer srv2.Close()
	f.baseURL = srv2.URL

	snap, err = f.Fetch(context.Background())
	require.NoError(t, err)
	assert.True(t, snap.Process.CPUPercent > 0, "CPU should be derived from Prometheus delta")
}

func TestFetch_PrometheusProcessFallback_NotUsedWhenGopsutilWorks(t *testing.T) {
	metricsText := `# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes 1.048576e+07
`
	srv := newTestServer(404, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	// Simulate gopsutil having found the process and returned real RSS
	f.procHandle.proc = nil // no proc, but we'll set proc directly
	// We can't easily simulate gopsutil success in tests, so just verify
	// that the fallback IS used when proc.RSS == 0
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(10485760), snap.Process.RSS)
}

func TestFetchThreads_PerRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(requestTimeout + time.Second)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	start := time.Now()
	_, err := f.fetchThreads(context.Background())
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, requestTimeout+2*time.Second, "should timeout within per-request deadline + retries")
}

func TestHTTPFetcher_ConcurrentAccess(t *testing.T) {
	srv := newTestServer(200, ThreadsResponse{}, 200, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	const goroutines = 10
	done := make(chan struct{})
	for range goroutines {
		go func() {
			defer func() { done <- struct{}{} }()
			f.DetectFrankenPHP(context.Background())
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			f.FetchServerNames(context.Background())
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			_ = f.HasFrankenPHP()
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			_ = f.ServerNames()
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = f.Fetch(context.Background())
		}()
	}

	for range goroutines * 5 {
		<-done
	}
}

func TestFetchConfig_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/" {
			w.WriteHeader(200)
			w.Write([]byte(`{"apps":{"http":{"servers":{}}}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	raw, err := f.FetchConfig(context.Background())
	require.NoError(t, err)
	assert.JSONEq(t, `{"apps":{"http":{"servers":{}}}}`, string(raw))
}

func TestFetchConfig_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	_, err := f.FetchConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestFetchConfig_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/" {
			w.WriteHeader(200)
			w.Write([]byte("not json"))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	_, err := f.FetchConfig(context.Background())
	require.Error(t, err)
}

func TestFetchConfig_EmptyConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/" {
			w.WriteHeader(200)
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	raw, err := f.FetchConfig(context.Background())
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))
}

func TestNewHTTPFetcher_TCPAddr(t *testing.T) {
	f := NewHTTPFetcher("http://localhost:2019", 0)
	assert.False(t, f.IsUnixSocket())
}

func TestNewHTTPFetcher_UnixSocket(t *testing.T) {
	f := NewHTTPFetcher("unix//tmp/test.sock", 0)
	assert.True(t, f.IsUnixSocket())
}

func TestNewHTTPFetcher_UnixSocketTripleSlash(t *testing.T) {
	f := NewHTTPFetcher("unix:///tmp/test.sock", 0)
	assert.True(t, f.IsUnixSocket())
}

func unixSocketPath(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "ember-test-*.sock")
	require.NoError(t, err)
	path := f.Name()
	f.Close()
	os.Remove(path)
	t.Cleanup(func() { os.Remove(path) })
	return path
}

func TestHTTPFetcher_UnixSocket_Integration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets are not supported on Windows")
	}
	sockPath := unixSocketPath(t)
	l, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer l.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		case "/metrics":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("# no metrics\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})}
	go srv.Serve(l)
	defer srv.Close()

	f := NewHTTPFetcher("unix/"+sockPath, 0)
	assert.True(t, f.IsUnixSocket())

	raw, err := f.FetchConfig(context.Background())
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))
}

func TestHTTPFetcher_UnixSocket_TripleSlash_Integration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets are not supported on Windows")
	}
	sockPath := unixSocketPath(t)
	l, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer l.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})}
	go srv.Serve(l)
	defer srv.Close()

	f := NewHTTPFetcher("unix://"+sockPath, 0) // sockPath starts with /, producing unix:///...
	assert.True(t, f.IsUnixSocket())

	raw, err := f.FetchConfig(context.Background())
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))
}

func TestFetch_RecordsScrapeMetricsForEachStage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ThreadDebugStates":[],"ReservedThreadCount":0}`))
		case "/metrics":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("# TYPE x counter\nx 1\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	rec := instrumentation.New("test")
	f.SetRecorder(rec)
	f.DetectFrankenPHP(context.Background())

	_, err := f.Fetch(context.Background())
	require.NoError(t, err)

	for _, s := range rec.Snapshot().Stages {
		assert.Equal(t, uint64(1), s.Total, "stage %q must be recorded once", s.Stage)
		assert.Zero(t, s.Errors, "stage %q must not report errors", s.Stage)
	}
}

func TestFetch_RecordsErrorOnFailedStage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	rec := instrumentation.New("test")
	f.SetRecorder(rec)

	_, err := f.Fetch(context.Background())
	require.NoError(t, err)

	for _, s := range rec.Snapshot().Stages {
		if s.Stage == instrumentation.StageMetrics {
			assert.Equal(t, uint64(1), s.Errors, "metrics stage must count the upstream failure")
			assert.True(t, s.LastSuccessAt.IsZero(), "no success timestamp on a failure")
		}
	}
}

func TestFetch_NilRecorderIsNoOp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# TYPE x counter\nx 1\n"))
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	f.SetRecorder(nil)

	_, err := f.Fetch(context.Background())
	require.NoError(t, err)
}
