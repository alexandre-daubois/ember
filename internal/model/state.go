package model

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

const percentileExpiry = defaultPercentileExpiry

type SortField int

const (
	SortByIndex SortField = iota
	SortByState
	SortByMethod
	SortByURI
	SortByTime
	SortByMemory
	SortByRequests
)

var sortFieldOrder = []SortField{SortByIndex, SortByState, SortByMethod, SortByURI, SortByTime, SortByMemory, SortByRequests}

func (s SortField) String() string {
	switch s {
	case SortByState:
		return "state"
	case SortByMethod:
		return "method"
	case SortByURI:
		return "uri"
	case SortByTime:
		return "time"
	case SortByMemory:
		return "memory"
	case SortByRequests:
		return "requests"
	default:
		return "index"
	}
}

func (s SortField) Next() SortField {
	for i, f := range sortFieldOrder {
		if f == s {
			return sortFieldOrder[(i+1)%len(sortFieldOrder)]
		}
	}
	return SortByIndex
}

func (s SortField) Prev() SortField {
	for i, f := range sortFieldOrder {
		if f == s {
			return sortFieldOrder[(i-1+len(sortFieldOrder))%len(sortFieldOrder)]
		}
	}
	return SortByIndex
}

type HostSortField int

const (
	SortByHost HostSortField = iota
	SortByHostRPS
	SortByHostAvg
	SortByHostInFlight
	SortByHost2xx
	SortByHost4xx
	SortByHost5xx
)

var hostSortFieldOrder = []HostSortField{
	SortByHost, SortByHostRPS, SortByHostAvg,
	SortByHostInFlight, SortByHost2xx, SortByHost4xx, SortByHost5xx,
}

func (s HostSortField) String() string {
	switch s {
	case SortByHostRPS:
		return "rps"
	case SortByHostAvg:
		return "avg"
	case SortByHostInFlight:
		return "in-flight"
	case SortByHost2xx:
		return "2xx"
	case SortByHost4xx:
		return "4xx"
	case SortByHost5xx:
		return "5xx"
	default:
		return "host"
	}
}

func (s HostSortField) Next() HostSortField {
	for i, f := range hostSortFieldOrder {
		if f == s {
			return hostSortFieldOrder[(i+1)%len(hostSortFieldOrder)]
		}
	}
	return SortByHost
}

func (s HostSortField) Prev() HostSortField {
	for i, f := range hostSortFieldOrder {
		if f == s {
			return hostSortFieldOrder[(i-1+len(hostSortFieldOrder))%len(hostSortFieldOrder)]
		}
	}
	return SortByHost
}

type CertSortField int

const (
	SortByCertDomain CertSortField = iota
	SortByCertExpiry
	SortByCertSource
	SortByCertIssuer
)

var certSortFieldOrder = []CertSortField{
	SortByCertDomain, SortByCertExpiry, SortByCertSource, SortByCertIssuer,
}

func (s CertSortField) String() string {
	switch s {
	case SortByCertExpiry:
		return "expiry"
	case SortByCertSource:
		return "source"
	case SortByCertIssuer:
		return "issuer"
	default:
		return "domain"
	}
}

func (s CertSortField) Next() CertSortField {
	for i, f := range certSortFieldOrder {
		if f == s {
			return certSortFieldOrder[(i+1)%len(certSortFieldOrder)]
		}
	}
	return SortByCertDomain
}

func (s CertSortField) Prev() CertSortField {
	for i, f := range certSortFieldOrder {
		if f == s {
			return certSortFieldOrder[(i-1+len(certSortFieldOrder))%len(certSortFieldOrder)]
		}
	}
	return SortByCertDomain
}

type UpstreamSortField int

const (
	SortByUpstreamAddress UpstreamSortField = iota
	SortByUpstreamHealth
)

var upstreamSortFieldOrder = []UpstreamSortField{
	SortByUpstreamAddress, SortByUpstreamHealth,
}

