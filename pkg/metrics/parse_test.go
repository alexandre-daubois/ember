package metrics_test

import (
	"math"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	prommodel "github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alexandre-daubois/ember/pkg/metrics"
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

func TestParsePrometheus_GlobalMetrics(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)

	assert.Equal(t, float64(20), snap.TotalThreads, "TotalThreads")
	assert.Equal(t, float64(4), snap.BusyThreads, "BusyThreads")
	assert.Equal(t, float64(2), snap.QueueDepth, "QueueDepth")
	assert.False(t, snap.HasConfigReloadMetrics, "HasConfigReloadMetrics should be false for FrankenPHP-only metrics")
	assert.Equal(t, float64(0), snap.ConfigLastReloadSuccessful, "ConfigLastReloadSuccessful default")
	assert.Equal(t, float64(0), snap.ConfigLastReloadSuccessTimestamp, "ConfigLastReloadSuccessTimestamp default")
}

func TestParsePrometheus_WorkerMetrics(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)
	require.Len(t, snap.Workers, 2)

	w := snap.Workers["/app/worker.php"]
	require.NotNil(t, w)

	assert.Equal(t, float64(8), w.Total, "Total")
	assert.Equal(t, float64(3), w.Busy, "Busy")
	assert.Equal(t, float64(5), w.Ready, "Ready")
	assert.Equal(t, 125.5, w.RequestTime, "RequestTime")
	assert.Equal(t, float64(10000), w.RequestCount, "RequestCount")
	assert.Equal(t, float64(2), w.Crashes, "Crashes")
	assert.Equal(t, float64(5), w.Restarts, "Restarts")
	assert.Equal(t, float64(1), w.QueueDepth, "QueueDepth")

	api := snap.Workers["/app/api.php"]
	require.NotNil(t, api)
	assert.Equal(t, float64(3000), api.RequestCount, "api.RequestCount")
	assert.Equal(t, float64(0), api.Crashes, "api.Crashes")
}

func TestParsePrometheus_Empty(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(""))
	require.NoError(t, err)
	assert.Equal(t, float64(0), snap.TotalThreads)
	assert.Empty(t, snap.Workers)
}

func TestParsePrometheus_PartialData(t *testing.T) {
	partial := `# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 7
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(partial))
	require.NoError(t, err)
	assert.Equal(t, float64(7), snap.BusyThreads, "BusyThreads")
	assert.Equal(t, float64(0), snap.TotalThreads, "TotalThreads")
}

const sampleCaddyMetrics = `# HELP caddy_http_requests_total Total HTTP requests
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute",server="srv0",code="200"} 150
caddy_http_requests_total{handler="subroute",server="srv0",code="404"} 10
# HELP caddy_http_request_errors_total Total HTTP request errors
# TYPE caddy_http_request_errors_total counter
caddy_http_request_errors_total{handler="reverse_proxy",server="srv0"} 7
# HELP caddy_http_request_duration_seconds Histogram of request durations
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{handler="subroute",server="srv0",le="0.005"} 50
caddy_http_request_duration_seconds_bucket{handler="subroute",server="srv0",le="0.01"} 100
caddy_http_request_duration_seconds_bucket{handler="subroute",server="srv0",le="+Inf"} 160
caddy_http_request_duration_seconds_sum{handler="subroute",server="srv0"} 12.5
caddy_http_request_duration_seconds_count{handler="subroute",server="srv0"} 160
# HELP caddy_http_requests_in_flight Active HTTP requests
# TYPE caddy_http_requests_in_flight gauge
caddy_http_requests_in_flight{handler="subroute",server="srv0"} 42
# HELP caddy_config_last_reload_successful Whether the last configuration reload was successful
# TYPE caddy_config_last_reload_successful gauge
caddy_config_last_reload_successful 1
# HELP caddy_config_last_reload_success_timestamp_seconds Timestamp of the last successful configuration reload
# TYPE caddy_config_last_reload_success_timestamp_seconds gauge
caddy_config_last_reload_success_timestamp_seconds 1.7120736e+09
`

func TestParsePrometheus_CaddyHTTP(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleCaddyMetrics))
	require.NoError(t, err)

	assert.Equal(t, float64(160), snap.HTTPRequestsTotal, "HTTPRequestsTotal")
	assert.Equal(t, float64(7), snap.HTTPRequestErrorsTotal, "HTTPRequestErrorsTotal")
	assert.Equal(t, 12.5, snap.HTTPRequestDurationSum, "HTTPRequestDurationSum")
	assert.Equal(t, float64(160), snap.HTTPRequestDurationCount, "HTTPRequestDurationCount")
	assert.Equal(t, float64(42), snap.HTTPRequestsInFlight, "HTTPRequestsInFlight")
	assert.True(t, snap.HasHTTPMetrics, "HasHTTPMetrics should be true when Caddy metrics are present")
	assert.True(t, snap.HasConfigReloadMetrics, "HasConfigReloadMetrics")
	assert.Equal(t, float64(1), snap.ConfigLastReloadSuccessful, "ConfigLastReloadSuccessful")
	assert.Equal(t, 1.7120736e+09, snap.ConfigLastReloadSuccessTimestamp, "ConfigLastReloadSuccessTimestamp")
}

func TestParsePrometheus_ReloadFailed(t *testing.T) {
	input := `# HELP caddy_config_last_reload_successful Whether the last configuration reload was successful
# TYPE caddy_config_last_reload_successful gauge
caddy_config_last_reload_successful 0
# HELP caddy_config_last_reload_success_timestamp_seconds Timestamp of the last successful configuration reload
# TYPE caddy_config_last_reload_success_timestamp_seconds gauge
caddy_config_last_reload_success_timestamp_seconds 1.7120736e+09
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)
	assert.True(t, snap.HasConfigReloadMetrics)
	assert.Equal(t, float64(0), snap.ConfigLastReloadSuccessful)
	assert.Equal(t, 1.7120736e+09, snap.ConfigLastReloadSuccessTimestamp)
}

