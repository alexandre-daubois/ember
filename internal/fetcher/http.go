package fetcher

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexandre-daubois/ember/internal/instrumentation"
	"github.com/alexandre-daubois/ember/pkg/metrics"
	"golang.org/x/sync/errgroup"
)

// swappableTransport routes requests through an inner *http.Transport that
// can be replaced atomically. It lets SetTLSConfig swap TLS settings while
// concurrent Fetch calls are in flight without any mutex on the hot path.
type swappableTransport struct {
	inner atomic.Pointer[http.Transport]
}

func (s *swappableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return s.inner.Load().RoundTrip(req)
}

func (s *swappableTransport) store(t *http.Transport) *http.Transport {
	return s.inner.Swap(t)
}

const (
	maxRetries                 = 3
	requestTimeout             = 5 * time.Second
	initialBackoff             = 200 * time.Millisecond
	serverNamesRefreshInterval = 30 * time.Second
)

type HTTPFetcher struct {
	baseURL    string
	socketPath string
	transport  *swappableTransport
	httpClient *http.Client
	procHandle *processHandle
	recorder   *instrumentation.Recorder

	mu                     sync.Mutex
	hasFrankenPHP          bool
	serverNames            []string
	hostNames              []string
	lastPromCPU            float64
	lastPromSample         time.Time
	lastServerNamesRefresh time.Time
	lastFrankenPHPCheck    time.Time
}

// SetRecorder attaches a Recorder so each per-stage sub-fetch in Fetch
// reports its duration and outcome. Must be called before any Fetch
// goroutine is spawned (typically once at startup); the field is read
// concurrently afterwards but never written. Passing nil disables it.
func (f *HTTPFetcher) SetRecorder(r *instrumentation.Recorder) {
	f.recorder = r
}

// NewHTTPFetcher creates a fetcher targeting the given Caddy admin API address.
// When pid is non-zero, OS-level process metrics are collected for that PID.
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

	swap := &swappableTransport{}
	swap.store(transport)

	return &HTTPFetcher{
		baseURL:    strings.TrimRight(baseURL, "/"),
		socketPath: socketPath,
		transport:  swap,
		httpClient: &http.Client{Transport: swap},
		procHandle: ph,
	}
}

// IsUnixSocket reports whether this fetcher communicates over a Unix socket.
func (f *HTTPFetcher) IsUnixSocket() bool {
	return f.socketPath != ""
}

// SetTLSConfig replaces the HTTP transport with one using the given TLS configuration.
// It is a no-op when the fetcher uses a Unix socket.
//
// The swap is atomic: concurrent Fetch calls observe either the old or the new
// transport, never a torn value. The previous transport's idle connections are
// closed so that subsequent requests negotiate TLS with the new configuration.
func (f *HTTPFetcher) SetTLSConfig(tlsConfig *tls.Config) {
	if f.socketPath != "" {
		return
	}
	next := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}
	if prev := f.transport.store(next); prev != nil {
		prev.CloseIdleConnections()
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

// DetectFrankenPHP probes the /frankenphp/threads endpoint and updates the
// internal flag. It returns true when FrankenPHP is available.
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

// HasFrankenPHP reports whether FrankenPHP was detected on the last check.
func (f *HTTPFetcher) HasFrankenPHP() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasFrankenPHP
}

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

// RestartWorkers sends a POST to the FrankenPHP worker restart endpoint.
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

	return metrics.ParsePrometheus(resp.Body)
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

// EmberLogSinkName is the well-known name Ember uses when it auto-registers
// a logging sink in Caddy. A fixed name lets re-registrations from a fresh
// Ember session overwrite a stale entry left over from a prior crash.
const EmberLogSinkName = "__ember__"

// emberRuntimeLogSinkName is the well-known name for the second sink Ember
// registers to capture Caddy's runtime logs (startup, reloads, TLS, admin
// API, plugins): anything that is not an access log.
const emberRuntimeLogSinkName = "__ember_runtime__"

