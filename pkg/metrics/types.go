// Package metrics exposes the data types used by Ember to represent
// Caddy and FrankenPHP metrics. Plugin authors can import this package
// to reuse Ember's metric structures and Prometheus parser.
//
// EXPERIMENTAL: this package is part of the plugin API and is not yet
// stable. Types and fields may change in any future release. Feedback is
// very welcome; please open an issue on the Ember repository if something
// does not fit your use case so the API can evolve with real needs.
package metrics

import (
	"time"

	dto "github.com/prometheus/client_model/go"
)

// ThreadDebugState is the live state of a single FrankenPHP thread from the
// FrankenPHP debug endpoint. WaitingSinceMilliseconds is a duration in
// milliseconds; RequestStartedAt is a Unix timestamp in milliseconds;
// MemoryUsage is in bytes.
type ThreadDebugState struct {
	Index                    int    `json:"Index"`
	Name                     string `json:"Name"`
	State                    string `json:"State"`
	IsWaiting                bool   `json:"IsWaiting"`
	IsBusy                   bool   `json:"IsBusy"`
	WaitingSinceMilliseconds int64  `json:"WaitingSinceMilliseconds"`

	CurrentURI       string `json:"CurrentURI,omitempty"`
	CurrentMethod    string `json:"CurrentMethod,omitempty"`
	RequestStartedAt int64  `json:"RequestStartedAt,omitempty"`
	MemoryUsage      int64  `json:"MemoryUsage,omitempty"`
	RequestCount     int64  `json:"RequestCount,omitempty"`
}

// ThreadsResponse is the FrankenPHP thread debug payload: the per-thread states
// plus the number of reserved (allocated but not yet started) threads.
type ThreadsResponse struct {
	ThreadDebugStates   []ThreadDebugState `json:"ThreadDebugStates"`
	ReservedThreadCount int                `json:"ReservedThreadCount"`
}

// WorkerMetrics holds the FrankenPHP worker-pool metrics for a single worker
// script, mirroring the frankenphp_worker_* families. Total/Busy/Ready are
// current counts; Crashes/Restarts/RequestCount are cumulative counters.
type WorkerMetrics struct {
	Worker       string  `json:"worker"`
	Total        float64 `json:"total"`
	Busy         float64 `json:"busy"`
	Ready        float64 `json:"ready"`
	RequestTime  float64 `json:"requestTime"`
	RequestCount float64 `json:"requestCount"`
	Crashes      float64 `json:"crashes"`
	Restarts     float64 `json:"restarts"`
	QueueDepth   float64 `json:"queueDepth"`
}

// UpstreamMetrics represents a single Caddy reverse proxy upstream health entry.
// Address is the dial target (e.g. "backend1:80"). It is not unique on its own
// when Caddy exports the same address from multiple handlers: in that case the
// parser disambiguates by combining Address and Handler, so consumers that need
// a stable identity should use both fields together. Handler is empty when
// Caddy omits the label (the common case for caddy_reverse_proxy_upstreams_healthy).
// Healthy is 1.0 when healthy, 0.0 when down.
type UpstreamMetrics struct {
	Address string  `json:"address"`
	Handler string  `json:"handler,omitempty"`
	Healthy float64 `json:"healthy"`
}

// HostMetrics holds Caddy HTTP metrics aggregated for one host. The paired
// *Sum/*Count fields are Prometheus histogram/summary components: durations are
// in seconds, sizes in bytes. DurationBuckets and TTFBBuckets are cumulative
// histogram buckets. StatusCodes and Methods map a code/method to its rate.
type HostMetrics struct {
	Host              string             `json:"host"`
	RequestsTotal     float64            `json:"requestsTotal"`
	DurationSum       float64            `json:"durationSum"`
	DurationCount     float64            `json:"durationCount"`
	InFlight          float64            `json:"inFlight"`
	DurationBuckets   []HistogramBucket  `json:"durationBuckets,omitempty"`
	StatusCodes       map[int]float64    `json:"statusCodes,omitempty"`
	Methods           map[string]float64 `json:"methods,omitempty"`
	ResponseSizeSum   float64            `json:"responseSizeSum"`
	ResponseSizeCount float64            `json:"responseSizeCount"`
	RequestSizeSum    float64            `json:"requestSizeSum"`
	RequestSizeCount  float64            `json:"requestSizeCount"`
	ErrorsTotal       float64            `json:"errorsTotal"`
	TTFBSum           float64            `json:"ttfbSum"`
	TTFBCount         float64            `json:"ttfbCount"`
	TTFBBuckets       []HistogramBucket  `json:"ttfbBuckets,omitempty"`
}

