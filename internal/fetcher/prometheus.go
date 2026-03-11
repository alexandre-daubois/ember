package fetcher

import (
	"fmt"
	"io"
	"slices"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func parsePrometheusMetrics(r io.Reader) (MetricsSnapshot, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(r)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("parse prometheus: %w", err)
	}

	snap := MetricsSnapshot{
		Workers: make(map[string]*WorkerMetrics),
	}

	snap.TotalThreads = scalarValue(families, "frankenphp_total_threads")
	snap.BusyThreads = scalarValue(families, "frankenphp_busy_threads")
	snap.QueueDepth = scalarValue(families, "frankenphp_queue_depth")

	perWorker := []struct {
		name   string
		setter func(wm *WorkerMetrics, v float64)
	}{
		{"frankenphp_total_workers", func(wm *WorkerMetrics, v float64) { wm.Total = v }},
		{"frankenphp_busy_workers", func(wm *WorkerMetrics, v float64) { wm.Busy = v }},
		{"frankenphp_ready_workers", func(wm *WorkerMetrics, v float64) { wm.Ready = v }},
		{"frankenphp_worker_request_time", func(wm *WorkerMetrics, v float64) { wm.RequestTime = v }},
		{"frankenphp_worker_request_count", func(wm *WorkerMetrics, v float64) { wm.RequestCount = v }},
		{"frankenphp_worker_crashes", func(wm *WorkerMetrics, v float64) { wm.Crashes = v }},
		{"frankenphp_worker_restarts", func(wm *WorkerMetrics, v float64) { wm.Restarts = v }},
		{"frankenphp_worker_queue_depth", func(wm *WorkerMetrics, v float64) { wm.QueueDepth = v }},
	}

	for _, pw := range perWorker {
		fam, ok := families[pw.name]
		if !ok {
			continue
		}
		for _, m := range fam.GetMetric() {
			worker := labelValue(m, "worker")
			if worker == "" {
				continue
			}
			wm := snap.getOrCreateWorker(worker)
			pw.setter(wm, metricValue(m))
		}
	}

	// Caddy HTTP metrics (available with `metrics` directive)
	snap.HTTPRequestsTotal = sumCounter(families, "caddy_http_requests_total")
	snap.HTTPRequestDurationSum, snap.HTTPRequestDurationCount, snap.DurationBuckets = histogramData(families, "caddy_http_request_duration_seconds")
	snap.HTTPRequestsInFlight = scalarValue(families, "caddy_http_requests_in_flight")
	snap.HasHTTPMetrics = snap.HTTPRequestsTotal > 0 || snap.HTTPRequestDurationCount > 0

	return snap, nil
}

func sumCounter(families map[string]*dto.MetricFamily, name string) float64 {
	fam, ok := families[name]
	if !ok {
		return 0
	}
	var total float64
	for _, m := range fam.GetMetric() {
		total += metricValue(m)
	}
	return total
}

func histogramData(families map[string]*dto.MetricFamily, name string) (float64, float64, []HistogramBucket) {
	fam, ok := families[name]
	if !ok {
		return 0, 0, nil
	}
	var sumTotal, countTotal float64
	bucketMap := make(map[float64]float64)
	for _, m := range fam.GetMetric() {
		if h := m.GetHistogram(); h != nil {
			sumTotal += h.GetSampleSum()
			countTotal += float64(h.GetSampleCount())
			for _, b := range h.GetBucket() {
				bucketMap[b.GetUpperBound()] += float64(b.GetCumulativeCount())
			}
		}
	}

	var buckets []HistogramBucket
	for ub, count := range bucketMap {
		buckets = append(buckets, HistogramBucket{UpperBound: ub, CumulativeCount: count})
	}
	slices.SortFunc(buckets, func(a, b HistogramBucket) int {
		if a.UpperBound < b.UpperBound {
			return -1
		}
		if a.UpperBound > b.UpperBound {
			return 1
		}
		return 0
	})

	return sumTotal, countTotal, buckets
}

func scalarValue(families map[string]*dto.MetricFamily, name string) float64 {
	fam, ok := families[name]
	if !ok || len(fam.GetMetric()) == 0 {
		return 0
	}
	return metricValue(fam.GetMetric()[0])
}

func metricValue(m *dto.Metric) float64 {
	if g := m.GetGauge(); g != nil {
		return g.GetValue()
	}
	if c := m.GetCounter(); c != nil {
		return c.GetValue()
	}
	if u := m.GetUntyped(); u != nil {
		return u.GetValue()
	}
	return 0
}

func labelValue(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

func (s *MetricsSnapshot) getOrCreateWorker(name string) *WorkerMetrics {
	wm, ok := s.Workers[name]
	if !ok {
		wm = &WorkerMetrics{Worker: name}
		s.Workers[name] = wm
	}
	return wm
}