func TestParsePrometheus_CaddyHistogramBuckets(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleCaddyMetrics))
	require.NoError(t, err)

	require.Len(t, snap.DurationBuckets, 3, "should parse 3 histogram buckets")

	assert.Equal(t, 0.005, snap.DurationBuckets[0].UpperBound)
	assert.Equal(t, float64(50), snap.DurationBuckets[0].CumulativeCount)

	assert.Equal(t, 0.01, snap.DurationBuckets[1].UpperBound)
	assert.Equal(t, float64(100), snap.DurationBuckets[1].CumulativeCount)

	assert.True(t, snap.DurationBuckets[2].UpperBound > 1e300, "+Inf bucket")
	assert.Equal(t, float64(160), snap.DurationBuckets[2].CumulativeCount)
}

func TestParsePrometheus_NoBucketsWithoutHistogram(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)
	assert.Empty(t, snap.DurationBuckets)
}

func TestParsePrometheus_HasHTTPMetrics_False(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)

	assert.False(t, snap.HasHTTPMetrics, "HasHTTPMetrics should be false when only FrankenPHP metrics are present")
}

func TestParsePrometheus_NoErrorMetrics(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)

	assert.Equal(t, float64(0), snap.HTTPRequestErrorsTotal, "HTTPRequestErrorsTotal should be 0 without error metrics")
}

const samplePerHostMetrics = `# HELP caddy_http_requests_total Total HTTP requests
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="example.com",handler="subroute",server="srv0",code="200"} 100
caddy_http_requests_total{host="example.com",handler="subroute",server="srv0",code="404"} 5
caddy_http_requests_total{host="api.example.com",handler="subroute",server="srv0",code="200"} 50
caddy_http_requests_total{host="api.example.com",handler="subroute",server="srv0",code="500"} 2
# HELP caddy_http_request_errors_total Total HTTP request errors
# TYPE caddy_http_request_errors_total counter
caddy_http_request_errors_total{host="example.com",handler="reverse_proxy"} 3
caddy_http_request_errors_total{host="api.example.com",handler="reverse_proxy"} 12
# HELP caddy_http_request_duration_seconds Histogram of request durations
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{host="example.com",le="0.005"} 30
caddy_http_request_duration_seconds_bucket{host="example.com",le="0.01"} 60
caddy_http_request_duration_seconds_bucket{host="example.com",le="+Inf"} 105
caddy_http_request_duration_seconds_sum{host="example.com"} 8.5
caddy_http_request_duration_seconds_count{host="example.com"} 105
caddy_http_request_duration_seconds_bucket{host="api.example.com",le="0.005"} 10
caddy_http_request_duration_seconds_bucket{host="api.example.com",le="0.01"} 30
caddy_http_request_duration_seconds_bucket{host="api.example.com",le="+Inf"} 52
caddy_http_request_duration_seconds_sum{host="api.example.com"} 4.0
caddy_http_request_duration_seconds_count{host="api.example.com"} 52
# HELP caddy_http_requests_in_flight Active HTTP requests
# TYPE caddy_http_requests_in_flight gauge
caddy_http_requests_in_flight{host="example.com",handler="subroute",server="srv0"} 3
caddy_http_requests_in_flight{host="api.example.com",handler="subroute",server="srv0"} 7
`

func TestPerHostMetrics_GroupsByHost(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(samplePerHostMetrics))
	require.NoError(t, err)

	require.Len(t, snap.Hosts, 2)

	ex := snap.Hosts["example.com"]
	require.NotNil(t, ex)
	assert.Equal(t, float64(105), ex.RequestsTotal)
	assert.Equal(t, 8.5, ex.DurationSum)
	assert.Equal(t, float64(105), ex.DurationCount)
	assert.Equal(t, float64(3), ex.InFlight)
	assert.Equal(t, float64(100), ex.StatusCodes[200])
	assert.Equal(t, float64(5), ex.StatusCodes[404])
	assert.Equal(t, float64(3), ex.ErrorsTotal, "per-host ErrorsTotal")
	assert.Len(t, ex.DurationBuckets, 3)

	api := snap.Hosts["api.example.com"]
	require.NotNil(t, api)
	assert.Equal(t, float64(52), api.RequestsTotal)
	assert.Equal(t, float64(7), api.InFlight)
	assert.Equal(t, float64(50), api.StatusCodes[200])
	assert.Equal(t, float64(2), api.StatusCodes[500])
	assert.Equal(t, float64(12), api.ErrorsTotal, "per-host ErrorsTotal")
}

