package exporter

import (
	"cmp"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/pkg/metrics"
	"github.com/alexandre-daubois/ember/pkg/plugin"
)

// instanceSlot holds the latest snapshot for a single Caddy instance. In
// single-instance mode the slot is keyed by the empty string so the
// ember_instance label is omitted from emitted metrics.
type instanceSlot struct {
	addr          string
	state         model.State
	pluginExports []plugin.PluginExport
	recorder      *instrumentation.Recorder
}

type StateHolder struct {
	mu        sync.RWMutex
	instances map[string]*instanceSlot
	multi     bool
}

func (h *StateHolder) put(name string, slot *instanceSlot) {
	h.mu.Lock()
	if h.instances == nil {
		h.instances = make(map[string]*instanceSlot)
	}
	h.instances[name] = slot
	h.mu.Unlock()
}

func (h *StateHolder) Store(s model.State) {
	h.put("", &instanceSlot{state: s})
}

func (h *StateHolder) StoreAll(s model.State, exports []plugin.PluginExport) {
	h.put("", &instanceSlot{state: s, pluginExports: exports})
}

func (h *StateHolder) Load() model.State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if slot, ok := h.instances[""]; ok {
		return slot.state
	}
	return model.State{}
}

func (h *StateHolder) SetMulti(multi bool) {
	h.mu.Lock()
	h.multi = multi
	h.mu.Unlock()
}

func (h *StateHolder) StoreInstance(name, addr string, s model.State, exports []plugin.PluginExport, recorder *instrumentation.Recorder) {
	h.put(name, &instanceSlot{addr: addr, state: s, pluginExports: exports, recorder: recorder})
}

type instanceEntry struct {
	name string
	slot *instanceSlot
}

func (h *StateHolder) lookup(name string) (*instanceSlot, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.instances[name], h.multi
}

func (h *StateHolder) entries() ([]instanceEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	entries := make([]instanceEntry, 0, len(h.instances))
	for name, slot := range h.instances {
		entries = append(entries, instanceEntry{name: name, slot: slot})
	}
	slices.SortFunc(entries, func(a, b instanceEntry) int { return cmp.Compare(a.name, b.name) })
	return entries, h.multi
}

const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

type renderEntry struct {
	instance string
	state    *model.State
}

// metricCtx tracks which metric families have already received their HELP/TYPE
// lines so a single family rendered for several instances stays valid Prometheus
// text.
type metricCtx struct {
	out      io.Writer
	prefix   string
	helpSeen map[string]bool
}

func newMetricCtx(w io.Writer, prefix string) *metricCtx {
	return &metricCtx{out: w, prefix: prefix, helpSeen: make(map[string]bool)}
}

func (c *metricCtx) help(name, help, typ string) {
	if c.helpSeen[name] {
		return
	}
	c.helpSeen[name] = true
	fmt.Fprintf(c.out, "# HELP %s %s\n", name, help)
	fmt.Fprintf(c.out, "# TYPE %s %s\n", name, typ)
}

// Handler returns an /metrics handler. prefix may be "" for unprefixed names.
// fallbackRecorder is consulted only in single-instance mode when no recorder
// has been associated with the slot via StoreInstance; it preserves the
// pre-multi-instance API used by the TUI and the tests.
func Handler(holder *StateHolder, prefix string, fallbackRecorder *instrumentation.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, multi := holder.entries()
		render := make([]renderEntry, 0, len(entries))
		for _, e := range entries {
			if e.slot.state.Current == nil {
				continue
			}
			label := ""
			if multi {
				label = e.name
			}
			render = append(render, renderEntry{instance: label, state: &e.slot.state})
		}
		if len(render) == 0 {
			http.Error(w, "no data yet", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", prometheusContentType)
		ctx := newMetricCtx(w, prefix)

		writeThreadMetrics(ctx, render)
		writeThreadMemory(ctx, render)
		writeWorkerMetrics(ctx, render)
		writeHostMetrics(ctx, render)
		writeErrorMetrics(ctx, render)
		writePercentiles(ctx, render)
		writeProcessMetrics(ctx, render)

		writeAllSelfMetrics(ctx, entries, multi, fallbackRecorder)

		for _, e := range entries {
			for _, pe := range e.slot.pluginExports {
				if pe.Exporter != nil && pe.Data != nil {
					safeWriteMetrics(w, pe.Exporter, pe.Data, prefix)
				}
			}
		}
	}
}

func safeWriteMetrics(w http.ResponseWriter, e plugin.Exporter, data any, prefix string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(w, "# plugin WriteMetrics panic: %v\n", r)
		}
	}()
	e.WriteMetrics(w, data, prefix)
}

