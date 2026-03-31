package exporter

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/pkg/plugin"
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
	Handler(holder, "", nil)(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec
}

func getWithPrefix(holder *StateHolder, prefix string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	Handler(holder, prefix, nil)(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
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
	s.Derived.P90 = 30.0
	s.Derived.P95 = 45.0
	s.Derived.P99 = 120.3

	holder := &StateHolder{}
	holder.Store(s)

	body := get(holder).Body.String()
	assert.Contains(t, body, `frankenphp_request_duration_milliseconds{quantile="0.5"} 12.50`)
	assert.Contains(t, body, `frankenphp_request_duration_milliseconds{quantile="0.9"} 30.00`)
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

	assert.Contains(t, body1, `/a.php`)
	assert.Contains(t, body1, `/m.php`)
	assert.Contains(t, body1, `/z.php`)
}

func stateWithHosts(hosts []model.HostDerived) model.State {
	s := stateWithThreads(nil, nil)
	s.HostDerived = hosts
	return s
}

func TestHandler_ThreadMetrics_NegativeOtherClampedToZero(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsBusy: true},
	}
	s := stateWithThreads(threads, nil)
	s.Derived.TotalBusy = 3
	s.Derived.TotalIdle = 2

	holder := &StateHolder{}
	holder.Store(s)

	body := get(holder).Body.String()
	assert.Contains(t, body, `frankenphp_threads_total{state="other"} 0`)
}

func TestStatusClassRates_AllClasses(t *testing.T) {
	codes := map[int]float64{200: 10, 301: 5, 404: 3, 502: 2}
	classes := statusClassRates(codes)

	assert.Equal(t, 10.0, classes["2xx"])
	assert.Equal(t, 5.0, classes["3xx"])
	assert.Equal(t, 3.0, classes["4xx"])
	assert.Equal(t, 2.0, classes["5xx"])
}

func TestStatusClassRates_Empty(t *testing.T) {
	assert.Nil(t, statusClassRates(nil))
	assert.Nil(t, statusClassRates(map[int]float64{}))
}

func TestHandler_HostMetrics(t *testing.T) {
	hosts := []model.HostDerived{
		{
			Host:     "api.example.com",
			RPS:      150.5,
			AvgTime:  23.4,
			InFlight: 5,
			P50:      10, P90: 30, P95: 50, P99: 120,
			HasPercentiles: true,
			StatusCodes:    map[int]float64{200: 140, 204: 5, 404: 3, 500: 2.5},
		},
		{
			Host:     "web.example.com",
			RPS:      50,
			AvgTime:  100,
			InFlight: 2,
		},
	}
	holder := &StateHolder{}
	holder.Store(stateWithHosts(hosts))

	body := get(holder).Body.String()

	assert.Contains(t, body, `ember_host_rps{host="api.example.com"} 150.50`)
	assert.Contains(t, body, `ember_host_rps{host="web.example.com"} 50.00`)

	assert.Contains(t, body, `ember_host_latency_avg_milliseconds{host="api.example.com"} 23.40`)
	assert.Contains(t, body, `ember_host_latency_avg_milliseconds{host="web.example.com"} 100.00`)

	assert.Contains(t, body, `ember_host_latency_milliseconds{host="api.example.com",quantile="0.5"} 10.00`)
	assert.Contains(t, body, `ember_host_latency_milliseconds{host="api.example.com",quantile="0.9"} 30.00`)
	assert.Contains(t, body, `ember_host_latency_milliseconds{host="api.example.com",quantile="0.95"} 50.00`)
	assert.Contains(t, body, `ember_host_latency_milliseconds{host="api.example.com",quantile="0.99"} 120.00`)
	assert.NotContains(t, body, `ember_host_latency_milliseconds{host="web.example.com"`)

	assert.Contains(t, body, `ember_host_inflight{host="api.example.com"} 5`)
	assert.Contains(t, body, `ember_host_inflight{host="web.example.com"} 2`)

	assert.Contains(t, body, `ember_host_status_rate{host="api.example.com",class="2xx"} 145.00`)
	assert.Contains(t, body, `ember_host_status_rate{host="api.example.com",class="4xx"} 3.00`)
	assert.Contains(t, body, `ember_host_status_rate{host="api.example.com",class="5xx"} 2.50`)
	assert.NotContains(t, body, `ember_host_status_rate{host="web.example.com"`)
}