func TestPerHostMetrics_IgnoresServerOnlyLeftoversWhenPerHostActive(t *testing.T) {
	// Reproduces the "srv0 appears alongside real hosts" symptom users hit
	// after enabling `per_host` on a running Caddy: the earlier server-only
	// counter is still present in Caddy's Prometheus registry (reloads do
	// not reset it), so we get both flavours in the same scrape. Ember
	// should surface only the real hosts.
	input := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute",server="srv0",code="200"} 999
caddy_http_requests_total{host="kept.com",handler="subroute",server="srv0",code="200"} 10
caddy_http_requests_total{host="other.com",handler="subroute",server="srv0",code="200"} 5
# TYPE caddy_http_requests_in_flight gauge
caddy_http_requests_in_flight{handler="subroute",server="srv0"} 42
caddy_http_requests_in_flight{host="kept.com",handler="subroute",server="srv0"} 1
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	require.Len(t, snap.Hosts, 2, "srv0 leftover must be dropped once per_host labels are present")
	assert.NotContains(t, snap.Hosts, "srv0")
	assert.Contains(t, snap.Hosts, "kept.com")
	assert.Contains(t, snap.Hosts, "other.com")
}

func TestPerHostMetrics_UnrelatedHostLabelDoesNotFlipHeuristic(t *testing.T) {
	// A custom plugin metric with its own "host" label must not trick the
	// per_host detection into dropping legitimate caddy_http_* metrics that
	// carry only "server". The scan is scoped to caddy_http_* for that
	// reason.
	input := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute",server="srv0",code="200"} 42
# TYPE my_plugin_metric gauge
my_plugin_metric{host="plugin-internal"} 1
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	require.Len(t, snap.Hosts, 1, "server fallback must still kick in when no caddy_http_* carries a host label")
	assert.Contains(t, snap.Hosts, "srv0")
}

func TestPerHostMetrics_GroupsByServerLabel(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleCaddyMetrics))
	require.NoError(t, err)

	require.Len(t, snap.Hosts, 1, "should group by server label")
	srv := snap.Hosts["srv0"]
	require.NotNil(t, srv)
	assert.Equal(t, float64(160), srv.RequestsTotal)
	assert.Equal(t, 12.5, srv.DurationSum)
	assert.Equal(t, float64(42), srv.InFlight)
	assert.Equal(t, float64(150), srv.StatusCodes[200])
	assert.Equal(t, float64(10), srv.StatusCodes[404])
}

const sampleRealCaddyMetrics = `# HELP caddy_http_requests_total Counter of HTTP(S) requests made.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute",server="srv0"} 3236
# HELP caddy_http_request_duration_seconds Histogram of round-trip request durations.
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{code="200",handler="subroute",method="GET",server="srv0",le="0.1"} 418
caddy_http_request_duration_seconds_bucket{code="200",handler="subroute",method="GET",server="srv0",le="+Inf"} 704
caddy_http_request_duration_seconds_sum{code="200",handler="subroute",method="GET",server="srv0"} 85.83
caddy_http_request_duration_seconds_count{code="200",handler="subroute",method="GET",server="srv0"} 704
caddy_http_request_duration_seconds_bucket{code="404",handler="subroute",method="GET",server="srv0",le="0.1"} 40
caddy_http_request_duration_seconds_bucket{code="404",handler="subroute",method="GET",server="srv0",le="+Inf"} 1828
caddy_http_request_duration_seconds_sum{code="404",handler="subroute",method="GET",server="srv0"} 428.22
caddy_http_request_duration_seconds_count{code="404",handler="subroute",method="GET",server="srv0"} 1828
# HELP caddy_http_requests_in_flight Number of requests currently handled by this server.
# TYPE caddy_http_requests_in_flight gauge
caddy_http_requests_in_flight{handler="subroute",server="srv0"} 5
`

func TestStatusCodesFromHistogram_ServerLabel(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleRealCaddyMetrics))
	require.NoError(t, err)

	require.Len(t, snap.Hosts, 1)
	srv := snap.Hosts["srv0"]
	require.NotNil(t, srv)
	assert.Equal(t, float64(3236), srv.RequestsTotal)
	assert.Equal(t, float64(5), srv.InFlight)
	assert.Equal(t, float64(704), srv.StatusCodes[200], "should extract 200 from histogram count")
	assert.Equal(t, float64(1828), srv.StatusCodes[404], "should extract 404 from histogram count")
}

