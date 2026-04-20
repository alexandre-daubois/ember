package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freshHolder() *exporter.StateHolder {
	holder := &exporter.StateHolder{}
	var st model.State
	st.Update(&fetcher.Snapshot{
		Process: fetcher.ProcessMetrics{CPUPercent: 1.0, RSS: 1024},
	})
	holder.Store(st.CopyForExport())
	return holder
}

func TestNewMetricsHandler_ServesMetricsAndHealth(t *testing.T) {
	holder := freshHolder()

	cfg := &config{interval: time.Second}
	handler := newMetricsHandler(holder, cfg)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	// healthz can be 200 or 503 depending on freshness; either way it must respond
	assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, rec.Code)
}

func TestNewMetricsHandler_BasicAuth_RejectsUnauthenticated(t *testing.T) {
	holder := &exporter.StateHolder{}
	cfg := &config{
		interval:    time.Second,
		metricsAuth: "admin:secret",
	}
	handler := newMetricsHandler(holder, cfg)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"basic auth must reject unauthenticated requests")
}

func TestNewMetricsHandler_BasicAuth_AcceptsCorrectCredentials(t *testing.T) {
	holder := freshHolder()
	cfg := &config{
		interval:    time.Second,
		metricsAuth: "admin:secret",
	}
	handler := newMetricsHandler(holder, cfg)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewMetricsHandler_PrefixIsAppliedToMetricNames(t *testing.T) {
	holder := freshHolder()
	cfg := &config{
		interval:      time.Second,
		metricsPrefix: "ember_test",
	}
	handler := newMetricsHandler(holder, cfg)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "ember_test_process_cpu_percent",
		"prefix must propagate into the rendered metric names")
}

func TestNewMetricsHandler_RecorderIsExposed(t *testing.T) {
	holder := freshHolder()

	rec := instrumentation.New("test")
	rec.Record(instrumentation.StageMetrics, 50*time.Millisecond, nil)

	cfg := &config{
		interval: time.Second,
		recorder: rec,
	}
	handler := newMetricsHandler(holder, cfg)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ember_scrape_total",
		"a wired-up recorder must surface its self-metrics on /metrics")
}

func TestEndToEnd_FetcherToExporter(t *testing.T) {
	// End-to-end: a stubbed Caddy admin API feeds the HTTPFetcher, the
	// snapshot updates a model.State, and the resulting export is scraped via
	// the exporter Handler. Exercises the full pipeline without needing live
	// Caddy.
	caddy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			_ = json.NewEncoder(w).Encode(fetcher.ThreadsResponse{
				ThreadDebugStates: []fetcher.ThreadDebugState{
					{Index: 0, Name: "Regular PHP Thread", IsBusy: true},
					{Index: 1, Name: "Regular PHP Thread", IsWaiting: true},
				},
				ReservedThreadCount: 2,
			})
		case "/metrics":
			_, _ = io.WriteString(w, `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="srv0",code="200"} 100
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="srv0",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{server="srv0"} 5
caddy_http_request_duration_seconds_count{server="srv0"} 100
# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 1
`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer caddy.Close()

	f := fetcher.NewHTTPFetcher(caddy.URL, 0)
	f.DetectFrankenPHP(context.Background())
	require.True(t, f.HasFrankenPHP())

	var state model.State
	snap, err := f.Fetch(context.Background())
	require.NoError(t, err)
	state.Update(snap)

	holder := &exporter.StateHolder{}
	holder.Store(state.CopyForExport())

	cfg := &config{interval: time.Second}
	handler := newMetricsHandler(holder, cfg)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, "process_cpu_percent")
	assert.Contains(t, body, "process_rss_bytes")
}

func TestEndToEnd_FetchPropagatesPartialFailure(t *testing.T) {
	caddy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/frankenphp/threads":
			_ = json.NewEncoder(w).Encode(fetcher.ThreadsResponse{
				ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}},
			})
		case "/metrics":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer caddy.Close()

	f := fetcher.NewHTTPFetcher(caddy.URL, 0)
	f.DetectFrankenPHP(context.Background())

	snap, err := f.Fetch(context.Background())
	require.NoError(t, err, "Fetch must degrade gracefully when only one stage fails")
	require.NotNil(t, snap)
	assert.NotEmpty(t, snap.Errors, "the failed metrics fetch must be recorded in the snapshot")
	assert.Len(t, snap.Threads.ThreadDebugStates, 1, "successful threads fetch must still surface")
}