// RegisterEmberLogSink hot-installs a Caddy logging sink that
// pushes JSON access logs to the given listener address. The sink uses
// soft_start so Caddy does not fail config load when the listener is briefly
// unavailable.
//
// The address must be reachable from Caddy's process; on the same host this
// is typically "127.0.0.1:<port>" but for remote Caddy instances you must
// supply a routable host:port pair.
func (f *HTTPFetcher) RegisterEmberLogSink(ctx context.Context, listenerAddr string) error {
	return f.registerLogSink(ctx, EmberLogSinkName, logSinkPayload(listenerAddr, map[string]any{
		"include": []string{"http.log.access"},
	}))
}

// RegisterEmberRuntimeLogSink hot-installs a second logging sink that pushes
// Caddy's runtime logs (everything that is not an access log) to the given
// listener address. Uses `exclude` so the sink is a catch-all minus access
// logs, which are handled by the primary __ember__ sink.
func (f *HTTPFetcher) RegisterEmberRuntimeLogSink(ctx context.Context, listenerAddr string) error {
	return f.registerLogSink(ctx, emberRuntimeLogSinkName, logSinkPayload(listenerAddr, map[string]any{
		"exclude": []string{"http.log.access"},
	}))
}

// logSinkPayload builds the JSON body for a Caddy sink definition. Using
// json.Marshal for the address ensures user-supplied values (via --log-listen)
// cannot inject extra fields through string interpolation.
func logSinkPayload(listenerAddr string, extra map[string]any) map[string]any {
	p := map[string]any{
		"writer": map[string]any{
			"output":     "net",
			"address":    "tcp/" + listenerAddr,
			"soft_start": true,
		},
		"encoder": map[string]any{"format": "json"},
	}
	maps.Copy(p, extra)
	return p
}

func (f *HTTPFetcher) registerLogSink(ctx context.Context, name string, payload map[string]any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s log sink payload: %w", name, err)
	}

	if err := f.putLogSink(ctx, name, body); err == nil {
		return nil
	}

	// The PUT fails when /config/logging/logs does not exist yet (Caddyfile
	// has no log directive). Bootstrap the path, then retry.
	if err := f.ensureLoggingPath(ctx); err != nil {
		return fmt.Errorf("register %s log sink: %w", name, err)
	}
	return f.putLogSink(ctx, name, body)
}

func (f *HTTPFetcher) putLogSink(ctx context.Context, name string, body []byte) error {
	endpoint := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// ensureLoggingPath creates /config/logging/logs in Caddy's config when it
// does not exist yet. A Caddyfile with no log directive produces a JSON config
// that omits the logging section entirely; Caddy's admin API refuses to PUT
// at a deep path whose parents are missing.
func (f *HTTPFetcher) ensureLoggingPath(ctx context.Context) error {
	// Try creating just the logs key (works when /config/logging exists).
	if err := f.putJSON(ctx, f.baseURL+"/config/logging/logs", []byte("{}")); err == nil {
		return nil
	}
	// /config/logging itself is missing. Create it with an empty logs map.
	body, _ := json.Marshal(map[string]any{"logs": map[string]any{}})
	return f.putJSON(ctx, f.baseURL+"/config/logging", body)
}

func (f *HTTPFetcher) putJSON(ctx context.Context, endpoint string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// EnableServerAccessLogs activates Caddy's HTTP access logs on the given
// server when none are configured yet. It is a no-op (returns false, nil)
// when the server already has its own logs block, so a user's existing
// configuration is never overwritten.
//
// Returns enabled=true only when Ember actually flipped the switch; pass that
// value to RestoreServerAccessLogs at shutdown to undo only what we changed.
func (f *HTTPFetcher) EnableServerAccessLogs(ctx context.Context, serverName string) (enabled bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := f.serverLogsEndpoint(serverName)

	existing, err := f.getServerLogs(ctx, endpoint)
	if err != nil {
		return false, err
	}
	if !isEmptyOrNull(existing) {
		// User-defined logs config: leave it alone.
		return false, nil
	}

	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		return false, err
	}
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := f.httpClient.Do(postReq)
	if err != nil {
		return false, fmt.Errorf("enable server logs: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, postResp.Body)
		_ = postResp.Body.Close()
	}()
	if postResp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("enable server logs: HTTP %d", postResp.StatusCode)
	}
	return true, nil
}

// RestoreServerAccessLogs removes the access-logs block that
// EnableServerAccessLogs added. We parse the current value as JSON (rather
// than comparing raw bytes) so whitespace or Caddy-side canonicalization of
// the empty object we posted does not leave our config behind.
//
// If the current value is not an empty object, a user or another tool
// modified the server's logs config in the meantime: we leave it alone.
func (f *HTTPFetcher) RestoreServerAccessLogs(ctx context.Context, serverName string) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := f.serverLogsEndpoint(serverName)

	existing, err := f.getServerLogs(ctx, endpoint)
	if err != nil {
		return err
	}
	// Only DELETE when the current value is an empty object -- the shape we
	// installed. If fields have appeared (user config) or the value is null
	// (already gone), skip.
	if !isEmptyObject(existing) {
		return nil
	}

	delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	delResp, err := f.httpClient.Do(delReq)
	if err != nil {
		return fmt.Errorf("restore server logs: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, delResp.Body)
		_ = delResp.Body.Close()
	}()
	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("restore server logs: HTTP %d", delResp.StatusCode)
	}
	return nil
}

