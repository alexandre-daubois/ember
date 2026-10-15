package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPKI struct {
	caFile, serverCert, serverKey, clientCert, clientKey string
	pool                                                 *x509.CertPool
}

func writeTestPKI(t *testing.T) testPKI {
	t.Helper()
	dir := t.TempDir()
	write := func(name, typ string, der []byte) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600))
		return path
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ember test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	ca, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	leaf := func(serial int64, cn string, usage x509.ExtKeyUsage) (string, string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: cn},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{usage},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
		require.NoError(t, err)
		keyDER, err := x509.MarshalECPrivateKey(key)
		require.NoError(t, err)
		return write(cn+".pem", "CERTIFICATE", der), write(cn+"-key.pem", "EC PRIVATE KEY", keyDER)
	}

	p := testPKI{caFile: write("ca.pem", "CERTIFICATE", caDER), pool: x509.NewCertPool()}
	p.pool.AddCert(ca)
	p.serverCert, p.serverKey = leaf(2, "server", x509.ExtKeyUsageServerAuth)
	p.clientCert, p.clientKey = leaf(3, "alice-laptop", x509.ExtKeyUsageClientAuth)
	return p
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func startRemoteServer(t *testing.T, cfg *config) (string, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	cfg.logger = slog.New(slog.NewTextHandler(logs, nil))
	cfg.serveRemote = true
	cfg.daemon = true
	if cfg.interval == 0 {
		cfg.interval = time.Second
	}
	if cfg.logSource == nil {
		cfg.logSource = &recordingLogSource{buf: model.NewLogBuffer(0)}
	}
	if cfg.relayed == nil {
		cfg.relayed = fetcher.NewHTTPFetcher("http://127.0.0.1:1", 0)
	}
	srv := newMetricsServer("127.0.0.1:0", newMetricsHandler(freshHolder(), cfg, map[string]time.Duration{"": cfg.interval}))
	require.NoError(t, configureExposeServer(srv, cfg))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { stopMetricsServer(srv) })
	return "https://" + ln.Addr().String(), logs
}

func tlsClient(pool *x509.CertPool, certs ...tls.Certificate) *http.Client {
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      pool,
		Certificates: certs,
	}}}
}

func (p testPKI) client(t *testing.T) *http.Client {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(p.clientCert, p.clientKey)
	require.NoError(t, err)
	return tlsClient(p.pool, cert)
}