// MetricsSnapshot is the parsed view of a single Caddy /metrics scrape, grouped
// by source: FrankenPHP thread/worker counters, Caddy HTTP metrics (global and
// per-host), reverse-proxy upstream health, Go process metrics, and config
// reload status. Duration fields are in seconds and size fields in bytes,
// matching Caddy's Prometheus families.
type MetricsSnapshot struct {
	// FrankenPHP-specific (require frankenphp metrics)
	TotalThreads float64                   `json:"totalThreads"`
	BusyThreads  float64                   `json:"busyThreads"`
	QueueDepth   float64                   `json:"queueDepth"`
	Workers      map[string]*WorkerMetrics `json:"workers"`

	// Caddy HTTP metrics (require `metrics` directive in Caddyfile)
	HTTPRequestErrorsTotal   float64           `json:"httpRequestErrorsTotal"`
	HTTPRequestsTotal        float64           `json:"httpRequestsTotal"`
	HTTPRequestDurationSum   float64           `json:"httpRequestDurationSum"`
	HTTPRequestDurationCount float64           `json:"httpRequestDurationCount"`
	HTTPRequestsInFlight     float64           `json:"httpRequestsInFlight"`
	DurationBuckets          []HistogramBucket `json:"durationBuckets,omitempty"`
	HasHTTPMetrics           bool              `json:"hasHttpMetrics"`

	// Per-host Caddy HTTP metrics
	Hosts map[string]*HostMetrics `json:"hosts,omitempty"`

	// Caddy reverse proxy upstream health
	Upstreams map[string]*UpstreamMetrics `json:"upstreams,omitempty"`

	// Go runtime process metrics (from standard Prometheus collector)
	ProcessCPUSecondsTotal  float64 `json:"processCpuSecondsTotal,omitempty"`
	ProcessRSSBytes         float64 `json:"processRssBytes,omitempty"`
	ProcessStartTimeSeconds float64 `json:"processStartTimeSeconds,omitempty"`

	// Caddy config reload status (built-in Caddy metrics)
	HasConfigReloadMetrics           bool    `json:"hasConfigReloadMetrics"`
	ConfigLastReloadSuccessful       float64 `json:"configLastReloadSuccessful"`
	ConfigLastReloadSuccessTimestamp float64 `json:"configLastReloadSuccessTimestamp"`

	// Extra contains metric families not consumed by Ember's core parser.
	// Plugin authors can use this to access custom metrics registered with
	// Caddy's Prometheus collector without making a separate /metrics request.
	Extra map[string]*dto.MetricFamily `json:"-"`
}

// HistogramBucket is one cumulative Prometheus histogram bucket: CumulativeCount
// is the number of observations less than or equal to UpperBound (the +Inf
// bucket carries the total). UpperBound units follow the source metric
// (seconds for duration histograms).
type HistogramBucket struct {
	UpperBound      float64 `json:"upperBound"`
	CumulativeCount float64 `json:"cumulativeCount"`
}

// ProcessMetrics holds OS-level metrics for the monitored server process.
// RSS is in bytes; CPUPercent is a percentage of a single core over the last
// sampling interval; CreateTime is a Unix timestamp in milliseconds.
type ProcessMetrics struct {
	PID        int32         `json:"pid"`
	CPUPercent float64       `json:"cpuPercent"`
	RSS        uint64        `json:"rss"`
	CreateTime int64         `json:"createTime"`
	Uptime     time.Duration `json:"uptime"`
}

// Snapshot is a complete point-in-time view returned by a fetch: FrankenPHP
// thread states, parsed Caddy metrics, process stats, the fetch time, and any
// non-fatal errors collected while assembling it.
type Snapshot struct {
	Threads       ThreadsResponse `json:"threads"`
	Metrics       MetricsSnapshot `json:"metrics"`
	Process       ProcessMetrics  `json:"process"`
	FetchedAt     time.Time       `json:"fetchedAt"`
	Errors        []string        `json:"errors,omitempty"`
	HasFrankenPHP bool            `json:"hasFrankenPHP"`
}