// serverLogsEndpoint returns the admin API URL for a server's logs config,
// URL-escaping the server name so unusual but valid names (containing
// spaces, slashes or other special characters) cannot break the path.
func (f *HTTPFetcher) serverLogsEndpoint(serverName string) string {
	return f.baseURL + "/config/apps/http/servers/" + url.PathEscape(serverName) + "/logs"
}

// getServerLogs retrieves the current /logs config for a server as raw bytes.
// Returns a non-nil error for any non-200 response so callers never silently
// fall through to a write on a broken admin API.
func (f *HTTPFetcher) getServerLogs(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get server logs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("get server logs: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// isEmptyOrNull reports whether body represents a missing or empty logs
// config -- either "null", "", or an object with no fields.
func isEmptyOrNull(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	return isEmptyObjectTrimmed(trimmed)
}

// isEmptyObject reports whether body parses as a JSON object with zero
// fields. It deliberately rejects "null", arrays and scalars, which all
// unmarshal successfully into a nil map but are not what we installed.
func isEmptyObject(body []byte) bool {
	return isEmptyObjectTrimmed(bytes.TrimSpace(body))
}

func isEmptyObjectTrimmed(trimmed []byte) bool {
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return false
	}
	return len(m) == 0
}

// CheckEmberLogSink reports whether the Ember access log sink is currently
// registered in Caddy's configuration. Returns false on any error.
func (f *HTTPFetcher) CheckEmberLogSink(ctx context.Context) bool {
	return f.checkLogSink(ctx, EmberLogSinkName)
}

// CheckEmberRuntimeLogSink is the runtime-sink counterpart of CheckEmberLogSink.
func (f *HTTPFetcher) CheckEmberRuntimeLogSink(ctx context.Context) bool {
	return f.checkLogSink(ctx, emberRuntimeLogSinkName)
}

func (f *HTTPFetcher) checkLogSink(ctx context.Context, name string) bool {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	endpoint := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	return !isEmptyOrNull(body)
}

// UnregisterEmberLogSink removes the sink installed by RegisterEmberLogSink.
// It is safe to call when no sink exists.
func (f *HTTPFetcher) UnregisterEmberLogSink(ctx context.Context) error {
	return f.unregisterLogSink(ctx, EmberLogSinkName)
}

// UnregisterEmberRuntimeLogSink is the runtime-sink counterpart.
func (f *HTTPFetcher) UnregisterEmberRuntimeLogSink(ctx context.Context) error {
	return f.unregisterLogSink(ctx, emberRuntimeLogSinkName)
}

func (f *HTTPFetcher) unregisterLogSink(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	url := f.baseURL + "/config/logging/logs/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("unregister %s log sink: %w", name, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	// 200 = removed, 404 = nothing to remove, both fine.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unregister %s log sink: HTTP %d", name, resp.StatusCode)
	}
	return nil
}