func getStatus(t *testing.T, c *http.Client, url string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "ember-test/1.0")
	resp, err := c.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestValidate_ServeRemote(t *testing.T) {
	pki := writeTestPKI(t)
	base := func() *config {
		return &config{daemon: true, expose: ":9191", interval: time.Second, addrsRaw: []string{"http://localhost:2019"}, serveRemote: true}
	}
	mTLS := func(c *config) { c.exposeCert, c.exposeKey, c.exposeCA = pki.serverCert, pki.serverKey, pki.caFile }
	tests := []struct {
		name    string
		mutate  func(*config)
		wantErr string
	}{
		{"without client CA", func(*config) {}, "--serve-remote requires --expose-client-ca"},
		{"TLS without client CA", func(c *config) { c.exposeCert, c.exposeKey = pki.serverCert, pki.serverKey }, "--serve-remote requires --expose-client-ca"},
		{"without daemon", func(c *config) { c.daemon, c.expose = false, "" }, "--serve-remote requires --daemon"},
		{"cert without key", func(c *config) { c.exposeCert = pki.serverCert }, "--expose-cert and --expose-key must be set together"},
		{"key without cert", func(c *config) { c.exposeKey = pki.serverKey }, "--expose-cert and --expose-key must be set together"},
		{"client CA without TLS", func(c *config) { c.exposeCA = pki.caFile }, "--expose-client-ca requires --expose-cert and --expose-key"},
		{"mTLS", mTLS, ""},
		{"mTLS and basic auth", func(c *config) { mTLS(c); c.metricsAuth = "u:p" }, "--serve-remote is incompatible with --metrics-auth"},
		{"TLS without daemon", func(c *config) { c.daemon, c.exposeCert, c.exposeKey = false, pki.serverCert, pki.serverKey }, "--expose-cert requires --daemon"},
		{"several --addr", func(c *config) {
			mTLS(c)
			c.addrsRaw = []string{"web1=http://localhost:2019", "web2=http://localhost:2020"}
		}, "--serve-remote relays a single Caddy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.mutate(cfg)
			err := validate(cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

type countingAdmin struct {
	mu   sync.Mutex
	seen []string
}

func (a *countingAdmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.seen = append(a.seen, r.Method+" "+r.URL.Path)
	a.mu.Unlock()
	switch r.URL.Path {
	case "/frankenphp/threads":
		http.Error(w, "no FrankenPHP here", http.StatusNotFound)
		return
	case "/config/apps/pki/certificate_authorities":
		http.Error(w, `{"error":"invalid traversal path at: config/apps/pki/certificate_authorities"}`, http.StatusBadRequest)
		return
	case "/pki/ca/local":
		http.Error(w, "an authenticating proxy in front of Caddy", http.StatusUnauthorized)
		return
	}
	_, _ = w.Write([]byte("{}"))
}

func (a *countingAdmin) requests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.seen...)
}

func newCountingAdmin(t *testing.T) (*countingAdmin, *fetcher.HTTPFetcher) {
	t.Helper()
	admin := &countingAdmin{}
	srv := httptest.NewServer(admin)
	t.Cleanup(srv.Close)
	return admin, fetcher.NewHTTPFetcher(srv.URL, 0)
}

func TestNewMetricsHandler_RemoteRoutesNeedServeRemote(t *testing.T) {
	_, hf := newCountingAdmin(t)
	for _, serveRemote := range []bool{false, true} {
		cfg := &config{interval: time.Second, serveRemote: serveRemote, logSource: &recordingLogSource{buf: model.NewLogBuffer(0)}, relayed: hf}
		h := newMetricsHandler(freshHolder(), cfg, nil)
		for _, path := range []string{"/logs?after=-1", "/caddy/config/", "/certificates"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			want := http.StatusNotFound
			if serveRemote {
				want = http.StatusOK
			}
			assert.Equal(t, want, rec.Code, "%s, serveRemote=%t", path, serveRemote)
		}
	}
}

func TestServeRemote_RequiresAClientCertificate(t *testing.T) {
	pki := writeTestPKI(t)
	admin, hf := newCountingAdmin(t)
	url, logs := startRemoteServer(t, &config{exposeCert: pki.serverCert, exposeKey: pki.serverKey, exposeCA: pki.caFile, relayed: hf})
	for _, path := range []string{"/caddy/metrics", "/caddy/config/", "/logs?after=-1", "/metrics"} {
		resp, err := tlsClient(pki.pool).Get(url + path)
		if err == nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err, "a client without certificate must fail the handshake: %s", path)
	}
	assert.Empty(t, admin.requests(), "nothing reaches Caddy without a certificate")
	require.Eventually(t, func() bool { return strings.Contains(logs.String(), "TLS handshake error") }, 2*time.Second, 10*time.Millisecond)

	client := pki.client(t)
	assert.Equal(t, http.StatusOK, getStatus(t, client, url+"/caddy/metrics"))
	assert.Equal(t, http.StatusOK, getStatus(t, client, url+"/logs?after=-1"))
	assert.Equal(t, http.StatusOK, getStatus(t, client, url+"/logs?after=0"))
	assert.Equal(t, http.StatusOK, getStatus(t, client, url+"/metrics"))
	client.CloseIdleConnections()
	assert.Equal(t, []string{"GET /metrics"}, admin.requests())

	require.Eventually(t, func() bool { return strings.Contains(logs.String(), "remote session opened") }, 2*time.Second, 10*time.Millisecond)
	out := logs.String()
	assert.Equal(t, 1, strings.Count(out, `msg="remote session opened"`), "only the first request of a TUI opens a session")
	assert.Contains(t, out, "user_agent=ember-test/1.0 client_cn=alice-laptop")
	assert.NotContains(t, out, `msg="remote request refused"`)
}

func TestExposeTLS_WithoutClientCARequiresNoClientCertificate(t *testing.T) {
	pki := writeTestPKI(t)
	// Without --serve-remote: validate refuses it without --expose-client-ca.
	cfg := &config{interval: time.Second, exposeCert: pki.serverCert, exposeKey: pki.serverKey, logger: slog.New(slog.DiscardHandler)}
	srv := newMetricsServer("127.0.0.1:0", newMetricsHandler(freshHolder(), cfg, nil))
	require.NoError(t, configureExposeServer(srv, cfg))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { stopMetricsServer(srv) })

	client := tlsClient(pki.pool)
	t.Cleanup(client.CloseIdleConnections)
	assert.Equal(t, http.StatusOK, getStatus(t, client, "https://"+ln.Addr().String()+"/metrics"))
}

