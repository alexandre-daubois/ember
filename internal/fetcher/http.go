package fetcher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexandre-daubois/ember/internal/instrumentation"
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
