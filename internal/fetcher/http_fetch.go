package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/pkg/metrics"
	"golang.org/x/sync/errgroup"
)

// FetchServerNames queries the Caddy config API for HTTP server names
// and the host routes they expose, caching both. Returns the server names
// on success, nil on failure. The host names are accessible via HostNames().
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

	type route struct {
		Match []struct {
			Host []string `json:"host"`
		} `json:"match"`
	}
	type server struct {
		Routes []route `json:"routes"`
	}
	var servers map[string]server
	if err := json.NewDecoder(resp.Body).Decode(&servers); err != nil {
		return nil
	}

	names := make([]string, 0, len(servers))
	hostSet := make(map[string]struct{})
	for name, srv := range servers {
		names = append(names, name)
		for _, r := range srv.Routes {
			for _, m := range r.Match {
				for _, h := range m.Host {
					if h != "" {
						hostSet[h] = struct{}{}
					}
				}
			}
		}
	}
	slices.Sort(names)
	hosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		hosts = append(hosts, h)
	}
	slices.Sort(hosts)

	f.mu.Lock()
	f.serverNames = names
	f.hostNames = hosts
	f.mu.Unlock()
	return names
}

// ServerNames returns the cached Caddy server names from the last successful fetch.
func (f *HTTPFetcher) ServerNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.serverNames
}

// Fetch collects a full snapshot from the Caddy admin API: thread states,
// Prometheus metrics, and OS-level process stats. Partial results are returned
// when individual sub-fetches fail.
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
			start := time.Now()
			t, err := f.fetchThreads(gCtx)
			f.recorder.Record(instrumentation.StageThreads, time.Since(start), err)
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
		start := time.Now()
		m, err := f.fetchMetrics(gCtx)
		f.recorder.Record(instrumentation.StageMetrics, time.Since(start), err)
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
		start := time.Now()
		p, err := f.procHandle.fetch(gCtx)
		f.recorder.Record(instrumentation.StageProcess, time.Since(start), err)
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

		// Fall back to Prometheus process metrics when gopsutil returns nothing:
		// in containers, gopsutil often cannot access the target process.
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

	// Seed declared hosts as empty entries so they appear in the TUI before
	// the first request. Prefer routes[].match[].host[] (the real hosts, e.g.
	// "app.localhost"); fall back to server names (srv0, srv1...) only when
	// the Caddyfile has no host matcher — typical of a bare `:8080 { ... }`
	// block — so the table is not empty while traffic ramps up.
	f.mu.Lock()
	seed := f.hostNames
	if len(seed) == 0 {
		seed = f.serverNames
	}
	f.mu.Unlock()
	if len(seed) > 0 {
		if metrics.Hosts == nil {
			metrics.Hosts = make(map[string]*HostMetrics, len(seed))
		}
		for _, h := range seed {
			if _, ok := metrics.Hosts[h]; !ok {
				metrics.Hosts[h] = &HostMetrics{
					Host:        h,
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

// onConnected lazily detects FrankenPHP and refreshes Caddy server names
// on the first successful metrics fetch and periodically thereafter.
// This avoids blocking when Caddy is not yet ready or temporarily unreachable,
// while still picking up vhosts added after Ember started.
func (f *HTTPFetcher) onConnected(ctx context.Context) {
	f.mu.Lock()
	fpStale := time.Since(f.lastFrankenPHPCheck) >= serverNamesRefreshInterval
	f.mu.Unlock()
	if fpStale {
		detected := f.DetectFrankenPHP(ctx)
		if detected {
			f.mu.Lock()
			f.lastFrankenPHPCheck = time.Now()
			f.mu.Unlock()
		}
	}

	f.mu.Lock()
	stale := time.Since(f.lastServerNamesRefresh) >= serverNamesRefreshInterval
	f.mu.Unlock()
	if stale {
		if names := f.FetchServerNames(ctx); names != nil {
			f.mu.Lock()
			f.lastServerNamesRefresh = time.Now()
			f.mu.Unlock()
		}
	}
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

	return metrics.ParsePrometheus(resp.Body)
}
