package fetcher

import (
	"fmt"
	"io"
	"slices"
	"strconv"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func parsePrometheusMetrics(r io.Reader) (snap MetricsSnapshot, err error) {
	defer func() {
		if r := recover(); r != nil {
			snap = MetricsSnapshot{}
			err = fmt.Errorf("parse prometheus: panic: %v", r)
		}
	}()

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, parseErr := parser.TextToMetricFamilies(r)
	if parseErr != nil {
		return MetricsSnapshot{}, fmt.Errorf("parse prometheus: %w", parseErr)
	}

	snap = MetricsSnapshot{
		Workers: make(map[string]*WorkerMetrics),
		Hosts:   make(map[string]*HostMetrics),
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
	snap.HTTPRequestErrorsTotal = sumCounter(families, "caddy_http_request_errors_total")
	snap.HTTPRequestsTotal = sumCounter(families, "caddy_http_requests_total")
	snap.HTTPRequestDurationSum, snap.HTTPRequestDurationCount, snap.DurationBuckets = histogramData(families, "caddy_http_request_duration_seconds")
	snap.HTTPRequestsInFlight = scalarValue(families, "caddy_http_requests_in_flight")
	snap.HasHTTPMetrics = snap.HTTPRequestsTotal > 0 || snap.HTTPRequestDurationCount > 0

	snap.ProcessCPUSecondsTotal = scalarValue(families, "process_cpu_seconds_total")
	snap.ProcessRSSBytes = scalarValue(families, "process_resident_memory_bytes")
	snap.ProcessStartTimeSeconds = scalarValue(families, "process_start_time_seconds")

	_, hasReload := families["caddy_config_last_reload_successful"]
	snap.HasConfigReloadMetrics = hasReload
	snap.ConfigLastReloadSuccessful = scalarValue(families, "caddy_config_last_reload_successful")
	snap.ConfigLastReloadSuccessTimestamp = scalarValue(families, "caddy_config_last_reload_success_timestamp_seconds")

	snap.Hosts = perHostMetrics(families)
	snap.Upstreams = upstreamMetrics(families)

	// Fallback: if HTTP metrics exist but no host labels, aggregate as a single "*" entry
	if snap.HasHTTPMetrics && len(snap.Hosts) == 0 {
		statusCodes := aggregateStatusCodes(families, "caddy_http_requests_total")
		if statusCodes == nil {
			statusCodes = statusCodesFromHistogram(families, "caddy_http_request_duration_seconds")
		}
		snap.Hosts = map[string]*HostMetrics{
			"*": {
				Host:            "*",
				RequestsTotal:   snap.HTTPRequestsTotal,
				DurationSum:     snap.HTTPRequestDurationSum,
				DurationCount:   snap.HTTPRequestDurationCount,
				InFlight:        snap.HTTPRequestsInFlight,
				DurationBuckets: snap.DurationBuckets,
				StatusCodes:     statusCodes,
			},
		}
	}

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
	sortBuckets(buckets)

	return sumTotal, countTotal, buckets
}

func sortBuckets(buckets []HistogramBucket) {
	slices.SortFunc(buckets, func(a, b HistogramBucket) int {
		if a.UpperBound < b.UpperBound {
			return -1
		}
		if a.UpperBound > b.UpperBound {
			return 1
		}
		return 0
	})
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

func aggregateStatusCodes(families map[string]*dto.MetricFamily, name string) map[int]float64 {
	fam, ok := families[name]
	if !ok {
		return nil
	}
	codes := make(map[int]float64)
	for _, m := range fam.GetMetric() {
		if code := labelValue(m, "code"); code != "" {
			if c, err := strconv.Atoi(code); err == nil {
				codes[c] += metricValue(m)
			}
		}
	}
	if len(codes) == 0 {
		return nil
	}
	return codes
}

func statusCodesFromHistogram(families map[string]*dto.MetricFamily, name string) map[int]float64 {
	fam, ok := families[name]
	if !ok {
		return nil
	}
	codes := make(map[int]float64)
	for _, m := range fam.GetMetric() {
		h := m.GetHistogram()
		if h == nil {
			continue
		}
		if code := labelValue(m, "code"); code != "" {
			if c, err := strconv.Atoi(code); err == nil {
				codes[c] += float64(h.GetSampleCount())
			}
		}
	}
	if len(codes) == 0 {
		return nil
	}
	return codes
}

func hostOrServer(m *dto.Metric) string {
	if h := labelValue(m, "host"); h != "" {
		return h
	}
	return labelValue(m, "server")
}

func perHostMetrics(families map[string]*dto.MetricFamily) map[string]*HostMetrics {
	hosts := make(map[string]*HostMetrics)

	getOrCreate := func(host string) *HostMetrics {
		hm, ok := hosts[host]
		if !ok {
			hm = &HostMetrics{Host: host, StatusCodes: make(map[int]float64), Methods: make(map[string]float64)}
			hosts[host] = hm
		}
		return hm
	}

	hostsWithCounterCodes := make(map[string]bool)
	if fam, ok := families["caddy_http_requests_total"]; ok {
		for _, m := range fam.GetMetric() {
			host := hostOrServer(m)
			if host == "" {
				continue
			}
			hm := getOrCreate(host)
			v := metricValue(m)
			hm.RequestsTotal += v
			if code := labelValue(m, "code"); code != "" {
				if c, err := strconv.Atoi(code); err == nil {
					hm.StatusCodes[c] += v
					hostsWithCounterCodes[host] = true
				}
			}
			if method := labelValue(m, "method"); method != "" {
				hm.Methods[method] += v
			}
		}
	}

	bucketMaps := make(map[string]map[float64]float64)
	if fam, ok := families["caddy_http_request_duration_seconds"]; ok {
		for _, m := range fam.GetMetric() {
			host := hostOrServer(m)
			if host == "" {
				continue
			}
			h := m.GetHistogram()
			if h == nil {
				continue
			}
			hm := getOrCreate(host)
			hm.DurationSum += h.GetSampleSum()
			hm.DurationCount += float64(h.GetSampleCount())

			if !hostsWithCounterCodes[host] {
				if code := labelValue(m, "code"); code != "" {
					if c, err := strconv.Atoi(code); err == nil {
						hm.StatusCodes[c] += float64(h.GetSampleCount())
					}
				}
			}

			if bucketMaps[host] == nil {
				bucketMaps[host] = make(map[float64]float64)
			}
			for _, b := range h.GetBucket() {
				bucketMaps[host][b.GetUpperBound()] += float64(b.GetCumulativeCount())
			}
		}
	}

	for host, bm := range bucketMaps {
		hm := hosts[host]
		hm.DurationBuckets = make([]HistogramBucket, 0, len(bm))
		for ub, count := range bm {
			hm.DurationBuckets = append(hm.DurationBuckets, HistogramBucket{UpperBound: ub, CumulativeCount: count})
		}
		sortBuckets(hm.DurationBuckets)
	}

	ttfbBucketMaps := make(map[string]map[float64]float64)
	if fam, ok := families["caddy_http_response_duration_seconds"]; ok {
		for _, m := range fam.GetMetric() {
			host := hostOrServer(m)
			if host == "" {
				continue
			}
			h := m.GetHistogram()
			if h == nil {
				continue
			}
			hm := getOrCreate(host)
			hm.TTFBSum += h.GetSampleSum()
			hm.TTFBCount += float64(h.GetSampleCount())

			if ttfbBucketMaps[host] == nil {
				ttfbBucketMaps[host] = make(map[float64]float64)
			}
			for _, b := range h.GetBucket() {
				ttfbBucketMaps[host][b.GetUpperBound()] += float64(b.GetCumulativeCount())
			}
		}
	}

	for host, bm := range ttfbBucketMaps {
		hm := hosts[host]
		hm.TTFBBuckets = make([]HistogramBucket, 0, len(bm))
		for ub, count := range bm {
			hm.TTFBBuckets = append(hm.TTFBBuckets, HistogramBucket{UpperBound: ub, CumulativeCount: count})
		}
		sortBuckets(hm.TTFBBuckets)
	}

	if fam, ok := families["caddy_http_response_size_bytes"]; ok {
		for _, m := range fam.GetMetric() {
			host := hostOrServer(m)
			if host == "" {
				continue
			}
			h := m.GetHistogram()
			if h == nil {
				continue
			}
			hm := getOrCreate(host)
			hm.ResponseSizeSum += h.GetSampleSum()
			hm.ResponseSizeCount += float64(h.GetSampleCount())
		}
	}

	if fam, ok := families["caddy_http_request_size_bytes"]; ok {
		for _, m := range fam.GetMetric() {
			host := hostOrServer(m)
			if host == "" {
				continue
			}
			h := m.GetHistogram()
			if h == nil {
				continue
			}
			hm := getOrCreate(host)
			hm.RequestSizeSum += h.GetSampleSum()
			hm.RequestSizeCount += float64(h.GetSampleCount())
		}
	}

	if fam, ok := families["caddy_http_request_errors_total"]; ok {
		for _, m := range fam.GetMetric() {
			host := hostOrServer(m)
			if host == "" {
				continue
			}
			getOrCreate(host).ErrorsTotal += metricValue(m)
		}
	}

	if fam, ok := families["caddy_http_requests_in_flight"]; ok {
		for _, m := range fam.GetMetric() {
			host := hostOrServer(m)
			if host == "" {
				continue
			}
			getOrCreate(host).InFlight += metricValue(m)
		}
	}

	return hosts
}

func upstreamMetrics(families map[string]*dto.MetricFamily) map[string]*UpstreamMetrics {
	fam, ok := families["caddy_reverse_proxy_upstreams_healthy"]
	if !ok {
		return nil
	}

	upstreams := make(map[string]*UpstreamMetrics)
	for _, m := range fam.GetMetric() {
		addr := labelValue(m, "upstream")
		if addr == "" {
			continue
		}
		handler := labelValue(m, "handler")
		key := addr
		if handler != "" {
			key = addr + "/" + handler
		}
		upstreams[key] = &UpstreamMetrics{
			Address: addr,
			Handler: handler,
			Healthy: metricValue(m),
		}
	}

	if len(upstreams) == 0 {
		return nil
	}
	return upstreams
}

func (s *MetricsSnapshot) getOrCreateWorker(name string) *WorkerMetrics {
	wm, ok := s.Workers[name]
	if !ok {
		wm = &WorkerMetrics{Worker: name}
		s.Workers[name] = wm
	}
	return wm
}
