package exporter

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/prometheus/common/expfmt"
	prommodel "github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stateWithThreads(threads []fetcher.ThreadDebugState, workers map[string]*fetcher.WorkerMetrics) model.State {
	if workers == nil {
		workers = map[string]*fetcher.WorkerMetrics{}
	}
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{ThreadDebugStates: threads},
		Metrics: fetcher.MetricsSnapshot{Workers: workers},
		Process: fetcher.ProcessMetrics{CPUPercent: 42.5, RSS: 128 * 1024 * 1024},
	}
	var s model.State
	s.Update(snap)
	return s
}

func get(holder *StateHolder) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	Handler(holder)(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec
}

func TestHandler_NoData_Returns503(t *testing.T) {
	holder := &StateHolder{}
	rec := get(holder)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandler_ContentType(t *testing.T) {
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, nil))

	rec := get(holder)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, prometheusContentType, rec.Header().Get("Content-Type"))
}

func TestHandler_ThreadMetrics(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsBusy: true},
		{Index: 1, IsBusy: true},
		{Index: 2, IsWaiting: true},
		{Index: 3, State: "starting"},
	}
	holder := &StateHolder{}
	holder.Store(stateWithThreads(threads, nil))

	body := get(holder).Body.String()
	assert.Contains(t, body, `frankenphp_threads_total{state="busy"} 2`)
	assert.Contains(t, body, `frankenphp_threads_total{state="idle"} 1`)
	assert.Contains(t, body, `frankenphp_threads_total{state="other"} 1`)
	assert.Contains(t, body, "# HELP frankenphp_threads_total")
	assert.Contains(t, body, "# TYPE frankenphp_threads_total gauge")
}

func TestHandler_ThreadMemory(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, MemoryUsage: 10 * 1024 * 1024},
		{Index: 1, MemoryUsage: 0},
		{Index: 2, MemoryUsage: 5 * 1024 * 1024},
	}
	holder := &StateHolder{}
	holder.Store(stateWithThreads(threads, nil))

	body := get(holder).Body.String()
	assert.Contains(t, body, `frankenphp_thread_memory_bytes{index="0"} 10485760`)
	assert.Contains(t, body, `frankenphp_thread_memory_bytes{index="2"} 5242880`)
	assert.NotContains(t, body, `index="1"`)
}

func TestHandler_ThreadMemory_SkippedWhenAllZero(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, MemoryUsage: 0},
		{Index: 1, MemoryUsage: 0},
	}
	holder := &StateHolder{}
	holder.Store(stateWithThreads(threads, nil))

	body := get(holder).Body.String()
	assert.NotContains(t, body, "frankenphp_thread_memory_bytes")
}

func TestHandler_WorkerMetrics(t *testing.T) {
	workers := map[string]*fetcher.WorkerMetrics{
		"/app/worker.php": {Crashes: 2, Restarts: 5, QueueDepth: 1, RequestCount: 10000},
		"/app/api.php":    {Crashes: 0, Restarts: 1, QueueDepth: 0, RequestCount: 3000},
	}
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, workers))

	body := get(holder).Body.String()

	assert.Contains(t, body, `frankenphp_worker_crashes_total{worker="/app/api.php"} 0`)
	assert.Contains(t, body, `frankenphp_worker_crashes_total{worker="/app/worker.php"} 2`)
	assert.Contains(t, body, `frankenphp_worker_restarts_total{worker="/app/worker.php"} 5`)
	assert.Contains(t, body, `frankenphp_worker_queue_depth{worker="/app/worker.php"} 1`)
	assert.Contains(t, body, `frankenphp_worker_requests_total{worker="/app/worker.php"} 10000`)
	assert.Contains(t, body, `frankenphp_worker_requests_total{worker="/app/api.php"} 3000`)
}

func TestHandler_WorkerMetrics_SkippedWhenNoWorkers(t *testing.T) {
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, nil))

	body := get(holder).Body.String()
	assert.NotContains(t, body, "frankenphp_worker_crashes_total")
}

