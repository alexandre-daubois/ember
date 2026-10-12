package fetcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRemoteTest(t *testing.T, h http.Handler) *RemoteFetcher {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	f := NewRemoteFetcher(u)
	t.Cleanup(f.CloseIdleConnections)
	return f
}

func TestRemoteFetcher_OnlyA4xxIsARefusal(t *testing.T) {
	for status, refused := range map[int]bool{
		http.StatusNotFound:           true,
		http.StatusUnauthorized:       true,
		http.StatusServiceUnavailable: false,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newRemoteTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))

			_, err := f.FetchLogs(context.Background(), -1)

			require.Error(t, err)
			if refused {
				var r RefusedError
				require.ErrorAs(t, err, &r)
				assert.Equal(t, status, r.Status)
			} else {
				assert.NotErrorAs(t, err, new(RefusedError))
			}
		})
	}
}

type recordingServer struct {
	mu   sync.Mutex
	seen []string
	next http.Handler
}

func (s *recordingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.seen = append(s.seen, r.URL.Path)
	s.mu.Unlock()
	s.next.ServeHTTP(w, r)
}

func (s *recordingServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

func TestRemoteFetcher_TheDaemonDialsTheTLSHosts(t *testing.T) {
	daemon := &recordingServer{next: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]CertificateInfo{{Subject: "shop.test", Source: "tls"}})
	})}
	f := newRemoteTest(t, daemon)

	certs := f.DialTLSCertificates(context.Background(), []string{"127.0.0.1"})

	require.Len(t, certs, 1)
	assert.Equal(t, "shop.test", certs[0].Subject)
	require.Len(t, daemon.requests(), 1, "the TUI dials nothing itself")
	assert.Equal(t, "/certificates", daemon.requests()[0])
}

func TestRemoteFetcher_RefusesToRestartWorkers(t *testing.T) {
	daemon := &recordingServer{next: http.NotFoundHandler()}
	f := newRemoteTest(t, daemon)

	err := f.RestartWorkers(context.Background())

	require.EqualError(t, err, "worker restart is not available in a remote session")
	assert.Empty(t, daemon.requests(), "nothing reaches the daemon")
}