func TestHandler_HostMetrics_SkippedWhenNoHosts(t *testing.T) {
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, nil))

	body := get(holder).Body.String()
	assert.NotContains(t, body, "ember_host_rps")
	assert.NotContains(t, body, "ember_host_latency")
	assert.NotContains(t, body, "ember_host_inflight")
	assert.NotContains(t, body, "ember_host_status_rate")
}

func TestHandler_HostMetrics_SortedDeterministic(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "z.example.com", RPS: 1},
		{Host: "a.example.com", RPS: 2},
		{Host: "m.example.com", RPS: 3},
	}
	holder := &StateHolder{}
	holder.Store(stateWithHosts(hosts))

	body1 := get(holder).Body.String()
	body2 := get(holder).Body.String()
	require.Equal(t, body1, body2, "output should be deterministic")
}

func TestHandler_HostMetrics_ValidPrometheus(t *testing.T) {
	hosts := []model.HostDerived{
		{
			Host:     "api.example.com",
			RPS:      100,
			AvgTime:  25,
			InFlight: 3,
			P50:      10, P90: 30, P95: 50, P99: 120,
			HasPercentiles: true,
			StatusCodes:    map[int]float64{200: 90, 404: 5, 500: 5},
		},
	}
	holder := &StateHolder{}
	holder.Store(stateWithHosts(hosts))

	rec := get(holder)
	require.Equal(t, http.StatusOK, rec.Code)

	parser := expfmt.NewTextParser(prommodel.UTF8Validation)
	families, err := parser.TextToMetricFamilies(rec.Body)
	require.NoError(t, err, "output must be valid Prometheus text format")

	assert.Contains(t, families, "ember_host_rps")
	assert.Contains(t, families, "ember_host_latency_avg_milliseconds")
	assert.Contains(t, families, "ember_host_latency_milliseconds")
	assert.Contains(t, families, "ember_host_inflight")
	assert.Contains(t, families, "ember_host_status_rate")
}

func TestPrefixed(t *testing.T) {
	assert.Equal(t, "frankenphp_threads_total", prefixed("", "frankenphp_threads_total"))
	assert.Equal(t, "prod_frankenphp_threads_total", prefixed("prod", "frankenphp_threads_total"))
	assert.Equal(t, "myapp_ember_host_rps", prefixed("myapp", "ember_host_rps"))
}

func TestHandler_WithPrefix_AllMetricsPrefixed(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsBusy: true, MemoryUsage: 10 * 1024 * 1024},
		{Index: 1, IsWaiting: true},
	}
	workers := map[string]*fetcher.WorkerMetrics{
		"/app/worker.php": {Crashes: 2, Restarts: 5, QueueDepth: 1, RequestCount: 10000},
	}
	s := stateWithThreads(threads, workers)
	s.Derived.HasPercentiles = true
	s.Derived.P50 = 12.5
	s.Derived.P95 = 45.0
	s.Derived.P99 = 120.3
	s.HostDerived = []model.HostDerived{
		{Host: "test.com", RPS: 100, AvgTime: 25, InFlight: 3,
			HasPercentiles: true, P50: 10, P90: 30, P95: 50, P99: 120,
			StatusCodes: map[int]float64{200: 90, 500: 5}},
	}

	holder := &StateHolder{}
	holder.Store(s)

	rec := getWithPrefix(holder, "prod")
	require.Equal(t, http.StatusOK, rec.Code)

	parser := expfmt.NewTextParser(prommodel.UTF8Validation)
	families, err := parser.TextToMetricFamilies(rec.Body)
	require.NoError(t, err, "output must be valid Prometheus text format")

	assert.Contains(t, families, "prod_frankenphp_threads_total")
	assert.Contains(t, families, "prod_frankenphp_thread_memory_bytes")
	assert.Contains(t, families, "prod_frankenphp_worker_crashes_total")
	assert.Contains(t, families, "prod_frankenphp_request_duration_milliseconds")
	assert.Contains(t, families, "prod_process_cpu_percent")
	assert.Contains(t, families, "prod_process_rss_bytes")
	assert.Contains(t, families, "prod_ember_host_rps")
	assert.Contains(t, families, "prod_ember_host_latency_avg_milliseconds")
	assert.Contains(t, families, "prod_ember_host_latency_milliseconds")
	assert.Contains(t, families, "prod_ember_host_inflight")
	assert.Contains(t, families, "prod_ember_host_status_rate")

	// Original names must NOT be present
	assert.NotContains(t, families, "frankenphp_threads_total")
	assert.NotContains(t, families, "ember_host_rps")
	assert.NotContains(t, families, "process_cpu_percent")
}