func TestExposeTLSConfig_ErrorsNameTheFlag(t *testing.T) {
	pki := writeTestPKI(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.pem")
	notPEM := filepath.Join(dir, "not.pem")
	require.NoError(t, os.WriteFile(notPEM, []byte("not a certificate"), 0o600))
	tests := []struct {
		name          string
		cert, key, ca string
		wantPrefix    string
	}{
		{"missing cert", missing, pki.serverKey, pki.caFile, "--expose-cert: open " + missing},
		{"missing key", pki.serverCert, missing, pki.caFile, "--expose-key: open " + missing},
		{"missing client CA", pki.serverCert, pki.serverKey, missing, "--expose-client-ca: open " + missing},
		{"client CA without PEM", pki.serverCert, pki.serverKey, notPEM, "--expose-client-ca: no valid PEM certificate in " + notPEM},
		{"key of another certificate", pki.serverCert, pki.clientKey, pki.caFile, "--expose-cert and --expose-key: tls: private key does not match public key"},
		{"cert without PEM", notPEM, pki.serverKey, pki.caFile, "--expose-cert and --expose-key: tls: failed to find any PEM data in certificate input"},
		{"key without PEM", pki.serverCert, notPEM, pki.caFile, "--expose-cert and --expose-key: tls: failed to find any PEM data in key input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exposeTLSConfig(&config{exposeCert: tt.cert, exposeKey: tt.key, exposeCA: tt.ca})
			require.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), tt.wantPrefix), err.Error())
		})
	}
}

func TestConfigureExposeServer_ReturnsTheTLSError(t *testing.T) {
	pki := writeTestPKI(t)
	cfg := &config{exposeCert: filepath.Join(t.TempDir(), "missing.pem"), exposeKey: pki.serverKey, exposeCA: pki.caFile}
	_, want := exposeTLSConfig(cfg)
	require.Error(t, want)
	srv := newMetricsServer("127.0.0.1:0", http.NotFoundHandler())

	err := configureExposeServer(srv, cfg)

	require.EqualError(t, err, want.Error())
}

func TestServeRemote_TracesTheRelayRefusals(t *testing.T) {
	pki := writeTestPKI(t)
	admin, hf := newCountingAdmin(t)
	url, logs := startRemoteServer(t, &config{exposeCert: pki.serverCert, exposeKey: pki.serverKey, exposeCA: pki.caFile, relayed: hf})
	client := pki.client(t)
	t.Cleanup(client.CloseIdleConnections)

	assert.Equal(t, http.StatusForbidden, getStatus(t, client, url+"/caddy/debug/pprof/"))
	for _, path := range []string{"/caddy/frankenphp/workers/restart", "/logs?after=-1"} {
		req, err := http.NewRequest(http.MethodPost, url+path, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode, path)
	}
	assert.Equal(t, http.StatusNotFound, getStatus(t, client, url+"/caddy/frankenphp/threads"))
	assert.Equal(t, http.StatusBadRequest, getStatus(t, client, url+"/caddy/config/apps/pki/certificate_authorities"))
	assert.Equal(t, http.StatusUnauthorized, getStatus(t, client, url+"/caddy/pki/ca/local"))

	assert.Equal(t, []string{"GET /frankenphp/threads", "GET /config/apps/pki/certificate_authorities", "GET /pki/ca/local"}, admin.requests(), "only the TUI reads reached Caddy")
	require.Eventually(t, func() bool {
		return strings.Count(logs.String(), `msg="remote request refused"`) == 3
	}, 2*time.Second, 10*time.Millisecond)
	out := logs.String()
	assert.Contains(t, out, "method=GET path=/caddy/debug/pprof/ status=403")
	assert.Contains(t, out, "method=POST path=/caddy/frankenphp/workers/restart status=405")
	assert.Contains(t, out, "method=POST path=/logs status=405")
	assert.NotContains(t, out, "remote session opened", "a refused POST opens no session")
	assert.NotContains(t, out, "/caddy/frankenphp/threads", "Caddy's own 404 is an answer, not a refusal")
	assert.NotContains(t, out, "/caddy/config/apps/pki", "nor is its 400 for a PKI app it does not have")
	assert.NotContains(t, out, "/caddy/pki/ca/local", "nor a 401 from a proxy in front of Caddy")
}

