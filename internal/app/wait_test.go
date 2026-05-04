package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newWaitInst(t *testing.T, name, addr string) *waitInstance {
	t.Helper()
	return &waitInstance{name: name, addr: addr, fetcher: fetcher.NewHTTPFetcher(addr, 0)}
}

func reachableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total 100\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func unreachableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
}

func TestRunWait_AlreadyReachable(t *testing.T) {
	srv := httptest.NewServer(reachableHandler())
	defer srv.Close()

	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, []*waitInstance{newWaitInst(t, "a", srv.URL)}, 100*time.Millisecond, false)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ready")
}

func TestRunWait_BecomesReachable(t *testing.T) {
	var ready atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reachableHandler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	go func() {
		time.Sleep(150 * time.Millisecond)
		ready.Store(true)
	}()

	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, []*waitInstance{newWaitInst(t, "a", srv.URL)}, 50*time.Millisecond, false)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Waiting")
	assert.Contains(t, buf.String(), "ready")
}

func TestRunWait_Timeout(t *testing.T) {
	srv := httptest.NewServer(unreachableHandler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	err := runWait(ctx, &buf, []*waitInstance{newWaitInst(t, "a", srv.URL)}, 50*time.Millisecond, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.Contains(t, err.Error(), srv.URL)
}

func TestRunWait_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(unreachableHandler())
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	err := runWait(ctx, &buf, []*waitInstance{newWaitInst(t, "a", srv.URL)}, time.Second, false)

	require.Error(t, err)
}

func TestRunWait_Quiet(t *testing.T) {
	srv := httptest.NewServer(reachableHandler())
	defer srv.Close()

	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, []*waitInstance{newWaitInst(t, "a", srv.URL)}, 100*time.Millisecond, false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ready")

	err = runWait(context.Background(), io.Discard, []*waitInstance{newWaitInst(t, "a", srv.URL)}, 100*time.Millisecond, false)
	require.NoError(t, err)
}

func TestRunWait_Multi_AllReachable(t *testing.T) {
	srvA := httptest.NewServer(reachableHandler())
	defer srvA.Close()
	srvB := httptest.NewServer(reachableHandler())
	defer srvB.Close()

	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, []*waitInstance{
		newWaitInst(t, "a", srvA.URL),
		newWaitInst(t, "b", srvB.URL),
	}, 50*time.Millisecond, false)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "ready at "+srvA.URL)
	assert.Contains(t, out, "ready at "+srvB.URL)
}

func TestRunWait_Multi_AllWaitsForLaggard(t *testing.T) {
	var ready atomic.Bool
	srvFast := httptest.NewServer(reachableHandler())
	defer srvFast.Close()
	srvSlow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reachableHandler().ServeHTTP(w, r)
	}))
	defer srvSlow.Close()

	go func() {
		time.Sleep(150 * time.Millisecond)
		ready.Store(true)
	}()

	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, []*waitInstance{
		newWaitInst(t, "fast", srvFast.URL),
		newWaitInst(t, "slow", srvSlow.URL),
	}, 50*time.Millisecond, false)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "ready at "+srvFast.URL)
	assert.Contains(t, out, "Waiting for Caddy at "+srvSlow.URL)
	assert.Contains(t, out, "ready at "+srvSlow.URL)
	// fast should be reported only once
	assert.Equal(t, 1, strings.Count(out, "ready at "+srvFast.URL))
}

func TestRunWait_Multi_AllTimeoutNamesLaggards(t *testing.T) {
	srvOK := httptest.NewServer(reachableHandler())
	defer srvOK.Close()
	srvKO := httptest.NewServer(unreachableHandler())
	defer srvKO.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	err := runWait(ctx, &buf, []*waitInstance{
		newWaitInst(t, "alpha", srvOK.URL),
		newWaitInst(t, "bravo", srvKO.URL),
	}, 50*time.Millisecond, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.Contains(t, err.Error(), "bravo")
	assert.NotContains(t, err.Error(), "alpha")
}

func TestRunWait_Multi_AnyReturnsOnFirstReady(t *testing.T) {
	srvOK := httptest.NewServer(reachableHandler())
	defer srvOK.Close()
	srvKO := httptest.NewServer(unreachableHandler())
	defer srvKO.Close()

	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, []*waitInstance{
		newWaitInst(t, "ok", srvOK.URL),
		newWaitInst(t, "ko", srvKO.URL),
	}, 50*time.Millisecond, true)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "ready at "+srvOK.URL)
	// ko never becomes ready and should not be reported as ready
	assert.NotContains(t, out, "ready at "+srvKO.URL)
}

func TestRunWait_Multi_AnyCancelsSlowPeers(t *testing.T) {
	srvOK := httptest.NewServer(reachableHandler())
	defer srvOK.Close()
	srvSlow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srvSlow.Close()

	start := time.Now()
	var buf bytes.Buffer
	err := runWait(context.Background(), &buf, []*waitInstance{
		newWaitInst(t, "ok", srvOK.URL),
		newWaitInst(t, "slow", srvSlow.URL),
	}, 100*time.Millisecond, true)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 2*time.Second, "--any must not wait for slow peer once first instance is ready")
}

func TestRunWait_Multi_AnyTimeoutNamesAll(t *testing.T) {
	srvA := httptest.NewServer(unreachableHandler())
	defer srvA.Close()
	srvB := httptest.NewServer(unreachableHandler())
	defer srvB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	err := runWait(ctx, &buf, []*waitInstance{
		newWaitInst(t, "alpha", srvA.URL),
		newWaitInst(t, "bravo", srvB.URL),
	}, 50*time.Millisecond, true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "alpha")
	assert.Contains(t, err.Error(), "bravo")
	assert.Contains(t, err.Error(), "no Caddy instance became reachable")
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
	assert.Contains(t, out, "--any")
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
	srv := httptest.NewServer(reachableHandler())
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
	srv := httptest.NewServer(reachableHandler())
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

func TestRun_WaitCmd_MultiAddr_AllSucceeds(t *testing.T) {
	srvA := httptest.NewServer(reachableHandler())
	defer srvA.Close()
	srvB := httptest.NewServer(reachableHandler())
	defer srvB.Close()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{
		"wait",
		"--addr", "alpha=" + srvA.URL,
		"--addr", "bravo=" + srvB.URL,
		"--interval", "100ms",
		"--timeout", "2s",
	})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "ready at "+srvA.URL)
	assert.Contains(t, out, "ready at "+srvB.URL)
}

func TestRun_WaitCmd_MultiAddr_AnyFlagShortCircuits(t *testing.T) {
	srvOK := httptest.NewServer(reachableHandler())
	defer srvOK.Close()
	srvKO := httptest.NewServer(unreachableHandler())
	defer srvKO.Close()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{
		"wait",
		"--any",
		"--addr", "ok=" + srvOK.URL,
		"--addr", "ko=" + srvKO.URL,
		"--interval", "100ms",
		"--timeout", "2s",
	})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "ready at "+srvOK.URL)
}

func TestRun_WaitCmd_MultiAddr_AllTimeoutErrors(t *testing.T) {
	srvOK := httptest.NewServer(reachableHandler())
	defer srvOK.Close()
	srvKO := httptest.NewServer(unreachableHandler())
	defer srvKO.Close()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{
		"wait",
		"--addr", "ok=" + srvOK.URL,
		"--addr", "ko=" + srvKO.URL,
		"--interval", "100ms",
		"--timeout", "300ms",
	})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	assert.Contains(t, err.Error(), "ko")
}