func prefixed(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "_" + name
}

// instLabelKV returns the key/value fragment ember_instance="name" for use
// inside an existing label set, or "" when no instance label should be emitted.
func instLabelKV(instance string) string {
	if instance == "" {
		return ""
	}
	return `ember_instance="` + escapeLabelValue(instance) + `"`
}

// labels assembles a Prometheus label set from optional fragments. Empty
// fragments are dropped; the result is "" or "{...}".
func labels(parts ...string) string {
	keep := parts[:0]
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	if len(keep) == 0 {
		return ""
	}
	return "{" + strings.Join(keep, ",") + "}"
}

func writeThreadMetrics(ctx *metricCtx, entries []renderEntry) {
	name := prefixed(ctx.prefix, "frankenphp_threads_total")
	ctx.help(name, "Number of FrankenPHP threads by state", "gauge")
	for _, e := range entries {
		s := e.state
		total := len(s.Current.Threads.ThreadDebugStates)
		other := total - s.Derived.TotalBusy - s.Derived.TotalIdle
		if other < 0 {
			other = 0
		}
		inst := instLabelKV(e.instance)
		fmt.Fprintf(ctx.out, "%s%s %d\n", name, labels(`state="busy"`, inst), s.Derived.TotalBusy)
		fmt.Fprintf(ctx.out, "%s%s %d\n", name, labels(`state="idle"`, inst), s.Derived.TotalIdle)
		fmt.Fprintf(ctx.out, "%s%s %d\n", name, labels(`state="other"`, inst), other)
	}
}

func writeThreadMemory(ctx *metricCtx, entries []renderEntry) {
	hasAny := false
	for _, e := range entries {
		for _, t := range e.state.Current.Threads.ThreadDebugStates {
			if t.MemoryUsage > 0 {
				hasAny = true
				break
			}
		}
		if hasAny {
			break
		}
	}
	if !hasAny {
		return
	}

	name := prefixed(ctx.prefix, "frankenphp_thread_memory_bytes")
	ctx.help(name, "Memory usage per FrankenPHP thread", "gauge")
	for _, e := range entries {
		inst := instLabelKV(e.instance)
		for _, t := range e.state.Current.Threads.ThreadDebugStates {
			if t.MemoryUsage > 0 {
				fmt.Fprintf(ctx.out, "%s%s %d\n", name, labels(fmt.Sprintf(`index="%d"`, t.Index), inst), t.MemoryUsage)
			}
		}
	}
}

func writeWorkerMetrics(ctx *metricCtx, entries []renderEntry) {
	hasAny := false
	for _, e := range entries {
		if len(e.state.Current.Metrics.Workers) > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}

	emitWorkers := func(metricName, helpText string, get func(*metrics.WorkerMetrics) float64) {
		full := prefixed(ctx.prefix, metricName)
		ctx.help(full, helpText, workerCounterType(metricName))
		for _, e := range entries {
			workers := e.state.Current.Metrics.Workers
			if len(workers) == 0 {
				continue
			}
			inst := instLabelKV(e.instance)
			for _, name := range sortedWorkerNames(workers) {
				wm := workers[name]
				fmt.Fprintf(ctx.out, "%s%s %g\n", full, labels(fmt.Sprintf(`worker="%s"`, escapeLabelValue(name)), inst), get(wm))
			}
		}
	}

	emitWorkers("frankenphp_worker_crashes_total", "Total worker crashes", func(wm *metrics.WorkerMetrics) float64 { return wm.Crashes })
	emitWorkers("frankenphp_worker_restarts_total", "Total worker restarts", func(wm *metrics.WorkerMetrics) float64 { return wm.Restarts })
	emitWorkers("frankenphp_worker_queue_depth", "Requests in queue per worker", func(wm *metrics.WorkerMetrics) float64 { return wm.QueueDepth })
	emitWorkers("frankenphp_worker_requests_total", "Total requests processed per worker", func(wm *metrics.WorkerMetrics) float64 { return wm.RequestCount })
}

func workerCounterType(name string) string {
	if strings.HasSuffix(name, "_depth") {
		return "gauge"
	}
	return "counter"
}

type hostView struct {
	instance string
	hosts    []model.HostDerived
}