func TestServeRemote_TracesASessionWithoutLogs(t *testing.T) {
	pki := writeTestPKI(t)
	url, logs := startRemoteServer(t, &config{
		exposeCert: pki.serverCert,
		exposeKey:  pki.serverKey,
		exposeCA:   pki.caFile,
		logSource:  &recordingLogSource{err: errors.New("logs are not available: Caddy is on another host and the daemon has no --log-listen")},
	})
	client := pki.client(t)
	t.Cleanup(client.CloseIdleConnections)

	// LogsHandler reads -01 as -1, and so does the trace.
	for _, after := range []string{"-1", "-01"} {
		assert.Equal(t, http.StatusConflict, getStatus(t, client, url+"/logs?after="+after), after)
	}

	require.Eventually(t, func() bool { return strings.Count(logs.String(), `msg="remote session opened"`) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Contains(t, logs.String(), "path=/logs status=409", "the refusal of the logs is traced too")
}

func TestServeRemote_TracesTheConfigReads(t *testing.T) {
	pki := writeTestPKI(t)
	_, hf := newCountingAdmin(t)
	url, logs := startRemoteServer(t, &config{exposeCert: pki.serverCert, exposeKey: pki.serverKey, exposeCA: pki.caFile, relayed: hf})
	client := pki.client(t)
	t.Cleanup(client.CloseIdleConnections)

	for _, path := range []string{"/caddy/config/", "/caddy/config/apps/http/servers", "/caddy/metrics"} {
		assert.Equal(t, http.StatusOK, getStatus(t, client, url+path), path)
	}
	assert.Equal(t, http.StatusForbidden, getStatus(t, client, url+"/caddy/load"))

	require.Eventually(t, func() bool { return strings.Count(logs.String(), `msg="remote config read"`) == 2 }, 2*time.Second, 10*time.Millisecond)
	out := logs.String()
	assert.Contains(t, out, "path=/caddy/config/ client_cn=alice-laptop")
	assert.Contains(t, out, "path=/caddy/config/apps/http/servers client_cn=alice-laptop")
	assert.NotContains(t, out, "path=/caddy/metrics", "only Caddy's config is audited")
	assert.Contains(t, out, "path=/caddy/load status=403 client_cn=alice-laptop", "a refusal names the client too")
}

func newTestRemoteLogs(t *testing.T, cfg *config, hf *fetcher.HTTPFetcher) *remoteLogs {
	t.Helper()
	cfg.logger = slog.New(slog.DiscardHandler)
	l := newRemoteLogs(cfg, hf)
	l.lease = 300 * time.Millisecond
	t.Cleanup(l.Close)
	return l
}

func TestRemoteLogs_LeaseInstallsThenRestoresCaddy(t *testing.T) {
	api := newFakeCaddyLogAPI(t)
	t.Cleanup(api.srv.Close)
	api.addServer("srv0", "")
	l := newTestRemoteLogs(t, &config{addrs: []addrSpec{{url: api.srv.URL}}}, fetcher.NewHTTPFetcher(api.srv.URL, 0))
	assert.Zero(t, api.putCount(), "nothing is installed before a TUI asks for the logs")

	_, next, err := l.Since(-1, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, api.putCount())
	assert.JSONEq(t, "{}", api.serverLogs("srv0"))

	conn, err := net.Dial("tcp", api.registeredAddr(t))
	require.NoError(t, err)
	_, err = conn.Write([]byte(`{"level":"info","logger":"http.log.access.log0","msg":"handled"}` + "\n"))
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return l.buf.Len() == 1 }, 2*time.Second, 10*time.Millisecond)
	entries, next, err := l.Since(next, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "handled", entries[0].Message)
	joined, joinedNext, err := l.Since(-1, 0)
	require.NoError(t, err)
	assert.Empty(t, joined, "a TUI joining a running session starts from now")
	assert.Equal(t, next, joinedNext)

	for range 10 {
		time.Sleep(l.lease / 6)
		_, _, err = l.Since(next, 0)
		require.NoError(t, err)
	}
	assert.False(t, api.unregistered.Load(), "every request extends the lease")

	require.Eventually(t, func() bool {
		return api.unregistered.Load() && api.serverDeleteCount("srv0") == 1
	}, 2*time.Second, 10*time.Millisecond, "Caddy is restored once the lease expires")
	assert.Empty(t, api.serverLogs("srv0"))
	assert.Zero(t, l.buf.Len(), "the lines do not outlive the session")

	_, _, err = l.Since(next, 0)
	require.NoError(t, err)
	assert.Equal(t, 4, api.putCount(), "the next request opens a new session")
}

