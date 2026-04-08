package fetcher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	maxRetries                 = 3
	requestTimeout             = 5 * time.Second
	initialBackoff             = 200 * time.Millisecond
	serverNamesRefreshInterval = 30 * time.Second
)

type HTTPFetcher struct {
	baseURL    string
	socketPath string
	httpClient *http.Client
	procHandle *processHandle

	mu                     sync.Mutex
	hasFrankenPHP          bool
	serverNames            []string
	lastPromCPU            float64
	lastPromSample         time.Time
	lastServerNamesRefresh time.Time
	lastFrankenPHPCheck    time.Time
}

func NewHTTPFetcher(baseURL string, pid int32) *HTTPFetcher {
	ph := newProcessHandle(pid)

	var socketPath string
	transport := &http.Transport{
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}

	if sp, ok := ParseUnixAddr(baseURL); ok {
		socketPath = sp
		baseURL = "http://localhost"
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sp)
		}
	}

	return &HTTPFetcher{
		baseURL:    strings.TrimRight(baseURL, "/"),
		socketPath: socketPath,
		httpClient: &http.Client{Transport: transport},
		procHandle: ph,
	}
}

// IsUnixSocket reports whether this fetcher communicates over a Unix socket.
func (f *HTTPFetcher) IsUnixSocket() bool {
	return f.socketPath != ""
}

// SetTLSConfig replaces the HTTP transport with one using the given TLS configuration.
// It is a no-op when the fetcher uses a Unix socket.
func (f *HTTPFetcher) SetTLSConfig(tlsConfig *tls.Config) {
	if f.socketPath != "" {
		return
	}
	f.httpClient.Transport = &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}
}

// TLSOptions holds paths for TLS certificate files.
type TLSOptions struct {
	CACert     string
	ClientCert string
	ClientKey  string
	Insecure   bool
}

// BuildTLSConfig creates a *tls.Config from file paths.
func BuildTLSConfig(opts TLSOptions) (*tls.Config, error) {
	if !opts.Insecure && opts.CACert == "" && opts.ClientCert == "" {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if opts.Insecure {
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // user explicitly requested --insecure
	}

	if opts.CACert != "" {
		caCert, err := os.ReadFile(opts.CACert)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("invalid CA cert in %s", opts.CACert)
		}
		tlsConfig.RootCAs = pool
	}

	if opts.ClientCert != "" && opts.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(opts.ClientCert, opts.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
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

// FetchConfig returns the full Caddy configuration as raw JSON.
func (f *HTTPFetcher) FetchConfig(ctx context.Context) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.doWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fetch config: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch config: HTTP %d", resp.StatusCode)
	}

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fetch config: %w", err)
	}
	return raw, nil
}

// CheckAdminAPI returns nil if the Caddy admin API is reachable.
func (f *HTTPFetcher) CheckAdminAPI(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/", nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin API unreachable: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

// CheckMetricsEnabled returns true if the HTTP metrics directive is configured.
func (f *HTTPFetcher) CheckMetricsEnabled(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/apps/http/metrics", nil)
	if err != nil {
		return false, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return false, nil
	}

	// /config/apps/http/metrics returns "null" when not set, or a JSON object when set
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return false, nil
	}
	return string(raw) != "null", nil
}

// EnableMetrics activates the HTTP metrics directive via the admin API.
func (f *HTTPFetcher) EnableMetrics(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.baseURL+"/config/apps/http/metrics", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("enable metrics: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("enable metrics: HTTP %d", resp.StatusCode)
	}
	return nil
}

// FrankenPHPConfig holds the FrankenPHP app configuration from Caddy.
type FrankenPHPConfig struct {
	NumThreads int                      `json:"num_threads"`
	Workers    []FrankenPHPWorkerConfig `json:"workers"`
}

// FrankenPHPWorkerConfig holds a single worker definition.
type FrankenPHPWorkerConfig struct {
	FileName string `json:"file_name"`
	Name     string `json:"name"`
	Num      int    `json:"num"`
}

// FetchFrankenPHPConfig reads the FrankenPHP app config from the admin API.
func (f *HTTPFetcher) FetchFrankenPHPConfig(ctx context.Context) (*FrankenPHPConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/config/apps/frankenphp", nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var cfg FrankenPHPConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