const samplePerHostNoCounterCodes = `# HELP caddy_http_requests_total Counter of HTTP(S) requests made.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="example.com",handler="subroute",server="srv0"} 200
caddy_http_requests_total{host="api.example.com",handler="subroute",server="srv0"} 100
# HELP caddy_http_request_duration_seconds Histogram of request durations
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{host="example.com",code="200",le="0.1"} 80
caddy_http_request_duration_seconds_bucket{host="example.com",code="200",le="+Inf"} 150
caddy_http_request_duration_seconds_sum{host="example.com",code="200"} 10.0
caddy_http_request_duration_seconds_count{host="example.com",code="200"} 150
caddy_http_request_duration_seconds_bucket{host="example.com",code="404",le="0.1"} 20
caddy_http_request_duration_seconds_bucket{host="example.com",code="404",le="+Inf"} 50
caddy_http_request_duration_seconds_sum{host="example.com",code="404"} 5.0
caddy_http_request_duration_seconds_count{host="example.com",code="404"} 50
caddy_http_request_duration_seconds_bucket{host="api.example.com",code="200",le="0.1"} 60
caddy_http_request_duration_seconds_bucket{host="api.example.com",code="200",le="+Inf"} 90
caddy_http_request_duration_seconds_sum{host="api.example.com",code="200"} 8.0
caddy_http_request_duration_seconds_count{host="api.example.com",code="200"} 90
caddy_http_request_duration_seconds_bucket{host="api.example.com",code="500",le="0.1"} 5
caddy_http_request_duration_seconds_bucket{host="api.example.com",code="500",le="+Inf"} 10
caddy_http_request_duration_seconds_sum{host="api.example.com",code="500"} 1.0
caddy_http_request_duration_seconds_count{host="api.example.com",code="500"} 10
`

func TestPerHostMetrics_StatusCodesFromHistogram(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(samplePerHostNoCounterCodes))
	require.NoError(t, err)

	require.Len(t, snap.Hosts, 2)

	ex := snap.Hosts["example.com"]
	require.NotNil(t, ex)
	assert.Equal(t, float64(200), ex.RequestsTotal)
	assert.Equal(t, float64(150), ex.StatusCodes[200], "200 from histogram count")
	assert.Equal(t, float64(50), ex.StatusCodes[404], "404 from histogram count")

	api := snap.Hosts["api.example.com"]
	require.NotNil(t, api)
	assert.Equal(t, float64(100), api.RequestsTotal)
	assert.Equal(t, float64(90), api.StatusCodes[200], "200 from histogram count")
	assert.Equal(t, float64(10), api.StatusCodes[500], "500 from histogram count")
}

func TestStatusCodesFromHistogram_NoHistogram(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)
	assert.Empty(t, snap.DurationBuckets)
}

const sampleServerLabelMetrics = `# HELP caddy_http_requests_total Counter of HTTP(S) requests made.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute",server="main"} 50
caddy_http_requests_total{handler="subroute",server="app"} 30
caddy_http_requests_total{handler="subroute",server="api"} 20
# HELP caddy_http_request_duration_seconds Histogram of request durations.
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{code="200",handler="subroute",method="GET",server="main",le="0.1"} 40
caddy_http_request_duration_seconds_bucket{code="200",handler="subroute",method="GET",server="main",le="+Inf"} 50
caddy_http_request_duration_seconds_sum{code="200",handler="subroute",method="GET",server="main"} 3.5
caddy_http_request_duration_seconds_count{code="200",handler="subroute",method="GET",server="main"} 50
caddy_http_request_duration_seconds_bucket{code="200",handler="subroute",method="GET",server="app",le="0.1"} 25
caddy_http_request_duration_seconds_bucket{code="200",handler="subroute",method="GET",server="app",le="+Inf"} 30
caddy_http_request_duration_seconds_sum{code="200",handler="subroute",method="GET",server="app"} 2.0
caddy_http_request_duration_seconds_count{code="200",handler="subroute",method="GET",server="app"} 30
caddy_http_request_duration_seconds_bucket{code="404",handler="subroute",method="GET",server="api",le="0.1"} 15
caddy_http_request_duration_seconds_bucket{code="404",handler="subroute",method="GET",server="api",le="+Inf"} 20
caddy_http_request_duration_seconds_sum{code="404",handler="subroute",method="GET",server="api"} 1.0
caddy_http_request_duration_seconds_count{code="404",handler="subroute",method="GET",server="api"} 20
# HELP caddy_http_requests_in_flight Number of requests currently handled.
# TYPE caddy_http_requests_in_flight gauge
caddy_http_requests_in_flight{handler="subroute",server="main"} 3
caddy_http_requests_in_flight{handler="subroute",server="app"} 1
caddy_http_requests_in_flight{handler="subroute",server="api"} 0
`

