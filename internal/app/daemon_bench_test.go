package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
)

func realisticPrometheusMetrics(nHosts int) string {
	base := `# HELP frankenphp_total_threads Total number of PHP threads
# TYPE frankenphp_total_threads counter
frankenphp_total_threads 20
# HELP frankenphp_busy_threads Number of busy PHP threads
# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 8
# HELP frankenphp_queue_depth Number of requests in queue
# TYPE frankenphp_queue_depth gauge
frankenphp_queue_depth 2
# HELP frankenphp_total_workers Total active workers
# TYPE frankenphp_total_workers gauge
frankenphp_total_workers{worker="/app/worker.php"} 10
# HELP frankenphp_busy_workers Busy workers
# TYPE frankenphp_busy_workers gauge
frankenphp_busy_workers{worker="/app/worker.php"} 4
# HELP frankenphp_ready_workers Ready workers
# TYPE frankenphp_ready_workers gauge
frankenphp_ready_workers{worker="/app/worker.php"} 6
# HELP frankenphp_worker_request_time Cumulative request time
# TYPE frankenphp_worker_request_time counter
frankenphp_worker_request_time{worker="/app/worker.php"} 500.5
# HELP frankenphp_worker_request_count Total requests processed
# TYPE frankenphp_worker_request_count counter
frankenphp_worker_request_count{worker="/app/worker.php"} 50000
# HELP frankenphp_worker_crashes Worker crash count
# TYPE frankenphp_worker_crashes counter
frankenphp_worker_crashes{worker="/app/worker.php"} 1
# HELP frankenphp_worker_restarts Worker restart count
# TYPE frankenphp_worker_restarts counter
frankenphp_worker_restarts{worker="/app/worker.php"} 3
# HELP process_cpu_seconds_total Total user and system CPU time
# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 42.5
# HELP process_resident_memory_bytes Resident memory size
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes 52428800
`
	for i := range nHosts {
		host := fmt.Sprintf("host-%d.example.com", i)
		base += fmt.Sprintf(`caddy_http_requests_total{host="%s",code="200"} %d
caddy_http_requests_total{host="%s",code="404"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="0.005"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="0.01"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="0.025"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="0.05"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="0.1"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="0.25"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="0.5"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="1"} %d
caddy_http_request_duration_seconds_bucket{host="%s",le="+Inf"} %d
caddy_http_request_duration_seconds_sum{host="%s"} %.1f
caddy_http_request_duration_seconds_count{host="%s"} %d
caddy_http_requests_in_flight{host="%s"} %d
caddy_http_request_errors_total{host="%s"} %d
`,
			host, 10000+i*500,
			host, 50+i*5,
			host, 1000+i*100,
			host, 3000+i*200,
			host, 5000+i*300,
			host, 7000+i*400,
			host, 8000+i*400,
			host, 9000+i*400,
			host, 9500+i*400,
			host, 9800+i*400,
			host, 10050+i*505,
			host, float64(50+i*5),
			host, 10050+i*505,
			host, i%5,
			host, i*2,
		)
	}

	if nHosts > 0 {
		base = `# HELP caddy_http_requests_total Total HTTP requests
# TYPE caddy_http_requests_total counter
# HELP caddy_http_request_duration_seconds Histogram of request durations
# TYPE caddy_http_request_duration_seconds histogram
# HELP caddy_http_requests_in_flight Active HTTP requests
# TYPE caddy_http_requests_in_flight gauge
# HELP caddy_http_request_errors_total HTTP request errors
# TYPE caddy_http_request_errors_total counter
` + base
	}

	return base
}

func realisticThreadsResponse(n int) fetcher.ThreadsResponse {
	threads := make([]fetcher.ThreadDebugState, n)
	for i := range threads {
		threads[i] = fetcher.ThreadDebugState{
			Index:     i,
			Name:      "Worker PHP Thread - /app/worker.php",
			IsBusy:    i%3 == 0,
			IsWaiting: i%3 != 0,
			State:     "ready",
		}
		if threads[i].IsBusy {
			threads[i].CurrentMethod = "GET"
			threads[i].CurrentURI = "/api/v2/users"
			threads[i].MemoryUsage = int64(i+1) * 1024 * 1024
			threads[i].RequestCount = int64(i * 100)
		}
	}
	return fetcher.ThreadsResponse{ThreadDebugStates: threads, ReservedThreadCount: 2}
}

func benchDaemonServer(nThreads, nHosts int) *httptest.Server {
	threadsResp := realisticThreadsResponse(nThreads)
	metricsBody := realisticPrometheusMetrics(nHosts)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(threadsResp)
		case "/metrics":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(metricsBody))
		case "/config/apps/http/servers":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// BenchmarkDaemonPollCycle benchmarks the full daemon poll loop:
// HTTP fetch + Prometheus parse + State.Update + CopyForExport.
func BenchmarkDaemonPollCycle(b *testing.B) {
	srv := benchDaemonServer(100, 10)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	f.DetectFrankenPHP(context.Background())

	holder := &exporter.StateHolder{}
	var state model.State

	// warm up
	snap, _ := f.Fetch(context.Background())
	state.Update(snap)
	holder.Store(state.CopyForExport())

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		snap, _ := f.Fetch(context.Background())
		state.Update(snap)
		holder.Store(state.CopyForExport())
	}
}

// TestDaemonMemoryFootprint measures the real process memory after
// simulating 5 minutes of daemon operation (300 poll cycles).
// This is not a benchmark but a test that reports memory metrics.
func TestDaemonMemoryFootprint(t *testing.T) {
	srv := benchDaemonServer(100, 10)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	f.DetectFrankenPHP(context.Background())

	holder := &exporter.StateHolder{}
	var state model.State

	// stabilize GC
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	// simulate 300 poll cycles (~5 min at 1s interval)
	for range 300 {
		snap, err := f.Fetch(context.Background())
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		state.Update(snap)
		holder.Store(state.CopyForExport())
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	heapInuse := after.HeapInuse
	sys := after.Sys
	heapGrowth := after.HeapInuse - before.HeapInuse

	t.Logf("After 300 poll cycles (100 threads, 10 hosts):")
	t.Logf("  Sys (total from OS):  %d KB", sys/1024)
	t.Logf("  HeapInuse:            %d KB", heapInuse/1024)
	t.Logf("  Heap growth:          %d KB", heapGrowth/1024)
	t.Logf("  HeapObjects:          %d", after.HeapObjects)
	t.Logf("  NumGC:                %d", after.NumGC)
}
