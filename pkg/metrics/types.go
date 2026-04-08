// Package metrics exposes the data types used by Ember to represent
// Caddy and FrankenPHP metrics. Plugin authors can import this package
// to reuse Ember's metric structures and Prometheus parser.
package metrics

import (
	"time"

	dto "github.com/prometheus/client_model/go"
)

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

type ThreadsResponse struct {
	ThreadDebugStates   []ThreadDebugState `json:"ThreadDebugStates"`
	ReservedThreadCount int                `json:"ReservedThreadCount"`
}

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

type HistogramBucket struct {
	UpperBound      float64 `json:"upperBound"`
	CumulativeCount float64 `json:"cumulativeCount"`
}

type ProcessMetrics struct {
	PID        int32         `json:"pid"`
	CPUPercent float64       `json:"cpuPercent"`
	RSS        uint64        `json:"rss"`
	CreateTime int64         `json:"createTime"`
	Uptime     time.Duration `json:"uptime"`
}

type Snapshot struct {
	Threads       ThreadsResponse `json:"threads"`
	Metrics       MetricsSnapshot `json:"metrics"`
	Process       ProcessMetrics  `json:"process"`
	FetchedAt     time.Time       `json:"fetchedAt"`
	Errors        []string        `json:"errors,omitempty"`
	HasFrankenPHP bool            `json:"hasFrankenPHP"`
}
