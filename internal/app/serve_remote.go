package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/alexandre-daubois/ember/internal/exporter"
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
)

func exposeTLSConfig(cfg *config) (*tls.Config, error) {
	if cfg.exposeCert == "" {
		return nil, nil
	}
	certPEM, err := os.ReadFile(cfg.exposeCert)
	if err != nil {
		return nil, fmt.Errorf("--expose-cert: %w", err)
	}
	keyPEM, err := os.ReadFile(cfg.exposeKey)
	if err != nil {
		return nil, fmt.Errorf("--expose-key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("--expose-cert and --expose-key: %w", err)
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if cfg.exposeCA == "" {
		return tc, nil
	}
	caPEM, err := os.ReadFile(cfg.exposeCA)
	if err != nil {
		return nil, fmt.Errorf("--expose-client-ca: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("--expose-client-ca: no valid PEM certificate in %s", cfg.exposeCA)
	}
	tc.ClientCAs, tc.ClientAuth = pool, tls.RequireAndVerifyClientCert
	return tc, nil
}

func configureExposeServer(srv *http.Server, cfg *config) error {
	tc, err := exposeTLSConfig(cfg)
	if err != nil {
		return err
	}
	srv.TLSConfig = tc
	if tc != nil {
		srv.ErrorLog = slog.NewLogLogger(cfg.logger.Handler(), slog.LevelWarn)
	}
	return nil
}

func listenMetrics(srv *http.Server) error {
	if srv.TLSConfig != nil {
		return srv.ListenAndServeTLS("", "")
	}
	return srv.ListenAndServe()
}

type remoteRoutes struct {
	logs    exporter.LogSource
	relayed *fetcher.HTTPFetcher
}

func traceRemote(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := &fetcher.RemoteTrace{}
		next.ServeHTTP(w, r.WithContext(fetcher.WithRemoteTrace(r.Context(), trace)))
		var cn []any
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			cn = []any{"client_cn", r.TLS.PeerCertificates[0].Subject.CommonName}
		}
		if trace.Refused != 0 {
			log.Warn("remote request refused", append([]any{"remote_addr", r.RemoteAddr, "method", r.Method, "path", r.URL.Path, "status", trace.Refused}, cn...)...)
		}
		if trace.SessionOpened {
			log.Info("remote session opened", append([]any{"remote_addr", r.RemoteAddr, "user_agent", r.UserAgent()}, cn...)...)
		}
		if trace.ConfigRead {
			log.Info("remote config read", append([]any{"remote_addr", r.RemoteAddr, "path", r.URL.Path}, cn...)...)
		}
	})
}

// getOnly refuses other methods than GET and HEAD as a "GET /path" pattern does, but from the route, for the trace to see it.
func getOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fetcher.RemoteTraceOf(r.Context()).Refused = http.StatusMethodNotAllowed
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const remoteLogsLease = 30 * time.Second

// A session opens and closes outside mu, so that no request waits behind Caddy.
type sessionState int

const (
	sessionClosed sessionState = iota
	sessionOpening
	sessionOpen
	sessionClosing
)

type remoteLogs struct {
	hf    *fetcher.HTTPFetcher
	addr  string
	epoch int64
	lease time.Duration
	log   *slog.Logger

	mu          sync.Mutex
	unavailable error
	buf         *model.LogBuffer
	state       sessionState
	// transition is closed when the current opening or closing ends.
	transition chan struct{}
	stop       func() error
	last       time.Time
}

func newRemoteLogs(cfg *config, hf *fetcher.HTTPFetcher) *remoteLogs {
	l := &remoteLogs{epoch: rand.Int64(), lease: remoteLogsLease, log: cfg.logger}
	if addr, ok := logListenAddr(cfg); ok {
		l.hf, l.addr, l.buf = hf, addr, model.NewLogBuffer(0)
		// As warnIfPublicListener, before any bind: a host name is not loopback.
		if host, _, err := net.SplitHostPort(addr); err == nil && host != "localhost" && !net.ParseIP(host).IsLoopback() {
			cfg.logger.Warn("log listener is not on loopback: access log contents will be readable by any host that can reach this port", "listen", addr)
		}
	} else {
		l.unavailable = errors.New("logs are not available: Caddy is on another host and the daemon has no --log-listen")
	}
	return l
}

func (l *remoteLogs) Epoch() int64 {
	return l.epoch
}

func (l *remoteLogs) Since(after int64, limit int) ([]fetcher.LogEntry, int64, error) {
	l.mu.Lock()
	if err := l.unavailable; err != nil {
		l.mu.Unlock()
		return nil, 0, err
	}
	opening := l.state == sessionClosed
	if opening {
		l.state, l.transition = sessionOpening, make(chan struct{})
	} else if after < 0 {
		after = l.buf.WriteCount()
	}
	l.last = time.Now()
	l.mu.Unlock()
	if opening {
		// Clear our own sinks first: a killed daemon leaves them on a dead port, and Caddy refuses a PUT over them (409).
		for _, name := range []string{fetcher.DaemonLogSinks.Access, fetcher.DaemonLogSinks.Runtime} {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = l.hf.UnregisterLogSink(ctx, name)
			cancel()
		}
		addr, stop, ok := receiveCaddyLogs(l.addr, l.hf, fetcher.DaemonLogSinks, func(batch []fetcher.LogEntry) {
			for _, e := range batch {
				l.buf.Append(e)
			}
		}, func(string) {}, l.log)
		l.mu.Lock()
		close(l.transition)
		if !ok {
			l.state = sessionClosed
			l.mu.Unlock()
			return nil, 0, fmt.Errorf("logs are not available: cannot listen on %s", l.addr)
		}
		l.state, l.stop = sessionOpen, stop
		time.AfterFunc(l.lease, l.expire)
		l.log.Info("remote logs started", "listen", addr)
		l.mu.Unlock()
	}
	entries, next := l.buf.Since(after, limit)
	return entries, next, nil
}

func (l *remoteLogs) expire() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state != sessionOpen {
		return
	}
	if left := l.lease - time.Since(l.last); left > 0 {
		time.AfterFunc(left, l.expire)
		return
	}
	l.end()
}

// end closes the open session. Called with mu held, it releases it while Caddy is restored.
func (l *remoteLogs) end() {
	stop := l.stop
	l.state, l.stop, l.transition = sessionClosing, nil, make(chan struct{})
	l.mu.Unlock()
	err := stop()
	l.mu.Lock()
	l.buf.Clear()
	l.state = sessionClosed
	close(l.transition)
	if err != nil {
		l.log.Warn("remote logs stopped: Caddy not fully restored", "err", err)
		return
	}
	l.log.Info("remote logs stopped: Caddy restored")
}

func (l *remoteLogs) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.unavailable = errors.New("logs are not available: the daemon is stopping")
	for l.state == sessionOpening || l.state == sessionClosing {
		done := l.transition
		l.mu.Unlock()
		<-done
		l.mu.Lock()
	}
	if l.state == sessionOpen {
		l.end()
	}
}
