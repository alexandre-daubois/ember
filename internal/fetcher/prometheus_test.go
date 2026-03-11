package fetcher

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err)

	assert.Equal(t, float64(20), snap.TotalThreads, "TotalThreads")
	assert.Equal(t, float64(4), snap.BusyThreads, "BusyThreads")
	assert.Equal(t, float64(2), snap.QueueDepth, "QueueDepth")
}

func TestParsePrometheusMetrics_WorkerMetrics(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(sampleMetrics))
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

func TestParsePrometheusMetrics_Empty(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(""))
	require.NoError(t, err)
	assert.Equal(t, float64(0), snap.TotalThreads)
	assert.Empty(t, snap.Workers)
}

func TestParsePrometheusMetrics_PartialData(t *testing.T) {
	partial := `# TYPE frankenphp_busy_threads gauge
frankenphp_busy_threads 7
`
	snap, err := parsePrometheusMetrics(strings.NewReader(partial))
	require.NoError(t, err)
	assert.Equal(t, float64(7), snap.BusyThreads, "BusyThreads")
	assert.Equal(t, float64(0), snap.TotalThreads, "TotalThreads")
}

const sampleCaddyMetrics = `# HELP caddy_http_requests_total Total HTTP requests
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{handler="subroute",server="srv0",code="200"} 150
caddy_http_requests_total{handler="subroute",server="srv0",code="404"} 10
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
`

func TestParsePrometheusMetrics_CaddyHTTP(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(sampleCaddyMetrics))
	require.NoError(t, err)

	assert.Equal(t, float64(160), snap.HTTPRequestsTotal, "HTTPRequestsTotal")
	assert.Equal(t, 12.5, snap.HTTPRequestDurationSum, "HTTPRequestDurationSum")
	assert.Equal(t, float64(160), snap.HTTPRequestDurationCount, "HTTPRequestDurationCount")
	assert.Equal(t, float64(42), snap.HTTPRequestsInFlight, "HTTPRequestsInFlight")
	assert.True(t, snap.HasHTTPMetrics, "HasHTTPMetrics should be true when Caddy metrics are present")
}

func TestParsePrometheusMetrics_CaddyHistogramBuckets(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(sampleCaddyMetrics))
	require.NoError(t, err)

	require.Len(t, snap.DurationBuckets, 3, "should parse 3 histogram buckets")

	assert.Equal(t, 0.005, snap.DurationBuckets[0].UpperBound)
	assert.Equal(t, float64(50), snap.DurationBuckets[0].CumulativeCount)

	assert.Equal(t, 0.01, snap.DurationBuckets[1].UpperBound)
	assert.Equal(t, float64(100), snap.DurationBuckets[1].CumulativeCount)

	assert.True(t, snap.DurationBuckets[2].UpperBound > 1e300, "+Inf bucket")
	assert.Equal(t, float64(160), snap.DurationBuckets[2].CumulativeCount)
}

func TestParsePrometheusMetrics_NoBucketsWithoutHistogram(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(sampleMetrics))
	require.NoError(t, err)
	assert.Empty(t, snap.DurationBuckets)
}

func TestParsePrometheusMetrics_HasHTTPMetrics_False(t *testing.T) {
	snap, err := parsePrometheusMetrics(strings.NewReader(sampleMetrics))
	require.NoError(t, err)

	assert.False(t, snap.HasHTTPMetrics, "HasHTTPMetrics should be false when only FrankenPHP metrics are present")
}

func TestParsePrometheusMetrics_Mixed(t *testing.T) {
	mixed := sampleMetrics + sampleCaddyMetrics
	snap, err := parsePrometheusMetrics(strings.NewReader(mixed))
	require.NoError(t, err)

	assert.Equal(t, float64(20), snap.TotalThreads, "TotalThreads")
	assert.Equal(t, float64(160), snap.HTTPRequestsTotal, "HTTPRequestsTotal")
	assert.Len(t, snap.Workers, 2)
	assert.True(t, snap.HasHTTPMetrics, "HasHTTPMetrics should be true in mixed metrics")
}