func TestPerHostMetrics_FallbackToServerLabel(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleServerLabelMetrics))
	require.NoError(t, err)

	require.Len(t, snap.Hosts, 3, "should group by server label")

	main := snap.Hosts["main"]
	require.NotNil(t, main)
	assert.Equal(t, float64(50), main.RequestsTotal)
	assert.Equal(t, float64(3), main.InFlight)
	assert.Equal(t, 3.5, main.DurationSum)
	assert.Equal(t, float64(50), main.StatusCodes[200])

	app := snap.Hosts["app"]
	require.NotNil(t, app)
	assert.Equal(t, float64(30), app.RequestsTotal)
	assert.Equal(t, float64(1), app.InFlight)
	assert.Equal(t, float64(30), app.StatusCodes[200])

	api := snap.Hosts["api"]
	require.NotNil(t, api)
	assert.Equal(t, float64(20), api.RequestsTotal)
	assert.Equal(t, float64(0), api.InFlight)
	assert.Equal(t, float64(20), api.StatusCodes[404])
}

func TestAggregateStatusCodes_FallbackWithoutHostLabels(t *testing.T) {
	input := `# HELP caddy_http_requests_total Counter.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{code="200"} 100
caddy_http_requests_total{code="404"} 15
caddy_http_requests_total{code="500"} 3
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{le="+Inf"} 118
caddy_http_request_duration_seconds_sum 10.0
caddy_http_request_duration_seconds_count 118
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	require.Contains(t, snap.Hosts, "*", "should fall back to * when no host/server label")
	star := snap.Hosts["*"]
	assert.Equal(t, float64(100), star.StatusCodes[200])
	assert.Equal(t, float64(15), star.StatusCodes[404])
	assert.Equal(t, float64(3), star.StatusCodes[500])
}

func TestStatusCodesFromHistogram_FallbackWithoutHostLabels(t *testing.T) {
	input := `# HELP caddy_http_request_duration_seconds Histogram.
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{code="200",le="0.1"} 50
caddy_http_request_duration_seconds_bucket{code="200",le="+Inf"} 80
caddy_http_request_duration_seconds_sum{code="200"} 5.0
caddy_http_request_duration_seconds_count{code="200"} 80
caddy_http_request_duration_seconds_bucket{code="500",le="0.1"} 5
caddy_http_request_duration_seconds_bucket{code="500",le="+Inf"} 10
caddy_http_request_duration_seconds_sum{code="500"} 1.0
caddy_http_request_duration_seconds_count{code="500"} 10
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	require.Contains(t, snap.Hosts, "*", "should fall back to * when no host/server label")
	star := snap.Hosts["*"]
	assert.Equal(t, float64(80), star.StatusCodes[200], "should use histogram sample count for 200")
	assert.Equal(t, float64(10), star.StatusCodes[500], "should use histogram sample count for 500")
}

func TestAggregateStatusCodes_NoCodeLabel(t *testing.T) {
	input := `# HELP caddy_http_requests_total Counter.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute"} 100
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{le="+Inf"} 100
caddy_http_request_duration_seconds_sum 5.0
caddy_http_request_duration_seconds_count 100
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	require.Contains(t, snap.Hosts, "*")
	star := snap.Hosts["*"]
	assert.Nil(t, star.StatusCodes, "no code label should result in nil status codes")
}

func TestPerHostMetrics_HostLabelTakesPriority(t *testing.T) {
	input := `# HELP caddy_http_requests_total Counter.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="example.com",handler="subroute",server="srv0"} 100
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	_, hasHost := snap.Hosts["example.com"]
	_, hasServer := snap.Hosts["srv0"]
	assert.True(t, hasHost, "should use host label")
	assert.False(t, hasServer, "should not use server label when host exists")
}

