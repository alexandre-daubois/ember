package fetcher

import (
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const caddyMetrics = "# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total{server=\"srv0\"} 3\n"

type adminRequest struct{ method, path, query, host, auth, cookie string }

type fakeAdmin struct {
	mu   sync.Mutex
	seen []adminRequest
}

func (a *fakeAdmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.seen = append(a.seen, adminRequest{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Host, r.Header.Get("Authorization"), r.Header.Get("Cookie")})
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/metrics":
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(caddyMetrics))
	case "/frankenphp/threads":
		_, _ = w.Write([]byte(`{"ThreadDebugStates":[]}`))
	case "/config/apps/http/servers":
		_, _ = w.Write([]byte(`{"srv0":{"listen":[":1"],"routes":[{"match":[{"host":["127.0.0.1"]}]}]}}`))
	case "/config/apps/pki/certificate_authorities":
		_, _ = w.Write([]byte(`{"local":{}}`))
	case "/pki/ca/local":
		_, _ = w.Write([]byte(`{"id":"local"}`))
	default:
		_, _ = w.Write([]byte(`{}`))
	}
}

func (a *fakeAdmin) requests() []adminRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.seen)
}

func newRelayTest(t *testing.T) (*fakeAdmin, string, http.Handler) {
	t.Helper()
	admin := &fakeAdmin{}
	srv := httptest.NewServer(admin)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return admin, u.Host, NewHTTPFetcher(srv.URL, 0).Relay()
}

func relay(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.SetBasicAuth("remote", "s3cret")
	req.Header.Set("Cookie", "session=1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRelay_ForwardsTheTUIReadsAlone(t *testing.T) {
	for _, path := range []string{
		"/metrics",
		"/frankenphp/threads",
		"/config/",
		"/config/apps/http/servers",
		"/config/apps/pki/certificate_authorities",
		"/pki/ca/local",
		"/pki/ca/my%20ca",
	} {
		admin, host, h := newRelayTest(t)

		rec := relay(h, http.MethodGet, path+"?pretty=1")

		require.Equal(t, http.StatusOK, rec.Code, path)
		require.Len(t, admin.requests(), 1, path)
		got := admin.requests()[0]
		assert.Equal(t, adminRequest{method: http.MethodGet, path: path, host: host}, got,
			"%s: same path, no query, the fetcher's Host, and no client header", path)
		assert.NotEmpty(t, rec.Header().Get("Content-Type"), path)
	}
}

func TestRelay_RefusesEverythingElse(t *testing.T) {
	tests := []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/frankenphp/workers/restart", http.StatusMethodNotAllowed},
		{http.MethodPost, "/config/", http.StatusMethodNotAllowed},
		{http.MethodPut, "/config/", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/config/", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/config/", http.StatusMethodNotAllowed},
		{http.MethodHead, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodOptions, "/metrics", http.StatusMethodNotAllowed},
		{http.MethodGet, "/debug/pprof/", http.StatusForbidden},
		{http.MethodGet, "/debug/pprof/goroutine", http.StatusForbidden},
		{http.MethodGet, "/load", http.StatusForbidden},
		{http.MethodGet, "/stop", http.StatusForbidden},
		{http.MethodGet, "/id/srv0", http.StatusForbidden},
		{http.MethodGet, "/config", http.StatusForbidden},
		{http.MethodGet, "/config/apps", http.StatusForbidden},
		{http.MethodGet, "/config/apps/frankenphp", http.StatusForbidden},
		{http.MethodGet, "/config/logging/logs/__ember__", http.StatusForbidden},
		{http.MethodGet, "/reverse_proxy/upstreams", http.StatusForbidden},
		{http.MethodGet, "/frankenphp/workers/restart", http.StatusForbidden},
		{http.MethodGet, "/pki/ca/", http.StatusForbidden},
		{http.MethodGet, "/pki/ca/local/certificates", http.StatusForbidden},
		{http.MethodGet, "/pki/ca/local%2Fcertificates", http.StatusForbidden},
		{http.MethodGet, "/pki/ca/..", http.StatusForbidden},
	}
	for _, tt := range tests {
		admin, _, h := newRelayTest(t)

		rec := relay(h, tt.method, tt.path)

		assert.Equal(t, tt.status, rec.Code, "%s %s", tt.method, tt.path)
		assert.Empty(t, admin.requests(), "%s %s must not reach Caddy", tt.method, tt.path)
		if tt.status == http.StatusMethodNotAllowed {
			assert.Equal(t, http.MethodGet, rec.Header().Get("Allow"))
		}
	}
}

func TestRelay_CaddyUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	rec := relay(NewHTTPFetcher("http://"+addr, 0).Relay(), http.MethodGet, "/metrics")

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestRelay_UnixSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets are not supported on Windows")
	}
	sockPath := unixSocketPath(t)
	l, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	admin := &fakeAdmin{}
	srv := &http.Server{Handler: admin}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })

	rec := relay(NewHTTPFetcher("unix/"+sockPath, 0).Relay(), http.MethodGet, "/config/")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, admin.requests(), 1)
	assert.Equal(t, "/config/", admin.requests()[0].path)
}

func TestRelay_PassesEveryRequestOfARemoteTUI(t *testing.T) {
	admin, _, h := newRelayTest(t)
	hf := newRemoteTest(t, http.StripPrefix("/caddy", h))
	ctx := context.Background()

	require.True(t, hf.DetectFrankenPHP(ctx))
	require.NotNil(t, hf.FetchServerNames(ctx))
	snap, err := hf.Fetch(ctx)
	require.NoError(t, err)
	assert.True(t, snap.Metrics.HasHTTPMetrics)
	_, err = hf.FetchConfig(ctx)
	require.NoError(t, err)
	hf.FetchPKICertificates(ctx)

	var reached []string
	for _, r := range admin.requests() {
		assert.Equal(t, http.MethodGet, r.method, r.path)
		if !slices.Contains(reached, r.path) {
			reached = append(reached, r.path)
		}
	}
	assert.ElementsMatch(t, append(slices.Clone(tuiReads), "/pki/ca/local"), reached,
		"every read of the list is used, and nothing else reaches Caddy")
}

func TestRelay_GzipsForAClientThatAcceptsIt(t *testing.T) {
	_, _, h := newRelayTest(t)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/plain; version=0.0.4", rec.Header().Get("Content-Type"))
	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	zr, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	body, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, caddyMetrics, string(body))
}

func TestRelay_CopiesPlainForAClientThatDoesNotAcceptGzip(t *testing.T) {
	_, _, h := newRelayTest(t)

	rec := relay(h, http.MethodGet, "/metrics")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, caddyMetrics, rec.Body.String())
}

func TestRelay_TheTUIClientReadsCaddysAnswerUnchanged(t *testing.T) {
	_, _, h := newRelayTest(t)
	f := newRemoteTest(t, http.StripPrefix("/caddy", h))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, f.baseURL+"/metrics", nil)
	require.NoError(t, err)

	resp, err := f.httpClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.True(t, resp.Uncompressed, "the relay gzipped the answer and the TUI's client undid it")
	assert.Equal(t, caddyMetrics, string(body))
}
