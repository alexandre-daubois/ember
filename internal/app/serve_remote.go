package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	if cfg.serveRemote {
		srv.Handler = traceRemote(srv.Handler, cfg.logger)
	}
	return nil
}

func listenMetrics(srv *http.Server) error {
	if srv.TLSConfig != nil {
		return srv.ListenAndServeTLS("", "")
	}
	return srv.ListenAndServe()
}

func traceRemote(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayed := strings.HasPrefix(r.URL.Path, "/caddy/")
		if r.URL.Path != "/logs" && r.URL.Path != "/certificates" && !relayed {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		daemonRefused := !relayed || rec.status == http.StatusForbidden || rec.status == http.StatusMethodNotAllowed
		var cn []any
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			cn = []any{"client_cn", r.TLS.PeerCertificates[0].Subject.CommonName}
		}
		if rec.status >= http.StatusBadRequest && rec.status < http.StatusInternalServerError && daemonRefused {
			log.Warn("remote request refused", append([]any{"remote_addr", r.RemoteAddr, "method", r.Method, "path", r.URL.Path, "status", rec.status}, cn...)...)
		}
		after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if r.URL.Path == "/logs" && err == nil && after == -1 && rec.status != http.StatusForbidden && rec.status != http.StatusMethodNotAllowed {
			log.Info("remote session opened", append([]any{"remote_addr", r.RemoteAddr, "user_agent", r.UserAgent()}, cn...)...)
		}
		if rec.status == http.StatusOK && strings.HasPrefix(r.URL.Path, "/caddy/config/") {
			log.Info("remote config read", append([]any{"remote_addr", r.RemoteAddr, "path", r.URL.Path}, cn...)...)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

const remoteLogsLease = 30 * time.Second

type remoteLogs struct {
	hf    *fetcher.HTTPFetcher
	addr  string
	lease time.Duration
	log   *slog.Logger

	mu          sync.Mutex
	unavailable error
	buf         *model.LogBuffer
	stop        func()
	last        time.Time
}

func newRemoteLogs(cfg *config, hf *fetcher.HTTPFetcher) *remoteLogs {
	l := &remoteLogs{lease: remoteLogsLease, log: cfg.logger}
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

func (l *remoteLogs) Since(after int64, limit int) ([]fetcher.LogEntry, int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.unavailable != nil {
		return nil, 0, l.unavailable
	}
	if l.stop == nil {
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
		}, func(string) {})
		if !ok {
			return nil, 0, fmt.Errorf("logs are not available: cannot listen on %s", l.addr)
		}
		l.stop = stop
		time.AfterFunc(l.lease, l.expire)
		l.log.Info("remote logs started", "listen", addr)
	} else if after < 0 {
		after = l.buf.WriteCount()
	}
	l.last = time.Now()
	entries, next := l.buf.Since(after, limit)
	return entries, next, nil
}

func (l *remoteLogs) expire() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stop == nil {
		return
	}
	if left := l.lease - time.Since(l.last); left > 0 {
		time.AfterFunc(left, l.expire)
		return
	}
	l.end()
}

func (l *remoteLogs) end() {
	l.stop()
	l.stop = nil
	l.buf.Clear()
	l.log.Info("remote logs stopped: Caddy restored")
}

func (l *remoteLogs) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stop != nil {
		l.end()
	}
	l.unavailable = errors.New("logs are not available: the daemon is stopping")
}
