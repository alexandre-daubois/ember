package fetcher

import (
	"context"
	"time"
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

	// Go runtime process metrics (from standard Prometheus collector)
	ProcessCPUSecondsTotal  float64 `json:"processCpuSecondsTotal,omitempty"`
	ProcessRSSBytes         float64 `json:"processRssBytes,omitempty"`
	ProcessStartTimeSeconds float64 `json:"processStartTimeSeconds,omitempty"`

	// Caddy config reload status (built-in Caddy metrics)
	HasConfigReloadMetrics           bool    `json:"hasConfigReloadMetrics"`
	ConfigLastReloadSuccessful       float64 `json:"configLastReloadSuccessful"`
	ConfigLastReloadSuccessTimestamp float64 `json:"configLastReloadSuccessTimestamp"`
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

type Fetcher interface {
	Fetch(ctx context.Context) (*Snapshot, error)
}