func TestPerHostMetrics_MethodExtraction(t *testing.T) {
	input := `# HELP caddy_http_requests_total Counter.
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="main",code="200",method="GET"} 80
caddy_http_requests_total{server="main",code="200",method="POST"} 15
caddy_http_requests_total{server="main",code="404",method="GET"} 5
caddy_http_requests_total{server="api",code="200",method="GET"} 30
caddy_http_requests_total{server="api",code="200",method="PUT"} 10
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="main",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{server="main"} 5.0
caddy_http_request_duration_seconds_count{server="main"} 100
caddy_http_request_duration_seconds_bucket{server="api",le="+Inf"} 40
caddy_http_request_duration_seconds_sum{server="api"} 2.0
caddy_http_request_duration_seconds_count{server="api"} 40
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	main := snap.Hosts["main"]
	require.NotNil(t, main)
	assert.Equal(t, float64(85), main.Methods["GET"])
	assert.Equal(t, float64(15), main.Methods["POST"])

	api := snap.Hosts["api"]
	require.NotNil(t, api)
	assert.Equal(t, float64(30), api.Methods["GET"])
	assert.Equal(t, float64(10), api.Methods["PUT"])
}

func TestSortBuckets(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buckets []metrics.HistogramBucket
		metrics.SortBuckets(buckets)
		assert.Empty(t, buckets)
	})

	t.Run("single element", func(t *testing.T) {
		buckets := []metrics.HistogramBucket{{UpperBound: 0.5, CumulativeCount: 10}}
		metrics.SortBuckets(buckets)
		require.Len(t, buckets, 1)
		assert.Equal(t, 0.5, buckets[0].UpperBound)
	})

	t.Run("already sorted", func(t *testing.T) {
		buckets := []metrics.HistogramBucket{
			{UpperBound: 0.005, CumulativeCount: 10},
			{UpperBound: 0.01, CumulativeCount: 20},
			{UpperBound: 0.1, CumulativeCount: 50},
			{UpperBound: math.Inf(1), CumulativeCount: 100},
		}
		metrics.SortBuckets(buckets)
		assert.Equal(t, 0.005, buckets[0].UpperBound)
		assert.Equal(t, 0.01, buckets[1].UpperBound)
		assert.Equal(t, 0.1, buckets[2].UpperBound)
		assert.True(t, math.IsInf(buckets[3].UpperBound, 1))
	})

	t.Run("unsorted", func(t *testing.T) {
		buckets := []metrics.HistogramBucket{
			{UpperBound: math.Inf(1), CumulativeCount: 100},
			{UpperBound: 0.01, CumulativeCount: 20},
			{UpperBound: 0.1, CumulativeCount: 50},
			{UpperBound: 0.005, CumulativeCount: 10},
		}
		metrics.SortBuckets(buckets)
		assert.Equal(t, 0.005, buckets[0].UpperBound)
		assert.Equal(t, 0.01, buckets[1].UpperBound)
		assert.Equal(t, 0.1, buckets[2].UpperBound)
		assert.True(t, math.IsInf(buckets[3].UpperBound, 1))
	})
}

func TestSortBuckets_DuplicateUpperBounds(t *testing.T) {
	buckets := []metrics.HistogramBucket{
		{UpperBound: 0.1, CumulativeCount: 30},
		{UpperBound: 0.01, CumulativeCount: 10},
		{UpperBound: 0.1, CumulativeCount: 50},
	}
	metrics.SortBuckets(buckets)
	assert.Equal(t, 0.01, buckets[0].UpperBound)
	assert.Equal(t, 0.1, buckets[1].UpperBound)
	assert.Equal(t, 0.1, buckets[2].UpperBound)
}

func TestScalarValue_HistogramReturnsZero(t *testing.T) {
	input := `# HELP test_hist A histogram
# TYPE test_hist histogram
test_hist_bucket{le="0.1"} 10
test_hist_bucket{le="+Inf"} 20
test_hist_sum 15.5
test_hist_count 20
`
	parser := expfmt.NewTextParser(prommodel.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(input))
	require.NoError(t, err)
	assert.Equal(t, 0.0, metrics.ScalarValue(families, "test_hist"))
}

func TestPerHostMetrics_ResponseSizeExtraction(t *testing.T) {
	input := `# HELP caddy_http_response_size_bytes Histogram of response sizes.
# TYPE caddy_http_response_size_bytes histogram
caddy_http_response_size_bytes_bucket{server="main",le="1000"} 50
caddy_http_response_size_bytes_bucket{server="main",le="+Inf"} 100
caddy_http_response_size_bytes_sum{server="main"} 500000
caddy_http_response_size_bytes_count{server="main"} 100
caddy_http_response_size_bytes_bucket{server="api",le="1000"} 30
caddy_http_response_size_bytes_bucket{server="api",le="+Inf"} 40
caddy_http_response_size_bytes_sum{server="api"} 200000
caddy_http_response_size_bytes_count{server="api"} 40
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="main"} 100
caddy_http_requests_total{server="api"} 40
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="main",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{server="main"} 5.0
caddy_http_request_duration_seconds_count{server="main"} 100
caddy_http_request_duration_seconds_bucket{server="api",le="+Inf"} 40
caddy_http_request_duration_seconds_sum{server="api"} 2.0
caddy_http_request_duration_seconds_count{server="api"} 40
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	main := snap.Hosts["main"]
	require.NotNil(t, main)
	assert.Equal(t, float64(500000), main.ResponseSizeSum)
	assert.Equal(t, float64(100), main.ResponseSizeCount)

	api := snap.Hosts["api"]
	require.NotNil(t, api)
	assert.Equal(t, float64(200000), api.ResponseSizeSum)
	assert.Equal(t, float64(40), api.ResponseSizeCount)
}