func (s UpstreamSortField) String() string {
	switch s {
	case SortByUpstreamHealth:
		return "health"
	default:
		return "address"
	}
}

func (s UpstreamSortField) Next() UpstreamSortField {
	for i, f := range upstreamSortFieldOrder {
		if f == s {
			return upstreamSortFieldOrder[(i+1)%len(upstreamSortFieldOrder)]
		}
	}
	return SortByUpstreamAddress
}

func (s UpstreamSortField) Prev() UpstreamSortField {
	for i, f := range upstreamSortFieldOrder {
		if f == s {
			return upstreamSortFieldOrder[(i-1+len(upstreamSortFieldOrder))%len(upstreamSortFieldOrder)]
		}
	}
	return SortByUpstreamAddress
}

type UpstreamDerived struct {
	Address       string
	Handler       string
	Healthy       bool
	HealthChanged bool
}

type HostDerived struct {
	Host                               string
	RPS                                float64
	AvgTime                            float64
	ErrorRate                          float64
	InFlight                           float64
	P50, P90, P95, P99                 float64
	HasPercentiles                     bool
	TTFBP50, TTFBP90, TTFBP95, TTFBP99 float64
	HasTTFB                            bool
	StatusCodes                        map[int]float64
	MethodRates                        map[string]float64
	AvgResponseSize                    float64
	AvgRequestSize                     float64
	TotalRequests                      float64
}

type State struct {
	Current         *fetcher.Snapshot
	Previous        *fetcher.Snapshot
	Derived         DerivedMetrics
	HostDerived     []HostDerived
	UpstreamDerived []UpstreamDerived
	percentiles     *percentileTracker
}

type DerivedMetrics struct {
	RPS            float64
	AvgTime        float64
	ErrorRate      float64
	TotalIdle      int
	TotalBusy      int
	TotalCrashes   float64
	P50            float64
	P90            float64
	P95            float64
	P99            float64
	HasPercentiles bool
}

func (s *State) ResetPercentiles() {
	if s.percentiles != nil {
		s.percentiles.reset()
	}
}

// CopyForExport returns a copy safe for concurrent read from the exporter.
func (s *State) CopyForExport() State {
	cp := *s
	cp.percentiles = nil
	cp.Previous = nil
	if s.Current != nil {
		snap := *s.Current
		snap.Threads.ThreadDebugStates = slices.Clone(snap.Threads.ThreadDebugStates)
		snap.Metrics.DurationBuckets = slices.Clone(snap.Metrics.DurationBuckets)
		snap.Errors = slices.Clone(snap.Errors)
		if snap.Metrics.Workers != nil {
			workers := make(map[string]*fetcher.WorkerMetrics, len(snap.Metrics.Workers))
			for k, v := range snap.Metrics.Workers {
				wc := *v
				workers[k] = &wc
			}
			snap.Metrics.Workers = workers
		}
		if snap.Metrics.Hosts != nil {
			hosts := make(map[string]*fetcher.HostMetrics, len(snap.Metrics.Hosts))
			for k, v := range snap.Metrics.Hosts {
				hc := *v
				if v.StatusCodes != nil {
					hc.StatusCodes = make(map[int]float64, len(v.StatusCodes))
					maps.Copy(hc.StatusCodes, v.StatusCodes)
				}
				if v.Methods != nil {
					hc.Methods = make(map[string]float64, len(v.Methods))
					maps.Copy(hc.Methods, v.Methods)
				}
				hc.DurationBuckets = slices.Clone(v.DurationBuckets)
				hc.TTFBBuckets = slices.Clone(v.TTFBBuckets)
				hosts[k] = &hc
			}
			snap.Metrics.Hosts = hosts
		}
		if snap.Metrics.Upstreams != nil {
			upstreams := make(map[string]*fetcher.UpstreamMetrics, len(snap.Metrics.Upstreams))
			for k, v := range snap.Metrics.Upstreams {
				uc := *v
				upstreams[k] = &uc
			}
			snap.Metrics.Upstreams = upstreams
		}
		cp.Current = &snap
	}
	if s.HostDerived != nil {
		cp.HostDerived = make([]HostDerived, len(s.HostDerived))
		for i, hd := range s.HostDerived {
			cp.HostDerived[i] = hd
			if hd.StatusCodes != nil {
				cp.HostDerived[i].StatusCodes = make(map[int]float64, len(hd.StatusCodes))
				maps.Copy(cp.HostDerived[i].StatusCodes, hd.StatusCodes)
			}
			if hd.MethodRates != nil {
				cp.HostDerived[i].MethodRates = make(map[string]float64, len(hd.MethodRates))
				maps.Copy(cp.HostDerived[i].MethodRates, hd.MethodRates)
			}
		}
	}
	cp.UpstreamDerived = slices.Clone(s.UpstreamDerived)
	return cp
}