func TestRemoteLogs_WarnsOnceThroughTheLogger(t *testing.T) {
	api := newFakeCaddyLogAPI(t)
	t.Cleanup(api.srv.Close)
	logs := &syncBuffer{}
	cfg := &config{addrs: []addrSpec{{url: api.srv.URL}}, logListen: "0.0.0.0:0", logger: slog.New(slog.NewTextHandler(logs, nil))}

	stderr := captureStderr(t, func() {
		l := newRemoteLogs(cfg, fetcher.NewHTTPFetcher(api.srv.URL, 0))
		defer l.Close()
		for range 2 {
			_, _, err := l.Since(-1, 0)
			require.NoError(t, err)
			l.mu.Lock()
			l.end()
			l.mu.Unlock()
		}
	})

	assert.Empty(t, stderr, "nothing outside the daemon's logger, lease after lease")
	assert.Equal(t, 1, strings.Count(logs.String(), "log listener is not on loopback"))
}

func TestRemoteLogs_UnavailableWithoutLogListen(t *testing.T) {
	cfg := &config{addrs: []addrSpec{{url: "http://prod.example.com:2019"}}}

	_, _, err := newTestRemoteLogs(t, cfg, fetcher.NewHTTPFetcher(cfg.addrs[0].url, 0)).Since(0, 0)

	require.ErrorContains(t, err, "--log-listen")
}

func TestRunDaemon_ServesLogsAndRestoresCaddyOnShutdown(t *testing.T) {
	api := newFakeCaddyLogAPI(t)
	t.Cleanup(api.srv.Close)
	api.addServer("srv0", "")
	pki := writeTestPKI(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	expose := ln.Addr().String()
	require.NoError(t, ln.Close())
	cfg := &config{
		addrs:       []addrSpec{{url: api.srv.URL}},
		interval:    time.Second,
		expose:      expose,
		daemon:      true,
		serveRemote: true,
		exposeCert:  pki.serverCert,
		exposeKey:   pki.serverKey,
		exposeCA:    pki.caFile,
		logger:      slog.New(slog.DiscardHandler),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instances, err := newInstances(ctx, cfg, "v-test")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- runDaemon(ctx, instances, cfg, nil) }()

	client := pki.client(t)
	t.Cleanup(client.CloseIdleConnections)
	require.Eventually(t, func() bool {
		resp, err := client.Get("https://" + cfg.expose + "/logs?after=-1")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)
	assert.Equal(t, 2, api.putCount())

	cancel()
	require.NoError(t, <-done)

	assert.True(t, api.unregistered.Load(), "an orderly stop removes the sinks")
	assert.Equal(t, 1, api.serverDeleteCount("srv0"))
	_, _, err = cfg.logSource.Since(0, 0)
	require.ErrorContains(t, err, "stopping")
	assert.Equal(t, 2, api.putCount(), "nothing is installed after the stop")
}
