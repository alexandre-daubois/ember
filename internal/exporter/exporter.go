package exporter

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/alexandre-daubois/ember/internal/model"
)

type StateHolder struct {
	mu    sync.RWMutex
	state model.State
}

func (h *StateHolder) Store(s model.State) {
	h.mu.Lock()
	h.state = s
	h.mu.Unlock()
}

func (h *StateHolder) Load() model.State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state
}

const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

func Handler(holder *StateHolder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s := holder.Load()
		if s.Current == nil {
			http.Error(w, "no data yet", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", prometheusContentType)

		writeThreadMetrics(w, &s)
		writeThreadMemory(w, &s)
		writeWorkerMetrics(w, &s)
		writeHostMetrics(w, &s)
		writePercentiles(w, &s)
		writeProcessMetrics(w, &s)
	}
}

func writeThreadMetrics(w http.ResponseWriter, s *model.State) {
	total := len(s.Current.Threads.ThreadDebugStates)
	other := total - s.Derived.TotalBusy - s.Derived.TotalIdle
	if other < 0 {
		other = 0
	}

	fmt.Fprintln(w, "# HELP frankenphp_threads_total Number of FrankenPHP threads by state")
	fmt.Fprintln(w, "# TYPE frankenphp_threads_total gauge")
	fmt.Fprintf(w, "frankenphp_threads_total{state=\"busy\"} %d\n", s.Derived.TotalBusy)
	fmt.Fprintf(w, "frankenphp_threads_total{state=\"idle\"} %d\n", s.Derived.TotalIdle)
	fmt.Fprintf(w, "frankenphp_threads_total{state=\"other\"} %d\n", other)
}

func writeThreadMemory(w http.ResponseWriter, s *model.State) {
	hasMemory := false
	for _, t := range s.Current.Threads.ThreadDebugStates {
		if t.MemoryUsage > 0 {
			hasMemory = true
			break
		}
	}
	if !hasMemory {
		return
	}

	fmt.Fprintln(w, "# HELP frankenphp_thread_memory_bytes Memory usage per FrankenPHP thread")
	fmt.Fprintln(w, "# TYPE frankenphp_thread_memory_bytes gauge")
	for _, t := range s.Current.Threads.ThreadDebugStates {
		if t.MemoryUsage > 0 {
			fmt.Fprintf(w, "frankenphp_thread_memory_bytes{index=\"%d\"} %d\n", t.Index, t.MemoryUsage)
		}
	}
}

func writeWorkerMetrics(out http.ResponseWriter, s *model.State) {
	if len(s.Current.Metrics.Workers) == 0 {
		return
	}

	names := sortedWorkerNames(s.Current.Metrics.Workers)

	fmt.Fprintln(out, "# HELP frankenphp_worker_crashes_total Total worker crashes")
	fmt.Fprintln(out, "# TYPE frankenphp_worker_crashes_total counter")
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "frankenphp_worker_crashes_total{worker=\"%s\"} %g\n", escapeLabelValue(name), wm.Crashes)
	}

	fmt.Fprintln(out, "# HELP frankenphp_worker_restarts_total Total worker restarts")
	fmt.Fprintln(out, "# TYPE frankenphp_worker_restarts_total counter")
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "frankenphp_worker_restarts_total{worker=\"%s\"} %g\n", escapeLabelValue(name), wm.Restarts)
	}

	fmt.Fprintln(out, "# HELP frankenphp_worker_queue_depth Requests in queue per worker")
	fmt.Fprintln(out, "# TYPE frankenphp_worker_queue_depth gauge")
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "frankenphp_worker_queue_depth{worker=\"%s\"} %g\n", escapeLabelValue(name), wm.QueueDepth)
	}

	fmt.Fprintln(out, "# HELP frankenphp_worker_requests_total Total requests processed per worker")
	fmt.Fprintln(out, "# TYPE frankenphp_worker_requests_total counter")
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "frankenphp_worker_requests_total{worker=\"%s\"} %g\n", escapeLabelValue(name), wm.RequestCount)
	}
}