func writeHostMetrics(ctx *metricCtx, entries []renderEntry) {
	views := make([]hostView, 0, len(entries))
	for _, e := range entries {
		if len(e.state.HostDerived) == 0 {
			continue
		}
		views = append(views, hostView{instance: e.instance, hosts: sortedHostNames(e.state.HostDerived)})
	}
	if len(views) == 0 {
		return
	}

	rps := prefixed(ctx.prefix, "ember_host_rps")
	ctx.help(rps, "Requests per second by host", "gauge")
	for _, v := range views {
		inst := instLabelKV(v.instance)
		for _, hd := range v.hosts {
			fmt.Fprintf(ctx.out, "%s%s %.2f\n", rps, labels(hostKV(hd.Host), inst), hd.RPS)
		}
	}

	avg := prefixed(ctx.prefix, "ember_host_latency_avg_milliseconds")
	ctx.help(avg, "Average response time by host", "gauge")
	for _, v := range views {
		inst := instLabelKV(v.instance)
		for _, hd := range v.hosts {
			fmt.Fprintf(ctx.out, "%s%s %.2f\n", avg, labels(hostKV(hd.Host), inst), hd.AvgTime)
		}
	}

	hasPercentiles := false
	for _, v := range views {
		for _, hd := range v.hosts {
			if hd.HasPercentiles {
				hasPercentiles = true
				break
			}
		}
		if hasPercentiles {
			break
		}
	}
	if hasPercentiles {
		lat := prefixed(ctx.prefix, "ember_host_latency_milliseconds")
		ctx.help(lat, "Response time percentiles by host", "gauge")
		for _, v := range views {
			inst := instLabelKV(v.instance)
			for _, hd := range v.hosts {
				if !hd.HasPercentiles {
					continue
				}
				h := hostKV(hd.Host)
				fmt.Fprintf(ctx.out, "%s%s %.2f\n", lat, labels(h, `quantile="0.5"`, inst), hd.P50)
				fmt.Fprintf(ctx.out, "%s%s %.2f\n", lat, labels(h, `quantile="0.9"`, inst), hd.P90)
				fmt.Fprintf(ctx.out, "%s%s %.2f\n", lat, labels(h, `quantile="0.95"`, inst), hd.P95)
				fmt.Fprintf(ctx.out, "%s%s %.2f\n", lat, labels(h, `quantile="0.99"`, inst), hd.P99)
			}
		}
	}

	infl := prefixed(ctx.prefix, "ember_host_inflight")
	ctx.help(infl, "In-flight requests by host", "gauge")
	for _, v := range views {
		inst := instLabelKV(v.instance)
		for _, hd := range v.hosts {
			fmt.Fprintf(ctx.out, "%s%s %.0f\n", infl, labels(hostKV(hd.Host), inst), hd.InFlight)
		}
	}

	hasStatus := false
	for _, v := range views {
		for _, hd := range v.hosts {
			if len(hd.StatusCodes) > 0 {
				hasStatus = true
				break
			}
		}
		if hasStatus {
			break
		}
	}
	if hasStatus {
		sr := prefixed(ctx.prefix, "ember_host_status_rate")
		ctx.help(sr, "Request rate by host and status class", "gauge")
		for _, v := range views {
			inst := instLabelKV(v.instance)
			for _, hd := range v.hosts {
				classes := statusClassRates(hd.StatusCodes)
				h := hostKV(hd.Host)
				for _, c := range []string{"2xx", "3xx", "4xx", "5xx"} {
					if rate, ok := classes[c]; ok {
						fmt.Fprintf(ctx.out, "%s%s %.2f\n", sr, labels(h, `class="`+c+`"`, inst), rate)
					}
				}
			}
		}
	}
}

