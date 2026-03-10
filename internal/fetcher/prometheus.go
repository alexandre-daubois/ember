package fetcher

import (
	"fmt"
	"io"

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

	return snap, nil
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