func (s *State) Update(snap *fetcher.Snapshot) {
	if s.percentiles == nil {
		s.percentiles = newPercentileTracker(percentileExpiry)
	}

	if s.detectCounterReset(snap) {
		s.percentiles.reset()
		s.Previous = nil
		s.Current = snap
		s.Derived = s.computeDerived()
		s.HostDerived = s.computeHostDerived()
		s.UpstreamDerived = s.computeUpstreamDerived()
		return
	}

	s.detectCompletedRequests(snap)
	s.Previous = s.Current
	s.Current = snap
	s.Derived = s.computeDerived()
	s.HostDerived = s.computeHostDerived()
	s.UpstreamDerived = s.computeUpstreamDerived()
}

// detectCounterReset returns true when cumulative Prometheus counters have
// decreased, which indicates that Caddy (or FrankenPHP) was restarted.
// Discarding Previous on reset avoids negative deltas in derived metrics.
func (s *State) detectCounterReset(snap *fetcher.Snapshot) bool {
	if s.Current == nil {
		return false
	}
	if s.Current.Metrics.HTTPRequestDurationCount > 0 && snap.Metrics.HTTPRequestDurationCount < s.Current.Metrics.HTTPRequestDurationCount {
		return true
	}
	if s.Current.Metrics.HTTPRequestsTotal > 0 && snap.Metrics.HTTPRequestsTotal < s.Current.Metrics.HTTPRequestsTotal {
		return true
	}
	return false
}

// detectCompletedRequests infers request completions by comparing thread states across
// two consecutive polls: FrankenPHP does not emit a completion event, so a thread
// transitioning from busy to idle (or starting a new request) is our only signal.
// The estimated request duration uses the midpoint between polls as end time,
// which halves the maximum estimation error compared to using either poll boundary.
func (s *State) detectCompletedRequests(newSnap *fetcher.Snapshot) {
	if s.Current == nil {
		return
	}

	prevByIndex := make(map[int]fetcher.ThreadDebugState, len(s.Current.Threads.ThreadDebugStates))
	for _, t := range s.Current.Threads.ThreadDebugStates {
		prevByIndex[t.Index] = t
	}

	for _, curr := range newSnap.Threads.ThreadDebugStates {
		prev, ok := prevByIndex[curr.Index]
		if !ok || !prev.IsBusy || prev.RequestStartedAt <= 0 {
			continue
		}

		completed := !curr.IsBusy || curr.RequestStartedAt != prev.RequestStartedAt
		if completed {
			// Estimate when the request ended: midpoint between the two polls
			// reduces max error from interval to interval/2.
			// If the request started after the midpoint (short-lived request),
			// fall back to currentFetchedAt as end estimate.
			endEstimate := (s.Current.FetchedAt.UnixMilli() + newSnap.FetchedAt.UnixMilli()) / 2
			if prev.RequestStartedAt >= endEstimate {
				endEstimate = newSnap.FetchedAt.UnixMilli()
			}
			durationMs := float64(endEstimate - prev.RequestStartedAt)
			s.percentiles.record(durationMs, newSnap.FetchedAt)
		}
	}
}

