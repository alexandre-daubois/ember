package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWait_AlreadyReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(200)
			w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total 100\n"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, f, srv.URL, 100*time.Millisecond)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ready")
}

func TestRunWait_BecomesReachable(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(500)
			return
		}
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(200)
			w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total 100\n"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	go func() {
		time.Sleep(150 * time.Millisecond)
		ready.Store(true)
	}()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, f, srv.URL, 50*time.Millisecond)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Waiting")
	assert.Contains(t, buf.String(), "ready")
}

func TestRunWait_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runWait(ctx, &buf, f, srv.URL, 50*time.Millisecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestRunWait_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runWait(ctx, &buf, f, srv.URL, time.Second)

	require.Error(t, err)
}

func TestRun_WaitHelp(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"wait", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Caddy")
	assert.Contains(t, out, "--timeout")
}

func TestRun_WaitQuietFlag(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"wait", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "--quiet")
	assert.Contains(t, buf.String(), "-q")
}

func TestRunWait_Quiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(200)
			w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total 100\n"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, f, srv.URL, 100*time.Millisecond)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ready")

	// with io.Discard (quiet mode), output is suppressed
	err = runWait(context.Background(), io.Discard, f, srv.URL, 100*time.Millisecond)
	require.NoError(t, err)
}

func TestRun_WaitInheritsAddr(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"wait", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "--addr")
}

func TestRun_WaitCmd_ExecutesRunE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total 1\n"))
	}))
	defer srv.Close()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"wait", "--addr", srv.URL, "--interval", "100ms", "--timeout", "2s"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "ready",
		"wait command must succeed end-to-end against a reachable server")
}

func TestRun_WaitCmd_QuietSuppressesOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total 1\n"))
	}))
	defer srv.Close()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"wait", "--quiet", "--addr", srv.URL, "--interval", "100ms", "--timeout", "2s"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	require.NoError(t, cmd.Execute())
	assert.NotContains(t, buf.String(), "ready",
		"--quiet must keep stdout empty even on success")
}