func TestHandler_ErrorMetrics(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "api.example.com", ErrorRate: 3.5},
		{Host: "web.example.com", ErrorRate: 0},
		{Host: "cdn.example.com", ErrorRate: 1.2},
	}
	holder := &StateHolder{}
	holder.Store(stateWithHosts(hosts))

	body := get(holder).Body.String()

	assert.Contains(t, body, "# HELP ember_host_error_rate")
	assert.Contains(t, body, "# TYPE ember_host_error_rate gauge")
	assert.Contains(t, body, `ember_host_error_rate{host="api.example.com"} 3.50`)
	assert.Contains(t, body, `ember_host_error_rate{host="cdn.example.com"} 1.20`)
	assert.NotContains(t, body, `ember_host_error_rate{host="web.example.com"}`)
}

func TestHandler_ErrorMetrics_SkippedWhenAllZero(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "api.example.com", ErrorRate: 0},
		{Host: "web.example.com", ErrorRate: 0},
	}
	holder := &StateHolder{}
	holder.Store(stateWithHosts(hosts))

	body := get(holder).Body.String()
	assert.NotContains(t, body, "ember_host_error_rate")
}

func TestHandler_ErrorMetrics_ValidPrometheus(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "api.example.com", ErrorRate: 5.0},
	}
	holder := &StateHolder{}
	holder.Store(stateWithHosts(hosts))

	rec := get(holder)
	require.Equal(t, http.StatusOK, rec.Code)

	parser := expfmt.NewTextParser(prommodel.UTF8Validation)
	families, err := parser.TextToMetricFamilies(rec.Body)
	require.NoError(t, err, "output must be valid Prometheus text format")
	assert.Contains(t, families, "ember_host_error_rate")
}

func TestHandler_EmptyPrefix_DefaultNames(t *testing.T) {
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, nil))

	rec := getWithPrefix(holder, "")
	body := rec.Body.String()

	assert.Contains(t, body, "frankenphp_threads_total")
	assert.Contains(t, body, "process_cpu_percent")
	assert.NotContains(t, body, "_frankenphp_threads_total")
}

func healthz(holder *StateHolder, interval time.Duration) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	HealthHandler(holder, interval)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	return rec
}

func TestHealthHandler_NoData(t *testing.T) {
	holder := &StateHolder{}
	rec := healthz(holder, time.Second)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "no data yet", body["status"])
}

