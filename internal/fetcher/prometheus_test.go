package fetcher

import (
	"strings"
	"testing"
)

const sampleMetrics = `# HELP frankenphp_total_threads Total number of PHP threads
# TYPE frankenphp_total_threads counter
frankenphp_total_threads 20
# HELP frankenphp_busy_threads Number of busy PHP threads
# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 4
# HELP frankenphp_queue_depth Number of regular requests in queue
# TYPE frankenphp_queue_depth gauge
frankenphp_queue_depth 2
# HELP frankenphp_total_workers Total active workers per script
# TYPE frankenphp_total_workers gauge
frankenphp_total_workers{worker="/app/worker.php"} 8
frankenphp_total_workers{worker="/app/api.php"} 4
# HELP frankenphp_busy_workers Busy workers per script
# TYPE frankenphp_busy_workers gauge
frankenphp_busy_workers{worker="/app/worker.php"} 3
frankenphp_busy_workers{worker="/app/api.php"} 1
# HELP frankenphp_ready_workers Ready workers per script
# TYPE frankenphp_ready_workers gauge
frankenphp_ready_workers{worker="/app/worker.php"} 5
frankenphp_ready_workers{worker="/app/api.php"} 3
# HELP frankenphp_worker_request_time Cumulative request processing time
# TYPE frankenphp_worker_request_time counter
frankenphp_worker_request_time{worker="/app/worker.php"} 125.5
frankenphp_worker_request_time{worker="/app/api.php"} 42.3
# HELP frankenphp_worker_request_count Total requests processed
# TYPE frankenphp_worker_request_count counter
frankenphp_worker_request_count{worker="/app/worker.php"} 10000
frankenphp_worker_request_count{worker="/app/api.php"} 3000
# HELP frankenphp_worker_crashes Worker crash count
# TYPE frankenphp_worker_crashes counter
frankenphp_worker_crashes{worker="/app/worker.php"} 2
frankenphp_worker_crashes{worker="/app/api.php"} 0
# HELP frankenphp_worker_restarts Worker voluntary restart count
# TYPE frankenphp_worker_restarts counter
frankenphp_worker_restarts{worker="/app/worker.php"} 5
frankenphp_worker_restarts{worker="/app/api.php"} 1
# HELP frankenphp_worker_queue_depth Requests in queue per worker
# TYPE frankenphp_worker_queue_depth gauge
frankenphp_worker_queue_depth{worker="/app/worker.php"} 1
frankenphp_worker_queue_depth{worker="/app/api.php"} 0
`

func TestParsePrometheusMetrics_GlobalMetrics(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(sampleMetrics))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertFloat(t, "TotalThreads", 20, snap.TotalThreads)
	assertFloat(t, "BusyThreads", 4, snap.BusyThreads)
	assertFloat(t, "QueueDepth", 2, snap.QueueDepth)
}

func TestParsePrometheusMetrics_WorkerMetrics(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(sampleMetrics))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(snap.Workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(snap.Workers))
	}

	w := snap.Workers["/app/worker.php"]
	if w == nil {
		t.Fatal("missing worker /app/worker.php")
	}

	assertFloat(t, "Total", 8, w.Total)
	assertFloat(t, "Busy", 3, w.Busy)
	assertFloat(t, "Ready", 5, w.Ready)
	assertFloat(t, "RequestTime", 125.5, w.RequestTime)
	assertFloat(t, "RequestCount", 10000, w.RequestCount)
	assertFloat(t, "Crashes", 2, w.Crashes)
	assertFloat(t, "Restarts", 5, w.Restarts)
	assertFloat(t, "QueueDepth", 1, w.QueueDepth)

	api := snap.Workers["/app/api.php"]
	if api == nil {
		t.Fatal("missing worker /app/api.php")
	}
	assertFloat(t, "api.RequestCount", 3000, api.RequestCount)
	assertFloat(t, "api.Crashes", 0, api.Crashes)
}

func TestParsePrometheusMetrics_Empty(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertFloat(t, "TotalThreads", 0, snap.TotalThreads)
	if len(snap.Workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(snap.Workers))
	}
}

func TestParsePrometheusMetrics_PartialData(t *testing.T) {
	partial := `# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 7
`
	snap, err := parsePrometheusMetrics(strings.NewReader(partial))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertFloat(t, "BusyThreads", 7, snap.BusyThreads)
	assertFloat(t, "TotalThreads", 0, snap.TotalThreads)
}

func assertFloat(t *testing.T, name string, expected, got float64) {
	t.Helper()
	if got != expected {
		t.Errorf("%s: expected %v, got %v", name, expected, got)
	}
}