func TestPerHostMetrics_RequestSizeExtraction(t *testing.T) {
	input := `# HELP caddy_http_request_size_bytes Histogram of request sizes.
# TYPE caddy_http_request_size_bytes histogram
caddy_http_request_size_bytes_bucket{server="main",le="1000"} 80
caddy_http_request_size_bytes_bucket{server="main",le="+Inf"} 100
caddy_http_request_size_bytes_sum{server="main"} 250000
caddy_http_request_size_bytes_count{server="main"} 100
caddy_http_request_size_bytes_bucket{server="api",le="1000"} 20
caddy_http_request_size_bytes_bucket{server="api",le="+Inf"} 40
caddy_http_request_size_bytes_sum{server="api"} 80000
caddy_http_request_size_bytes_count{server="api"} 40
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="main"} 100
caddy_http_requests_total{server="api"} 40
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="main",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{server="main"} 5.0
caddy_http_request_duration_seconds_count{server="main"} 100
caddy_http_request_duration_seconds_bucket{server="api",le="+Inf"} 40
caddy_http_request_duration_seconds_sum{server="api"} 2.0
caddy_http_request_duration_seconds_count{server="api"} 40
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	main := snap.Hosts["main"]
	require.NotNil(t, main)
	assert.Equal(t, float64(250000), main.RequestSizeSum)
	assert.Equal(t, float64(100), main.RequestSizeCount)

	api := snap.Hosts["api"]
	require.NotNil(t, api)
	assert.Equal(t, float64(80000), api.RequestSizeSum)
	assert.Equal(t, float64(40), api.RequestSizeCount)
}

func TestPerHostMetrics_TTFBExtraction(t *testing.T) {
	input := `# HELP caddy_http_response_duration_seconds Histogram of time-to-first-byte.
# TYPE caddy_http_response_duration_seconds histogram
caddy_http_response_duration_seconds_bucket{server="main",code="200",le="0.005"} 30
caddy_http_response_duration_seconds_bucket{server="main",code="200",le="0.01"} 60
caddy_http_response_duration_seconds_bucket{server="main",code="200",le="+Inf"} 100
caddy_http_response_duration_seconds_sum{server="main",code="200"} 0.8
caddy_http_response_duration_seconds_count{server="main",code="200"} 100
caddy_http_response_duration_seconds_bucket{server="api",code="200",le="0.005"} 10
caddy_http_response_duration_seconds_bucket{server="api",code="200",le="0.01"} 25
caddy_http_response_duration_seconds_bucket{server="api",code="200",le="+Inf"} 40
caddy_http_response_duration_seconds_sum{server="api",code="200"} 0.3
caddy_http_response_duration_seconds_count{server="api",code="200"} 40
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="main"} 100
caddy_http_requests_total{server="api"} 40
# TYPE caddy_http_request_duration_seconds histogram
caddy_http_request_duration_seconds_bucket{server="main",le="+Inf"} 100
caddy_http_request_duration_seconds_sum{server="main"} 5.0
caddy_http_request_duration_seconds_count{server="main"} 100
caddy_http_request_duration_seconds_bucket{server="api",le="+Inf"} 40
caddy_http_request_duration_seconds_sum{server="api"} 2.0
caddy_http_request_duration_seconds_count{server="api"} 40
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	main := snap.Hosts["main"]
	require.NotNil(t, main)
	assert.Equal(t, 0.8, main.TTFBSum)
	assert.Equal(t, float64(100), main.TTFBCount)
	assert.Len(t, main.TTFBBuckets, 3)
	assert.Equal(t, 0.005, main.TTFBBuckets[0].UpperBound)

	api := snap.Hosts["api"]
	require.NotNil(t, api)
	assert.Equal(t, 0.3, api.TTFBSum)
	assert.Equal(t, float64(40), api.TTFBCount)
	assert.Len(t, api.TTFBBuckets, 3)
}

func TestPerHostMetrics_NoTTFB(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(samplePerHostMetrics))
	require.NoError(t, err)

	ex := snap.Hosts["example.com"]
	require.NotNil(t, ex)
	assert.Empty(t, ex.TTFBBuckets)
	assert.Equal(t, float64(0), ex.TTFBSum)
}

func TestParsePrometheus_Mixed(t *testing.T) {
	mixed := sampleMetrics + sampleCaddyMetrics
	snap, err := metrics.ParsePrometheus(strings.NewReader(mixed))
	require.NoError(t, err)

	assert.Equal(t, float64(20), snap.TotalThreads, "TotalThreads")
	assert.Equal(t, float64(160), snap.HTTPRequestsTotal, "HTTPRequestsTotal")
	assert.Len(t, snap.Workers, 2)
	assert.True(t, snap.HasHTTPMetrics, "HasHTTPMetrics should be true in mixed metrics")
}

func TestParsePrometheus_ProcessMetrics(t *testing.T) {
	input := `# TYPE process_cpu_seconds_total counter
process_cpu_seconds_total 42.5
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes 5.24288e+07
# TYPE process_start_time_seconds gauge
process_start_time_seconds 1.71e+09
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	assert.Equal(t, 42.5, snap.ProcessCPUSecondsTotal)
	assert.Equal(t, 5.24288e+07, snap.ProcessRSSBytes)
	assert.Equal(t, 1.71e+09, snap.ProcessStartTimeSeconds)
}

func TestParsePrometheus_NoProcessMetrics(t *testing.T) {
	input := `# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 5
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	assert.Equal(t, float64(0), snap.ProcessCPUSecondsTotal)
	assert.Equal(t, float64(0), snap.ProcessRSSBytes)
	assert.Equal(t, float64(0), snap.ProcessStartTimeSeconds)
}