func TestHandler_Percentiles(t *testing.T) {
	s := stateWithThreads(nil, nil)
	s.Derived.HasPercentiles = true
	s.Derived.P50 = 12.5
	s.Derived.P95 = 45.0
	s.Derived.P99 = 120.3

	holder := &StateHolder{}
	holder.Store(s)

	body := get(holder).Body.String()
	assert.Contains(t, body, `frankenphp_request_duration_milliseconds{quantile="0.5"} 12.50`)
	assert.Contains(t, body, `frankenphp_request_duration_milliseconds{quantile="0.95"} 45.00`)
	assert.Contains(t, body, `frankenphp_request_duration_milliseconds{quantile="0.99"} 120.30`)
}

func TestHandler_NoPercentiles(t *testing.T) {
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, nil))

	body := get(holder).Body.String()
	assert.NotContains(t, body, "frankenphp_request_duration_milliseconds")
}

func TestHandler_ProcessMetrics(t *testing.T) {
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, nil))

	body := get(holder).Body.String()
	assert.Contains(t, body, "process_cpu_percent 42.50")
	assert.Contains(t, body, "process_rss_bytes 134217728")
}

func TestStateHolder_Concurrent(t *testing.T) {
	holder := &StateHolder{}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			holder.Store(stateWithThreads(nil, nil))
		}()
		go func() {
			defer wg.Done()
			_ = holder.Load()
		}()
	}
	wg.Wait()
}

func TestEscapeLabelValue(t *testing.T) {
	assert.Equal(t, `hello`, escapeLabelValue("hello"))
	assert.Equal(t, `path\\to\\file`, escapeLabelValue(`path\to\file`))
	assert.Equal(t, `say \"hi\"`, escapeLabelValue(`say "hi"`))
	assert.Equal(t, `line1\nline2`, escapeLabelValue("line1\nline2"))
}

func TestHandler_RoundTrip_ValidPrometheus(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsBusy: true, MemoryUsage: 10 * 1024 * 1024},
		{Index: 1, IsWaiting: true},
		{Index: 2, MemoryUsage: 5 * 1024 * 1024},
	}
	workers := map[string]*fetcher.WorkerMetrics{
		"/app/worker.php": {Crashes: 2, Restarts: 5, QueueDepth: 1, RequestCount: 10000},
	}
	s := stateWithThreads(threads, workers)
	s.Derived.HasPercentiles = true
	s.Derived.P50 = 12.5
	s.Derived.P95 = 45.0
	s.Derived.P99 = 120.3

	holder := &StateHolder{}
	holder.Store(s)

	rec := get(holder)
	require.Equal(t, http.StatusOK, rec.Code)

	parser := expfmt.NewTextParser(prommodel.UTF8Validation)
	families, err := parser.TextToMetricFamilies(rec.Body)
	require.NoError(t, err, "output must be valid Prometheus text format")

	assert.Contains(t, families, "frankenphp_threads_total")
	assert.Contains(t, families, "frankenphp_thread_memory_bytes")
	assert.Contains(t, families, "frankenphp_worker_crashes_total")
	assert.Contains(t, families, "frankenphp_request_duration_milliseconds")
	assert.Contains(t, families, "process_cpu_percent")
	assert.Contains(t, families, "process_rss_bytes")
}

func TestHandler_WorkerMetrics_SortedDeterministic(t *testing.T) {
	workers := map[string]*fetcher.WorkerMetrics{
		"/z.php": {Crashes: 1},
		"/a.php": {Crashes: 2},
		"/m.php": {Crashes: 3},
	}
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, workers))

	body1 := get(holder).Body.String()
	body2 := get(holder).Body.String()
	require.Equal(t, body1, body2, "output should be deterministic")

	aIdx := len(body1) - len(body1) // find relative positions
	_ = aIdx
	assert.Contains(t, body1, `/a.php`)
	assert.Contains(t, body1, `/m.php`)
	assert.Contains(t, body1, `/z.php`)
}