func writeHostMetrics(w http.ResponseWriter, s *model.State) {
	if len(s.HostDerived) == 0 {
		return
	}

	hosts := sortedHostNames(s.HostDerived)

	fmt.Fprintln(w, "# HELP ember_host_rps Requests per second by host")
	fmt.Fprintln(w, "# TYPE ember_host_rps gauge")
	for _, hd := range hosts {
		fmt.Fprintf(w, "ember_host_rps{host=\"%s\"} %.2f\n", escapeLabelValue(hd.Host), hd.RPS)
	}

	fmt.Fprintln(w, "# HELP ember_host_latency_avg_milliseconds Average response time by host")
	fmt.Fprintln(w, "# TYPE ember_host_latency_avg_milliseconds gauge")
	for _, hd := range hosts {
		fmt.Fprintf(w, "ember_host_latency_avg_milliseconds{host=\"%s\"} %.2f\n", escapeLabelValue(hd.Host), hd.AvgTime)
	}

	hasPercentiles := false
	for _, hd := range hosts {
		if hd.HasPercentiles {
			hasPercentiles = true
			break
		}
	}
	if hasPercentiles {
		fmt.Fprintln(w, "# HELP ember_host_latency_milliseconds Response time percentiles by host")
		fmt.Fprintln(w, "# TYPE ember_host_latency_milliseconds gauge")
		for _, hd := range hosts {
			if !hd.HasPercentiles {
				continue
			}
			h := escapeLabelValue(hd.Host)
			fmt.Fprintf(w, "ember_host_latency_milliseconds{host=\"%s\",quantile=\"0.5\"} %.2f\n", h, hd.P50)
			fmt.Fprintf(w, "ember_host_latency_milliseconds{host=\"%s\",quantile=\"0.9\"} %.2f\n", h, hd.P90)
			fmt.Fprintf(w, "ember_host_latency_milliseconds{host=\"%s\",quantile=\"0.95\"} %.2f\n", h, hd.P95)
			fmt.Fprintf(w, "ember_host_latency_milliseconds{host=\"%s\",quantile=\"0.99\"} %.2f\n", h, hd.P99)
		}
	}

	fmt.Fprintln(w, "# HELP ember_host_inflight In-flight requests by host")
	fmt.Fprintln(w, "# TYPE ember_host_inflight gauge")
	for _, hd := range hosts {
		fmt.Fprintf(w, "ember_host_inflight{host=\"%s\"} %.0f\n", escapeLabelValue(hd.Host), hd.InFlight)
	}

	hasStatus := false
	for _, hd := range hosts {
		if len(hd.StatusCodes) > 0 {
			hasStatus = true
			break
		}
	}
	if hasStatus {
		fmt.Fprintln(w, "# HELP ember_host_status_rate Request rate by host and status class")
		fmt.Fprintln(w, "# TYPE ember_host_status_rate gauge")
		for _, hd := range hosts {
			classes := statusClassRates(hd.StatusCodes)
			h := escapeLabelValue(hd.Host)
			for _, c := range []string{"2xx", "3xx", "4xx", "5xx"} {
				if rate, ok := classes[c]; ok {
					fmt.Fprintf(w, "ember_host_status_rate{host=\"%s\",class=\"%s\"} %.2f\n", h, c, rate)
				}
			}
		}
	}
}

func statusClassRates(codes map[int]float64) map[string]float64 {
	if len(codes) == 0 {
		return nil
	}
	classes := make(map[string]float64)
	for code, rate := range codes {
		switch {
		case code >= 200 && code < 300:
			classes["2xx"] += rate
		case code >= 300 && code < 400:
			classes["3xx"] += rate
		case code >= 400 && code < 500:
			classes["4xx"] += rate
		case code >= 500 && code < 600:
			classes["5xx"] += rate
		}
	}
	return classes
}

func sortedHostNames(hosts []model.HostDerived) []model.HostDerived {
	sorted := make([]model.HostDerived, len(hosts))
	copy(sorted, hosts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Host < sorted[j].Host
	})
	return sorted
}

func writePercentiles(w http.ResponseWriter, s *model.State) {
	if !s.Derived.HasPercentiles {
		return
	}

	fmt.Fprintln(w, "# HELP frankenphp_request_duration_milliseconds Request duration percentiles")
	fmt.Fprintln(w, "# TYPE frankenphp_request_duration_milliseconds gauge")
	fmt.Fprintf(w, "frankenphp_request_duration_milliseconds{quantile=\"0.5\"} %.2f\n", s.Derived.P50)
	fmt.Fprintf(w, "frankenphp_request_duration_milliseconds{quantile=\"0.95\"} %.2f\n", s.Derived.P95)
	fmt.Fprintf(w, "frankenphp_request_duration_milliseconds{quantile=\"0.99\"} %.2f\n", s.Derived.P99)
}

func writeProcessMetrics(w http.ResponseWriter, s *model.State) {
	fmt.Fprintln(w, "# HELP process_cpu_percent CPU usage of the monitored process")
	fmt.Fprintln(w, "# TYPE process_cpu_percent gauge")
	fmt.Fprintf(w, "process_cpu_percent %.2f\n", s.Current.Process.CPUPercent)

	fmt.Fprintln(w, "# HELP process_rss_bytes Resident set size of the monitored process")
	fmt.Fprintln(w, "# TYPE process_rss_bytes gauge")
	fmt.Fprintf(w, "process_rss_bytes %d\n", s.Current.Process.RSS)
}

func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func sortedWorkerNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