// Caddy exports caddy_reverse_proxy_upstreams_healthy with only two labels:
// upstream and handler. The current release even omits handler entirely, so this
// fixture exercises both shapes.
const sampleUpstreamMetrics = `# HELP caddy_reverse_proxy_upstreams_healthy Health status of reverse proxy upstreams
# TYPE caddy_reverse_proxy_upstreams_healthy gauge
caddy_reverse_proxy_upstreams_healthy{upstream="10.0.0.1:8080"} 1
caddy_reverse_proxy_upstreams_healthy{upstream="10.0.0.2:8080"} 1
caddy_reverse_proxy_upstreams_healthy{upstream="10.0.0.3:8080"} 0
caddy_reverse_proxy_upstreams_healthy{handler="reverse_proxy_1",upstream="api.internal:9090"} 1
`

func TestParsePrometheus_Upstreams(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleUpstreamMetrics))
	require.NoError(t, err)
	require.Len(t, snap.Upstreams, 4)

	u1 := snap.Upstreams["10.0.0.1:8080"]
	require.NotNil(t, u1)
	assert.Equal(t, "10.0.0.1:8080", u1.Address)
	assert.Empty(t, u1.Handler, "current Caddy omits the handler label")
	assert.Equal(t, float64(1), u1.Healthy)

	u3 := snap.Upstreams["10.0.0.3:8080"]
	require.NotNil(t, u3)
	assert.Equal(t, float64(0), u3.Healthy)

	api := snap.Upstreams["api.internal:9090/reverse_proxy_1"]
	require.NotNil(t, api)
	assert.Equal(t, "api.internal:9090", api.Address)
	assert.Equal(t, "reverse_proxy_1", api.Handler)
	assert.Equal(t, float64(1), api.Healthy)
}

func TestParsePrometheus_NoUpstreams(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)
	assert.Nil(t, snap.Upstreams)
}

func TestParsePrometheus_UpstreamsWithHandlerLabel(t *testing.T) {
	input := `# TYPE caddy_reverse_proxy_upstreams_healthy gauge
caddy_reverse_proxy_upstreams_healthy{handler="rp_0",upstream="a:80"} 1
caddy_reverse_proxy_upstreams_healthy{handler="rp_1",upstream="a:80"} 0
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, snap.Upstreams, 2, "same address with different handlers should be distinct entries")

	u0 := snap.Upstreams["a:80/rp_0"]
	require.NotNil(t, u0)
	assert.Equal(t, "rp_0", u0.Handler)
	assert.Equal(t, float64(1), u0.Healthy)

	u1 := snap.Upstreams["a:80/rp_1"]
	require.NotNil(t, u1)
	assert.Equal(t, "rp_1", u1.Handler)
	assert.Equal(t, float64(0), u1.Healthy)
}

func TestParsePrometheus_UpstreamsEmptyLabel(t *testing.T) {
	input := `# HELP caddy_reverse_proxy_upstreams_healthy Health status
# TYPE caddy_reverse_proxy_upstreams_healthy gauge
caddy_reverse_proxy_upstreams_healthy{handler="rp",upstream=""} 1
caddy_reverse_proxy_upstreams_healthy{handler="rp",upstream="valid:8080"} 1
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)
	require.Len(t, snap.Upstreams, 1)
	assert.Contains(t, snap.Upstreams, "valid:8080/rp")
}

func TestParsePrometheus_ExtraFamilies(t *testing.T) {
	input := `# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 5
# HELP acmeguard_decisions_total Total decisions
# TYPE acmeguard_decisions_total counter
acmeguard_decisions_total{action="ban"} 42
acmeguard_decisions_total{action="captcha"} 7
# HELP mymodule_cache_hits Cache hit count
# TYPE mymodule_cache_hits counter
mymodule_cache_hits 1234
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	require.NotNil(t, snap.Extra)
	assert.Len(t, snap.Extra, 2)
	assert.Contains(t, snap.Extra, "acmeguard_decisions_total")
	assert.Contains(t, snap.Extra, "mymodule_cache_hits")
	assert.NotContains(t, snap.Extra, "frankenphp_busy_threads")

	fam := snap.Extra["acmeguard_decisions_total"]
	assert.Len(t, fam.GetMetric(), 2)
}

func TestParsePrometheus_NoExtraWhenAllKnown(t *testing.T) {
	snap, err := metrics.ParsePrometheus(strings.NewReader(sampleMetrics))
	require.NoError(t, err)

	assert.Nil(t, snap.Extra)
}

func TestParsePrometheus_ExtraWithCoreMetrics(t *testing.T) {
	input := `# TYPE caddy_http_requests_total counter
caddy_http_requests_total{host="example.com",code="200"} 100
# TYPE custom_plugin_metric gauge
custom_plugin_metric{instance="a"} 42
`
	snap, err := metrics.ParsePrometheus(strings.NewReader(input))
	require.NoError(t, err)

	assert.True(t, snap.HasHTTPMetrics)
	require.NotNil(t, snap.Extra)
	assert.Len(t, snap.Extra, 1)
	assert.Contains(t, snap.Extra, "custom_plugin_metric")
}
