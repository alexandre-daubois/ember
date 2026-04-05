package fetcher

import (
	"context"
	"time"

	"github.com/alexandre-daubois/ember/pkg/metrics"
)

type ThreadDebugState = metrics.ThreadDebugState

type ThreadsResponse = metrics.ThreadsResponse

type WorkerMetrics = metrics.WorkerMetrics

type HostMetrics = metrics.HostMetrics

type MetricsSnapshot = metrics.MetricsSnapshot

type HistogramBucket = metrics.HistogramBucket

type ProcessMetrics = metrics.ProcessMetrics

type Snapshot = metrics.Snapshot

type CertificateInfo struct {
	Subject   string
	Issuer    string
	DNSNames  []string
	NotBefore time.Time
	NotAfter  time.Time
	Serial    string
	IsCA      bool
	Source    string
	Host      string
	AutoRenew bool
}

type Fetcher interface {
	Fetch(ctx context.Context) (*Snapshot, error)
}
