package fetcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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
	_, err := f.doWithRetry(context.Background(), req)

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
	_, err := f.doWithRetry(ctx, req)

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
