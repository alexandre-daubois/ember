package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	maxRetries     = 3
	requestTimeout = 5 * time.Second
	initialBackoff = 200 * time.Millisecond
)

type HTTPFetcher struct {
	baseURL    string
	httpClient *http.Client
	procHandle *processHandle

	mu             sync.Mutex
	hasFrankenPHP  bool
	serverNames    []string
	lastPromCPU    float64
	lastPromSample time.Time
}

func NewHTTPFetcher(baseURL string, pid int32) *HTTPFetcher {
	ph := newProcessHandle(pid)
	return &HTTPFetcher{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        2,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     30 * time.Second,
			},
		},
		procHandle: ph,
	}
}

func (f *HTTPFetcher) DetectFrankenPHP(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/frankenphp/threads", nil)
	if err != nil {
		return false
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	result := resp.StatusCode == http.StatusOK
	f.mu.Lock()
	f.hasFrankenPHP = result
	f.mu.Unlock()
	return result
}

func (f *HTTPFetcher) HasFrankenPHP() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasFrankenPHP
}

func (f *HTTPFetcher) FetchServerNames(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/apps/http/servers", nil)
	if err != nil {
		return nil
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var servers map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&servers); err != nil {
		return nil
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	slices.Sort(names)
	f.mu.Lock()
	f.serverNames = names
	f.mu.Unlock()
	return names
}

func (f *HTTPFetcher) ServerNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.serverNames
}

func (f *HTTPFetcher) Fetch(ctx context.Context) (*Snapshot, error) {
	var (
		threads   ThreadsResponse
		metrics   MetricsSnapshot
		proc      ProcessMetrics
		mu        sync.Mutex
		errs      []string
		metricsOK bool
	)

	g, gCtx := errgroup.WithContext(ctx)

	f.mu.Lock()
	hasFP := f.hasFrankenPHP
	f.mu.Unlock()

	if hasFP {
		g.Go(func() error {
			t, err := f.fetchThreads(gCtx)
			if err != nil {
				mu.Lock()
				errs = append(errs, err.Error())
				mu.Unlock()
				return nil
			}
			threads = t
			return nil
		})
	}

	g.Go(func() error {
		m, err := f.fetchMetrics(gCtx)
		if err != nil {
			mu.Lock()
			errs = append(errs, err.Error())
			mu.Unlock()
			return nil
		}
		mu.Lock()
		metricsOK = true
		mu.Unlock()
		metrics = m
		return nil
	})

	g.Go(func() error {
		p, err := f.procHandle.fetch(gCtx)
		if err != nil {
			mu.Lock()
			errs = append(errs, err.Error())
			mu.Unlock()
			return nil
		}
		proc = p
		return nil
	})

	_ = g.Wait()

	if metricsOK {
		f.onConnected(ctx)

		// Derive process metrics from Prometheus when gopsutil has no data
		if proc.RSS == 0 && metrics.ProcessRSSBytes > 0 {
			proc.RSS = uint64(metrics.ProcessRSSBytes)
		}
		if proc.CPUPercent == 0 && metrics.ProcessCPUSecondsTotal > 0 {
			now := time.Now()
			f.mu.Lock()
			if !f.lastPromSample.IsZero() {
				elapsed := now.Sub(f.lastPromSample).Seconds()
				if elapsed > 0 {
					proc.CPUPercent = (metrics.ProcessCPUSecondsTotal - f.lastPromCPU) / elapsed * 100
					if proc.CPUPercent < 0 {
						proc.CPUPercent = 0
					}
				}
			}
			f.lastPromCPU = metrics.ProcessCPUSecondsTotal
			f.lastPromSample = now
			f.mu.Unlock()
		}
		if proc.CreateTime == 0 && metrics.ProcessStartTimeSeconds > 0 {
			startSec := int64(metrics.ProcessStartTimeSeconds)
			proc.CreateTime = startSec * 1000
			proc.Uptime = time.Since(time.Unix(startSec, 0))
		}
	}

	// Seed known server names as empty host entries so they appear immediately
	f.mu.Lock()
	names := f.serverNames
	f.mu.Unlock()
	if len(names) > 0 && metrics.Hosts != nil {
		for _, name := range names {
			if _, ok := metrics.Hosts[name]; !ok {
				metrics.Hosts[name] = &HostMetrics{
					Host:        name,
					StatusCodes: make(map[int]float64),
					Methods:     make(map[string]float64),
				}
			}
		}
	}

	f.mu.Lock()
	currentHasFP := f.hasFrankenPHP
	f.mu.Unlock()

	return &Snapshot{
		Threads:       threads,
		Metrics:       metrics,
		Process:       proc,
		FetchedAt:     time.Now(),
		Errors:        errs,
		HasFrankenPHP: currentHasFP,
	}, nil
}

func (f *HTTPFetcher) onConnected(ctx context.Context) {
	f.mu.Lock()
	hasFP := f.hasFrankenPHP
	f.mu.Unlock()
	if !hasFP {
		f.DetectFrankenPHP(ctx)
	}

	f.mu.Lock()
	hasNames := len(f.serverNames) > 0
	f.mu.Unlock()
	if !hasNames {
		f.FetchServerNames(ctx)
	}
}

func (f *HTTPFetcher) RestartWorkers(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.baseURL+"/frankenphp/workers/restart", nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("restart workers: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("restart workers: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (f *HTTPFetcher) fetchThreads(ctx context.Context) (ThreadsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/frankenphp/threads", nil)
	if err != nil {
		return ThreadsResponse{}, err
	}

	resp, err := f.doWithRetry(ctx, req)
	if err != nil {
		return ThreadsResponse{}, fmt.Errorf("fetch threads: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ThreadsResponse{}, fmt.Errorf("fetch threads: HTTP %d", resp.StatusCode)
	}

	var result ThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ThreadsResponse{}, fmt.Errorf("fetch threads: decode: %w", err)
	}
	return result, nil
}

func (f *HTTPFetcher) fetchMetrics(ctx context.Context) (MetricsSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/metrics", nil)
	if err != nil {
		return MetricsSnapshot{}, err
	}

	resp, err := f.doWithRetry(ctx, req)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("fetch metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return MetricsSnapshot{}, fmt.Errorf("fetch metrics: HTTP %d", resp.StatusCode)
	}

	return parsePrometheusMetrics(resp.Body)
}

func (f *HTTPFetcher) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := initialBackoff * time.Duration(1<<uint(attempt-1))
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			req = req.Clone(ctx)
		}
		resp, err := f.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