func (s *State) computeDerived() DerivedMetrics {
	if s.Current == nil {
		return DerivedMetrics{}
	}

	var d DerivedMetrics

	for _, t := range s.Current.Threads.ThreadDebugStates {
		if t.IsBusy {
			d.TotalBusy++
		} else if t.IsWaiting {
			d.TotalIdle++
		}
	}

	for _, w := range s.Current.Metrics.Workers {
		d.TotalCrashes += w.Crashes
	}

	// Prefer Prometheus histogram buckets over the thread-based tracker: histograms
	// capture all requests, while the tracker only sees those that complete between two polls.
	if s.Previous != nil && len(s.Current.Metrics.DurationBuckets) > 0 && len(s.Previous.Metrics.DurationBuckets) > 0 {
		p50, p90, p95, p99, ok := histogramPercentiles(s.Previous.Metrics.DurationBuckets, s.Current.Metrics.DurationBuckets)
		if ok {
			d.P50 = p50
			d.P90 = p90
			d.P95 = p95
			d.P99 = p99
			d.HasPercentiles = true
		}
	} else if s.percentiles != nil {
		p50, p95, p99, ok := s.percentiles.percentiles(s.Current.FetchedAt)
		if ok {
			d.P50 = p50
			d.P95 = p95
			d.P99 = p99
			d.HasPercentiles = true
		}
	}

	if s.Previous == nil {
		return d
	}

	dt := s.Current.FetchedAt.Sub(s.Previous.FetchedAt).Seconds()
	if dt < 0.1 {
		return d
	}

	// try FrankenPHP worker metrics first
	var currCount, prevCount, currTime, prevTime float64
	for _, w := range s.Current.Metrics.Workers {
		currCount += w.RequestCount
		currTime += w.RequestTime
	}
	for _, w := range s.Previous.Metrics.Workers {
		prevCount += w.RequestCount
		prevTime += w.RequestTime
	}

	// fallback to Caddy HTTP metrics if no FrankenPHP worker metrics
	if currCount == 0 && s.Current.Metrics.HTTPRequestDurationCount > 0 {
		currCount = s.Current.Metrics.HTTPRequestDurationCount
		currTime = s.Current.Metrics.HTTPRequestDurationSum
		prevCount = s.Previous.Metrics.HTTPRequestDurationCount
		prevTime = s.Previous.Metrics.HTTPRequestDurationSum
	}

	// if either snapshot had no metrics data (fetch failed for that tick),
	// the delta is meaningless, so skip rate calculations
	if prevCount == 0 || currCount == 0 {
		return d
	}

	deltaCount := currCount - prevCount
	deltaTime := currTime - prevTime

	if deltaCount > 0 {
		d.RPS = deltaCount / dt
		d.AvgTime = (deltaTime / deltaCount) * 1000 // ms
	}

	deltaErrors := s.Current.Metrics.HTTPRequestErrorsTotal - s.Previous.Metrics.HTTPRequestErrorsTotal
	if deltaErrors > 0 {
		d.ErrorRate = deltaErrors / dt
	}

	return d
}

