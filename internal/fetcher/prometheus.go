package fetcher

import (
	"io"

	dto "github.com/prometheus/client_model/go"

	"github.com/alexandre-daubois/ember/pkg/metrics"
)

func parsePrometheusMetrics(r io.Reader) (MetricsSnapshot, error) {
	return metrics.ParsePrometheus(r)
}

// Wrappers kept for internal tests that exercise these helpers directly.

func sortBuckets(buckets []HistogramBucket) {
	metrics.SortBuckets(buckets)
}

func scalarValue(families map[string]*dto.MetricFamily, name string) float64 {
	return metrics.ScalarValue(families, name)
}
