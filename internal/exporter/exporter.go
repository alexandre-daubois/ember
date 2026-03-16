package exporter

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

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

func Handler(holder *StateHolder, prefix ...string) http.HandlerFunc {
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		s := holder.Load()
		if s.Current == nil {
			http.Error(w, "no data yet", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", prometheusContentType)

		writeThreadMetrics(w, &s, p)
		writeThreadMemory(w, &s, p)
		writeWorkerMetrics(w, &s, p)
		writeHostMetrics(w, &s, p)
		writePercentiles(w, &s, p)
		writeProcessMetrics(w, &s, p)
	}
}

func prefixed(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "_" + name
}

func writeThreadMetrics(w http.ResponseWriter, s *model.State, prefix string) {
	total := len(s.Current.Threads.ThreadDebugStates)
	other := total - s.Derived.TotalBusy - s.Derived.TotalIdle
	if other < 0 {
		other = 0
	}

	name := prefixed(prefix, "frankenphp_threads_total")
	fmt.Fprintf(w, "# HELP %s Number of FrankenPHP threads by state\n", name)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s{state=\"busy\"} %d\n", name, s.Derived.TotalBusy)
	fmt.Fprintf(w, "%s{state=\"idle\"} %d\n", name, s.Derived.TotalIdle)
	fmt.Fprintf(w, "%s{state=\"other\"} %d\n", name, other)
}

func writeThreadMemory(w http.ResponseWriter, s *model.State, prefix string) {
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

	name := prefixed(prefix, "frankenphp_thread_memory_bytes")
	fmt.Fprintf(w, "# HELP %s Memory usage per FrankenPHP thread\n", name)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	for _, t := range s.Current.Threads.ThreadDebugStates {
		if t.MemoryUsage > 0 {
			fmt.Fprintf(w, "%s{index=\"%d\"} %d\n", name, t.Index, t.MemoryUsage)
		}
	}
}

func writeWorkerMetrics(out http.ResponseWriter, s *model.State, prefix string) {
	if len(s.Current.Metrics.Workers) == 0 {
		return
	}

	names := sortedWorkerNames(s.Current.Metrics.Workers)

	crashes := prefixed(prefix, "frankenphp_worker_crashes_total")
	fmt.Fprintf(out, "# HELP %s Total worker crashes\n", crashes)
	fmt.Fprintf(out, "# TYPE %s counter\n", crashes)
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "%s{worker=\"%s\"} %g\n", crashes, escapeLabelValue(name), wm.Crashes)
	}

	restarts := prefixed(prefix, "frankenphp_worker_restarts_total")
	fmt.Fprintf(out, "# HELP %s Total worker restarts\n", restarts)
	fmt.Fprintf(out, "# TYPE %s counter\n", restarts)
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "%s{worker=\"%s\"} %g\n", restarts, escapeLabelValue(name), wm.Restarts)
	}

	queue := prefixed(prefix, "frankenphp_worker_queue_depth")
	fmt.Fprintf(out, "# HELP %s Requests in queue per worker\n", queue)
	fmt.Fprintf(out, "# TYPE %s gauge\n", queue)
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "%s{worker=\"%s\"} %g\n", queue, escapeLabelValue(name), wm.QueueDepth)
	}

	reqs := prefixed(prefix, "frankenphp_worker_requests_total")
	fmt.Fprintf(out, "# HELP %s Total requests processed per worker\n", reqs)
	fmt.Fprintf(out, "# TYPE %s counter\n", reqs)
	for _, name := range names {
		wm := s.Current.Metrics.Workers[name]
		fmt.Fprintf(out, "%s{worker=\"%s\"} %g\n", reqs, escapeLabelValue(name), wm.RequestCount)
	}
}