func (s *State) computeHostDerived() []HostDerived {
	if s.Current == nil || len(s.Current.Metrics.Hosts) == 0 {
		return nil
	}

	dt := 0.0
	if s.Previous != nil {
		dt = s.Current.FetchedAt.Sub(s.Previous.FetchedAt).Seconds()
	}

	result := make([]HostDerived, 0, len(s.Current.Metrics.Hosts))
	for host, curr := range s.Current.Metrics.Hosts {
		hd := HostDerived{
			Host:          host,
			InFlight:      curr.InFlight,
			TotalRequests: curr.RequestsTotal,
		}

		if curr.ResponseSizeCount > 0 {
			hd.AvgResponseSize = curr.ResponseSizeSum / curr.ResponseSizeCount
		}
		if curr.RequestSizeCount > 0 {
			hd.AvgRequestSize = curr.RequestSizeSum / curr.RequestSizeCount
		}

		if s.Previous != nil && dt >= 0.1 {
			if prev, ok := s.Previous.Metrics.Hosts[host]; ok {
				deltaCount := curr.DurationCount - prev.DurationCount
				deltaSum := curr.DurationSum - prev.DurationSum
				if deltaCount > 0 {
					hd.RPS = deltaCount / dt
					hd.AvgTime = (deltaSum / deltaCount) * 1000
				}

				deltaErrors := curr.ErrorsTotal - prev.ErrorsTotal
				if deltaErrors > 0 {
					hd.ErrorRate = deltaErrors / dt
				}

				hd.StatusCodes = computeStatusCodeRates(curr.StatusCodes, prev.StatusCodes, dt)
				hd.MethodRates = computeMethodRates(curr.Methods, prev.Methods, dt)

				if len(curr.DurationBuckets) > 0 && len(prev.DurationBuckets) > 0 {
					p50, p90, p95, p99, ok := histogramPercentiles(prev.DurationBuckets, curr.DurationBuckets)
					if ok {
						hd.P50 = p50
						hd.P90 = p90
						hd.P95 = p95
						hd.P99 = p99
						hd.HasPercentiles = true
					}
				}

				if len(curr.TTFBBuckets) > 0 && len(prev.TTFBBuckets) > 0 {
					p50, p90, p95, p99, ok := histogramPercentiles(prev.TTFBBuckets, curr.TTFBBuckets)
					if ok {
						hd.TTFBP50 = p50
						hd.TTFBP90 = p90
						hd.TTFBP95 = p95
						hd.TTFBP99 = p99
						hd.HasTTFB = true
					}
				}
			}
		}

		result = append(result, hd)
	}
	return result
}

// computeUpstreamDerived derives per-upstream state from the current snapshot.
// HealthChanged is computed using the same key the Prometheus parser used for
// this entry (address or address/handler), so multi-handler configs that expose
// the same address twice are tracked independently instead of collapsing.
func (s *State) computeUpstreamDerived() []UpstreamDerived {
	if s.Current == nil || len(s.Current.Metrics.Upstreams) == 0 {
		return nil
	}

	result := make([]UpstreamDerived, 0, len(s.Current.Metrics.Upstreams))
	for key, u := range s.Current.Metrics.Upstreams {
		healthy := u.Healthy >= 1
		ud := UpstreamDerived{
			Address: u.Address,
			Handler: u.Handler,
			Healthy: healthy,
		}

		if s.Previous != nil {
			if prev, ok := s.Previous.Metrics.Upstreams[key]; ok {
				prevHealthy := prev.Healthy >= 1
				ud.HealthChanged = healthy != prevHealthy
			}
		}

		result = append(result, ud)
	}
	return result
}

func computeMethodRates(curr, prev map[string]float64, dt float64) map[string]float64 {
	if len(curr) == 0 || dt <= 0 {
		return nil
	}
	rates := make(map[string]float64)
	for method, currCount := range curr {
		prevCount := prev[method]
		delta := currCount - prevCount
		if delta > 0 {
			rates[method] = delta / dt
		}
	}
	if len(rates) == 0 {
		return nil
	}
	return rates
}

func computeStatusCodeRates(curr, prev map[int]float64, dt float64) map[int]float64 {
	if len(curr) == 0 || dt <= 0 {
		return nil
	}
	rates := make(map[int]float64)
	for code, currCount := range curr {
		prevCount := prev[code]
		delta := currCount - prevCount
		if delta > 0 {
			rates[code] = delta / dt
		}
	}
	if len(rates) == 0 {
		return nil
	}
	return rates
}

func FormatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