func hostKV(host string) string {
	return `host="` + escapeLabelValue(host) + `"`
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

func writeErrorMetrics(ctx *metricCtx, entries []renderEntry) {
	views := make([]hostView, 0, len(entries))
	for _, e := range entries {
		hasErr := false
		for _, hd := range e.state.HostDerived {
			if hd.ErrorRate > 0 {
				hasErr = true
				break
			}
		}
		if !hasErr {
			continue
		}
		views = append(views, hostView{instance: e.instance, hosts: sortedHostNames(e.state.HostDerived)})
	}
	if len(views) == 0 {
		return
	}

	name := prefixed(ctx.prefix, "ember_host_error_rate")
	ctx.help(name, "Middleware error rate by host", "gauge")
	for _, v := range views {
		inst := instLabelKV(v.instance)
		for _, hd := range v.hosts {
			if hd.ErrorRate > 0 {
				fmt.Fprintf(ctx.out, "%s%s %.2f\n", name, labels(hostKV(hd.Host), inst), hd.ErrorRate)
			}
		}
	}
}

func writePercentiles(ctx *metricCtx, entries []renderEntry) {
	hasAny := false
	for _, e := range entries {
		if e.state.Derived.HasPercentiles {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}

	name := prefixed(ctx.prefix, "frankenphp_request_duration_milliseconds")
	ctx.help(name, "Request duration percentiles", "gauge")
	for _, e := range entries {
		if !e.state.Derived.HasPercentiles {
			continue
		}
		inst := instLabelKV(e.instance)
		fmt.Fprintf(ctx.out, "%s%s %.2f\n", name, labels(`quantile="0.5"`, inst), e.state.Derived.P50)
		fmt.Fprintf(ctx.out, "%s%s %.2f\n", name, labels(`quantile="0.9"`, inst), e.state.Derived.P90)
		fmt.Fprintf(ctx.out, "%s%s %.2f\n", name, labels(`quantile="0.95"`, inst), e.state.Derived.P95)
		fmt.Fprintf(ctx.out, "%s%s %.2f\n", name, labels(`quantile="0.99"`, inst), e.state.Derived.P99)
	}
}

func writeProcessMetrics(ctx *metricCtx, entries []renderEntry) {
	cpu := prefixed(ctx.prefix, "process_cpu_percent")
	ctx.help(cpu, "CPU usage of the monitored process", "gauge")
	for _, e := range entries {
		fmt.Fprintf(ctx.out, "%s%s %.2f\n", cpu, labels(instLabelKV(e.instance)), e.state.Current.Process.CPUPercent)
	}

	rss := prefixed(ctx.prefix, "process_rss_bytes")
	ctx.help(rss, "Resident set size of the monitored process", "gauge")
	for _, e := range entries {
		fmt.Fprintf(ctx.out, "%s%s %d\n", rss, labels(instLabelKV(e.instance)), e.state.Current.Process.RSS)
	}
}

// writeAllSelfMetrics emits ember_build_info once and per-stage scrape metrics
// for each instance that has a recorder. In single-instance mode the
// fallbackRecorder is consulted when the slot has none, preserving the pre
// multi-instance handler API.
func writeAllSelfMetrics(ctx *metricCtx, entries []instanceEntry, multi bool, fallback *instrumentation.Recorder) {
	type recorded struct {
		instance string
		snap     instrumentation.Snapshot
	}
	var snaps []recorded
	for _, e := range entries {
		if e.slot.state.Current == nil {
			continue
		}
		rec := e.slot.recorder
		if rec == nil && !multi {
			rec = fallback
		}
		if rec == nil {
			continue
		}
		instance := ""
		if multi {
			instance = e.name
		}
		snaps = append(snaps, recorded{instance: instance, snap: rec.Snapshot()})
	}
	if len(snaps) == 0 {
		return
	}

	build := prefixed(ctx.prefix, "ember_build_info")
	ctx.help(build, "Ember build information; the value is always 1", "gauge")
	first := snaps[0].snap
	fmt.Fprintf(ctx.out, "%s{version=\"%s\",goversion=\"%s\"} 1\n",
		build, escapeLabelValue(first.Version), escapeLabelValue(first.GoVersion))

	if len(first.Stages) == 0 {
		return
	}

	total := prefixed(ctx.prefix, "ember_scrape_total")
	ctx.help(total, "Total scrape attempts per stage (success + error)", "counter")
	for _, r := range snaps {
		inst := instLabelKV(r.instance)
		for _, s := range r.snap.Stages {
			fmt.Fprintf(ctx.out, "%s%s %d\n", total, labels(fmt.Sprintf(`stage="%s"`, escapeLabelValue(s.Stage)), inst), s.Total)
		}
	}

	errs := prefixed(ctx.prefix, "ember_scrape_errors_total")
	ctx.help(errs, "Failed scrape attempts per stage", "counter")
	for _, r := range snaps {
		inst := instLabelKV(r.instance)
		for _, s := range r.snap.Stages {
			fmt.Fprintf(ctx.out, "%s%s %d\n", errs, labels(fmt.Sprintf(`stage="%s"`, escapeLabelValue(s.Stage)), inst), s.Errors)
		}
	}

	dur := prefixed(ctx.prefix, "ember_scrape_duration_seconds")
	ctx.help(dur, "Duration of the last scrape attempt per stage, in seconds", "gauge")
	for _, r := range snaps {
		inst := instLabelKV(r.instance)
		for _, s := range r.snap.Stages {
			fmt.Fprintf(ctx.out, "%s%s %.6f\n", dur, labels(fmt.Sprintf(`stage="%s"`, escapeLabelValue(s.Stage)), inst), s.LastDuration.Seconds())
		}
	}

	last := prefixed(ctx.prefix, "ember_last_successful_scrape_timestamp_seconds")
	ctx.help(last, "Unix timestamp of the last successful scrape per stage; 0 means none yet", "gauge")
	for _, r := range snaps {
		inst := instLabelKV(r.instance)
		for _, s := range r.snap.Stages {
			var ts float64
			if !s.LastSuccessAt.IsZero() {
				ts = float64(s.LastSuccessAt.UnixNano()) / 1e9
			}
			fmt.Fprintf(ctx.out, "%s%s %.3f\n", last, labels(fmt.Sprintf(`stage="%s"`, escapeLabelValue(s.Stage)), inst), ts)
		}
	}
}

const (
	healthOK        = "ok"
	healthStale     = "stale"
	healthNoDataYet = "no data yet"
)

type healthInstanceBody struct {
	Name       string  `json:"name"`
	Addr       string  `json:"addr,omitempty"`
	Status     string  `json:"status"`
	LastFetch  string  `json:"last_fetch,omitempty"`
	AgeSeconds float64 `json:"age_seconds,omitempty"`
}

// instanceHealth derives a status string and the timing fields used in the
// /healthz body from a slot's state. Returns ("", "", 0) when status is
// healthNoDataYet so callers can omit the timing keys.
func instanceHealth(slot *instanceSlot, staleThreshold time.Duration) (status, lastFetch string, age float64) {
	if slot == nil || slot.state.Current == nil {
		return healthNoDataYet, "", 0
	}
	d := time.Since(slot.state.Current.FetchedAt)
	if d > staleThreshold {
		return healthStale, slot.state.Current.FetchedAt.Format(time.RFC3339), d.Seconds()
	}
	return healthOK, slot.state.Current.FetchedAt.Format(time.RFC3339), d.Seconds()
}

// healthSeverity orders statuses so callers can compute the worst across
// instances: ok < stale < no data yet.
func healthSeverity(status string) int {
	switch status {
	case healthStale:
		return 1
	case healthNoDataYet:
		return 2
	default:
		return 0
	}
}

func staleThresholdFor(interval time.Duration) time.Duration {
	return max(3*interval, 5*time.Second)
}

func encodeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeSingleHealthBody(w http.ResponseWriter, slot *instanceSlot, threshold time.Duration) {
	status, lastFetch, age := instanceHealth(slot, threshold)
	if status != healthOK {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	body := map[string]any{"status": status}
	if lastFetch != "" {
		body["last_fetch"] = lastFetch
		body["age_seconds"] = age
	}
	encodeJSON(w, body)
}

func HealthHandler(holder *StateHolder, interval time.Duration) http.HandlerFunc {
	threshold := staleThresholdFor(interval)

	return func(w http.ResponseWriter, r *http.Request) {
		entries, multi := holder.entries()
		w.Header().Set("Content-Type", "application/json")

		if !multi {
			var slot *instanceSlot
			if len(entries) > 0 {
				slot = entries[0].slot
			}
			writeSingleHealthBody(w, slot, threshold)
			return
		}

		bodies := make([]healthInstanceBody, 0, len(entries))
		worst := healthOK
		for _, e := range entries {
			status, lastFetch, age := instanceHealth(e.slot, threshold)
			bodies = append(bodies, healthInstanceBody{
				Name:       e.name,
				Addr:       e.slot.addr,
				Status:     status,
				LastFetch:  lastFetch,
				AgeSeconds: age,
			})
			if healthSeverity(status) > healthSeverity(worst) {
				worst = status
			}
		}
		if worst != healthOK {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		encodeJSON(w, map[string]any{
			"status":    worst,
			"instances": bodies,
		})
	}
}

// InstanceHealthHandler serves /healthz/<name>: per-instance readiness probe
// returning the same body shape as the single-instance /healthz. Returns 404
// outside multi-instance mode, when the name is unknown, or when the path has
// no name or extra segments.
func InstanceHealthHandler(holder *StateHolder, interval time.Duration) http.HandlerFunc {
	threshold := staleThresholdFor(interval)

	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/healthz/")
		if name == "" || strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}
		slot, multi := holder.lookup(name)
		if !multi || slot == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeSingleHealthBody(w, slot, threshold)
	}
}

func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// BasicAuth wraps an http.Handler with HTTP Basic Authentication.
// It uses constant-time comparison to prevent timing attacks.
func BasicAuth(next http.Handler, user, pass string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 || subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="ember metrics"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sortedWorkerNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