func writeHostMetrics(w http.ResponseWriter, s *model.State, prefix string) {
	if len(s.HostDerived) == 0 {
		return
	}

	hosts := sortedHostNames(s.HostDerived)

	rps := prefixed(prefix, "ember_host_rps")
	fmt.Fprintf(w, "# HELP %s Requests per second by host\n", rps)
	fmt.Fprintf(w, "# TYPE %s gauge\n", rps)
	for _, hd := range hosts {
		fmt.Fprintf(w, "%s{host=\"%s\"} %.2f\n", rps, escapeLabelValue(hd.Host), hd.RPS)
	}

	avg := prefixed(prefix, "ember_host_latency_avg_milliseconds")
	fmt.Fprintf(w, "# HELP %s Average response time by host\n", avg)
	fmt.Fprintf(w, "# TYPE %s gauge\n", avg)
	for _, hd := range hosts {
		fmt.Fprintf(w, "%s{host=\"%s\"} %.2f\n", avg, escapeLabelValue(hd.Host), hd.AvgTime)
	}

	hasPercentiles := false
	for _, hd := range hosts {
		if hd.HasPercentiles {
			hasPercentiles = true
			break
		}
	}
	if hasPercentiles {
		lat := prefixed(prefix, "ember_host_latency_milliseconds")
		fmt.Fprintf(w, "# HELP %s Response time percentiles by host\n", lat)
		fmt.Fprintf(w, "# TYPE %s gauge\n", lat)
		for _, hd := range hosts {
			if !hd.HasPercentiles {
				continue
			}
			h := escapeLabelValue(hd.Host)
			fmt.Fprintf(w, "%s{host=\"%s\",quantile=\"0.5\"} %.2f\n", lat, h, hd.P50)
			fmt.Fprintf(w, "%s{host=\"%s\",quantile=\"0.9\"} %.2f\n", lat, h, hd.P90)
			fmt.Fprintf(w, "%s{host=\"%s\",quantile=\"0.95\"} %.2f\n", lat, h, hd.P95)
			fmt.Fprintf(w, "%s{host=\"%s\",quantile=\"0.99\"} %.2f\n", lat, h, hd.P99)
		}
	}

	infl := prefixed(prefix, "ember_host_inflight")
	fmt.Fprintf(w, "# HELP %s In-flight requests by host\n", infl)
	fmt.Fprintf(w, "# TYPE %s gauge\n", infl)
	for _, hd := range hosts {
		fmt.Fprintf(w, "%s{host=\"%s\"} %.0f\n", infl, escapeLabelValue(hd.Host), hd.InFlight)
	}

	hasStatus := false
	for _, hd := range hosts {
		if len(hd.StatusCodes) > 0 {
			hasStatus = true
			break
		}
	}
	if hasStatus {
		sr := prefixed(prefix, "ember_host_status_rate")
		fmt.Fprintf(w, "# HELP %s Request rate by host and status class\n", sr)
		fmt.Fprintf(w, "# TYPE %s gauge\n", sr)
		for _, hd := range hosts {
			classes := statusClassRates(hd.StatusCodes)
			h := escapeLabelValue(hd.Host)
			for _, c := range []string{"2xx", "3xx", "4xx", "5xx"} {
				if rate, ok := classes[c]; ok {
					fmt.Fprintf(w, "%s{host=\"%s\",class=\"%s\"} %.2f\n", sr, h, c, rate)
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
	slices.SortFunc(sorted, func(a, b model.HostDerived) int {
		return cmp.Compare(a.Host, b.Host)
	})
	return sorted
}

func writePercentiles(w http.ResponseWriter, s *model.State, prefix string) {
	if !s.Derived.HasPercentiles {
		return
	}

	name := prefixed(prefix, "frankenphp_request_duration_milliseconds")
	fmt.Fprintf(w, "# HELP %s Request duration percentiles\n", name)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s{quantile=\"0.5\"} %.2f\n", name, s.Derived.P50)
	fmt.Fprintf(w, "%s{quantile=\"0.95\"} %.2f\n", name, s.Derived.P95)
	fmt.Fprintf(w, "%s{quantile=\"0.99\"} %.2f\n", name, s.Derived.P99)
}

func writeProcessMetrics(w http.ResponseWriter, s *model.State, prefix string) {
	cpu := prefixed(prefix, "process_cpu_percent")
	fmt.Fprintf(w, "# HELP %s CPU usage of the monitored process\n", cpu)
	fmt.Fprintf(w, "# TYPE %s gauge\n", cpu)
	fmt.Fprintf(w, "%s %.2f\n", cpu, s.Current.Process.CPUPercent)

	rss := prefixed(prefix, "process_rss_bytes")
	fmt.Fprintf(w, "# HELP %s Resident set size of the monitored process\n", rss)
	fmt.Fprintf(w, "# TYPE %s gauge\n", rss)
	fmt.Fprintf(w, "%s %d\n", rss, s.Current.Process.RSS)
}

func HealthHandler(holder *StateHolder, interval time.Duration) http.HandlerFunc {
	staleThreshold := 3 * interval
	if staleThreshold < 5*time.Second {
		staleThreshold = 5 * time.Second
	}

	enc := func(w http.ResponseWriter, v any) {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		s := holder.Load()
		w.Header().Set("Content-Type", "application/json")

		if s.Current == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			enc(w, map[string]string{"status": "no data yet"})
			return
		}

		age := time.Since(s.Current.FetchedAt)
		if age > staleThreshold {
			w.WriteHeader(http.StatusServiceUnavailable)
			enc(w, map[string]any{
				"status":      "stale",
				"last_fetch":  s.Current.FetchedAt.Format(time.RFC3339),
				"age_seconds": age.Seconds(),
			})
			return
		}

		enc(w, map[string]any{
			"status":      "ok",
			"last_fetch":  s.Current.FetchedAt.Format(time.RFC3339),
			"age_seconds": age.Seconds(),
		})
	}
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
	slices.Sort(names)
	return names
}