func TestHealthHandler_FreshData(t *testing.T) {
	snap := &fetcher.Snapshot{
		FetchedAt: time.Now(),
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	var s model.State
	s.Update(snap)

	holder := &StateHolder{}
	holder.Store(s)

	rec := healthz(holder, time.Second)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Contains(t, body, "last_fetch")
	assert.Contains(t, body, "age_seconds")
}

func TestHealthHandler_StaleData(t *testing.T) {
	snap := &fetcher.Snapshot{
		FetchedAt: time.Now().Add(-30 * time.Second),
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	var s model.State
	s.Update(snap)

	holder := &StateHolder{}
	holder.Store(s)

	rec := healthz(holder, time.Second)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "stale", body["status"])
	assert.Contains(t, body, "age_seconds")
}

func TestHealthHandler_StaleThresholdFloor(t *testing.T) {
	snap := &fetcher.Snapshot{
		FetchedAt: time.Now().Add(-4 * time.Second),
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	var s model.State
	s.Update(snap)

	holder := &StateHolder{}
	holder.Store(s)

	// interval=500ms -> 3x = 1.5s, but floor is 5s, so 4s-old data should be OK
	rec := healthz(holder, 500*time.Millisecond)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHealthHandler_ContentType(t *testing.T) {
	holder := &StateHolder{}
	rec := healthz(holder, time.Second)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestBasicAuth_ValidCredentials(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := BasicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBasicAuth_InvalidPassword(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := BasicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Basic")
}

func TestBasicAuth_NoCredentials(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := BasicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

type testExporter struct{}

func (e *testExporter) WriteMetrics(w io.Writer, _ any, _ string) {
	_, _ = io.WriteString(w, "test_plugin_metric 42\n")
}

type panicExporter struct{}

func (e *panicExporter) WriteMetrics(_ io.Writer, _ any, _ string) {
	panic("exporter boom")
}

func TestHandler_PluginMetrics(t *testing.T) {
	holder := &StateHolder{}
	holder.StoreAll(stateWithThreads(nil, nil), []plugin.PluginExport{
		{Exporter: &testExporter{}, Data: "data"},
	})

	rec := get(holder)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "test_plugin_metric 42")
}

func TestHandler_PluginWriteMetricsPanic(t *testing.T) {
	holder := &StateHolder{}
	holder.StoreAll(stateWithThreads(nil, nil), []plugin.PluginExport{
		{Exporter: &panicExporter{}, Data: "data"},
	})

	rec := get(holder)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "plugin WriteMetrics panic")
	assert.Contains(t, rec.Body.String(), "exporter boom")
}

func TestHandler_PluginPanicDoesNotBreakOtherMetrics(t *testing.T) {
	holder := &StateHolder{}
	holder.StoreAll(stateWithThreads(nil, nil), []plugin.PluginExport{
		{Exporter: &panicExporter{}, Data: "data"},
	})

	rec := get(holder)
	assert.Contains(t, rec.Body.String(), "process_cpu_percent")
}

func TestBasicAuth_InvalidUser(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := BasicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("wrong", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_WithRecorder_GoldenPath(t *testing.T) {
	holder := &StateHolder{}
	holder.Store(stateWithThreads(nil, nil))

	rec := instrumentation.New("v")
	rec.Record(instrumentation.StageMetrics, 12*time.Millisecond, nil)
	rec.Record(instrumentation.StageMetrics, 8*time.Millisecond, errors.New("boom"))
	rec.Record(instrumentation.StageThreads, 3*time.Millisecond, nil)

	w := httptest.NewRecorder()
	Handler(holder, "myapp", rec)(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `myapp_ember_build_info{version="v"`, "build_info must carry the prefix")
	assert.Contains(t, body, `myapp_ember_scrape_total{stage="metrics"} 2`)
	assert.Contains(t, body, `myapp_ember_scrape_errors_total{stage="metrics"} 1`)
	assert.Contains(t, body, `myapp_ember_scrape_total{stage="threads"} 1`)
	assert.Contains(t, body, `myapp_ember_scrape_duration_seconds{stage="threads"} 0.003`)
	assert.Regexp(t, `myapp_ember_last_successful_scrape_timestamp_seconds\{stage="metrics"\} \d+\.\d+`, body)
	assert.Regexp(t, `myapp_ember_last_successful_scrape_timestamp_seconds\{stage="process"\} 0\.000`, body)

	parser := expfmt.NewTextParser(prommodel.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(body))
	require.NoError(t, err, "self-metrics output must be valid Prometheus text format")
	assert.Contains(t, families, "myapp_ember_build_info")
	assert.Contains(t, families, "myapp_ember_scrape_total")
}
