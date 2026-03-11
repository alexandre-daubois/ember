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
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err, "Fetch should not return error on partial failure")
	assert.Len(t, snap.Threads.ThreadDebugStates, 1)
	assert.NotEmpty(t, snap.Errors, "expected errors to be recorded for failed metrics fetch")
}

func TestFetch_AllFail(t *testing.T) {
	srv := newTestServer(500, nil, 500, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err, "Fetch should not return error even if all fail")
	assert.GreaterOrEqual(t, len(snap.Errors), 2)
	assert.Empty(t, snap.Threads.ThreadDebugStates)
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
