package fetcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(threads.ThreadDebugStates) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(threads.ThreadDebugStates))
	}
	if threads.ThreadDebugStates[0].Name != "Worker PHP Thread - /app/worker.php" {
		t.Errorf("unexpected name: %s", threads.ThreadDebugStates[0].Name)
	}
	if threads.ReservedThreadCount != 2 {
		t.Errorf("expected ReservedThreadCount 2, got %d", threads.ReservedThreadCount)
	}
}

func TestFetchThreads_BadStatus(t *testing.T) {
	srv := newTestServer(500, nil, 200, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	_, err := f.fetchThreads(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestFetchMetrics_OK(t *testing.T) {
	metricsText := `# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 5
`
	srv := newTestServer(200, nil, 200, metricsText)
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	metrics, err := f.fetchMetrics(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metrics.BusyThreads != 5 {
		t.Errorf("expected BusyThreads 5, got %v", metrics.BusyThreads)
	}
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
	if err != nil {
		t.Fatalf("Fetch should not return error on partial failure: %v", err)
	}

	if len(snap.Threads.ThreadDebugStates) != 1 {
		t.Errorf("threads should be populated, got %d", len(snap.Threads.ThreadDebugStates))
	}
	if len(snap.Errors) == 0 {
		t.Error("expected errors to be recorded for failed metrics fetch")
	}
}

func TestFetch_AllFail(t *testing.T) {
	srv := newTestServer(500, nil, 500, "")
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	snap, err := f.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch should not return error even if all fail: %v", err)
	}

	if len(snap.Errors) < 2 {
		t.Errorf("expected at least 2 errors, got %d: %v", len(snap.Errors), snap.Errors)
	}
	if len(snap.Threads.ThreadDebugStates) != 0 {
		t.Error("threads should be empty on failure")
	}
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestartWorkers_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.RestartWorkers(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}
