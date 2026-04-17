package app

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCaddyLogAPI is a minimal stub of the bits of Caddy's admin API that
// setupLogSource exercises: GET /config/logging/logs (sink discovery),
// PUT/DELETE /config/logging/logs/<name> (hot sink registration), and
// GET /config/apps/http/servers + GET/POST/DELETE on each server's logs path
// (auto-enabling access logs).
type fakeCaddyLogAPI struct {
	srv          *httptest.Server
	mu           atomic.Pointer[string]
	registered   atomic.Bool
	unregistered atomic.Bool
	// callOrder records "PUT" and "DELETE" requests against the ember sink
	// path, in the order they arrive.
	callOrderMu sync.Mutex
	callOrder   []string

	// sinkGone simulates a Caddy reload that wipes the runtime sink config.
	sinkGone atomic.Bool
	// unavailable simulates Caddy not being reachable at all.
	unavailable atomic.Bool

	// servers maps server name to its current logs JSON (empty string = unset).
	serversMu sync.Mutex
	servers   map[string]string
	// serverPosts and serverDeletes count POST/DELETE on /servers/<n>/logs.
	serverPosts   map[string]int
	serverDeletes map[string]int
}

func newFakeCaddyLogAPI(t *testing.T) *fakeCaddyLogAPI {
	t.Helper()
	api := &fakeCaddyLogAPI{
		servers:       make(map[string]string),
		serverPosts:   make(map[string]int),
		serverDeletes: make(map[string]int),
	}
	api.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if api.unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.URL.Path == "/config/logging/logs" && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		case strings.HasPrefix(r.URL.Path, "/config/logging/logs/") && r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			if api.sinkGone.Load() {
				_, _ = io.WriteString(w, "null")
			} else {
				raw := api.mu.Load()
				if raw != nil {
					_, _ = io.WriteString(w, *raw)
				} else {
					_, _ = io.WriteString(w, "null")
				}
			}
		case strings.HasPrefix(r.URL.Path, "/config/logging/logs/") && r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			s := string(body)
			api.mu.Store(&s)
			api.registered.Store(true)
			api.sinkGone.Store(false)
			api.callOrderMu.Lock()
			api.callOrder = append(api.callOrder, "PUT")
			api.callOrderMu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/config/logging/logs/") && r.Method == http.MethodDelete:
			api.unregistered.Store(true)
			api.callOrderMu.Lock()
			api.callOrder = append(api.callOrder, "DELETE")
			api.callOrderMu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/config/apps/http/servers" && r.Method == http.MethodGet:
			api.serversMu.Lock()
			out := make(map[string]any, len(api.servers))
			for k := range api.servers {
				out[k] = struct{}{}
			}
			api.serversMu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, "/config/apps/http/servers/") && strings.HasSuffix(r.URL.Path, "/logs"):
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/config/apps/http/servers/"), "/logs")
			api.serversMu.Lock()
			defer api.serversMu.Unlock()
			switch r.Method {
			case http.MethodGet:
				w.WriteHeader(http.StatusOK)
				if v := api.servers[name]; v != "" {
					_, _ = io.WriteString(w, v)
				} else {
					_, _ = io.WriteString(w, "null")
				}
			case http.MethodPost:
				body, _ := io.ReadAll(r.Body)
				api.servers[name] = strings.TrimSpace(string(body))
				api.serverPosts[name]++
				w.WriteHeader(http.StatusOK)
			case http.MethodDelete:
				api.servers[name] = ""
				api.serverDeletes[name]++
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	return api
}

func (a *fakeCaddyLogAPI) addServer(name, logsJSON string) {
	a.serversMu.Lock()
	a.servers[name] = logsJSON
	a.serversMu.Unlock()
}

func (a *fakeCaddyLogAPI) serverPostCount(name string) int {
	a.serversMu.Lock()
	defer a.serversMu.Unlock()
	return a.serverPosts[name]
}

func (a *fakeCaddyLogAPI) serverDeleteCount(name string) int {
	a.serversMu.Lock()
	defer a.serversMu.Unlock()
	return a.serverDeletes[name]
}

func (a *fakeCaddyLogAPI) serverLogs(name string) string {
	a.serversMu.Lock()
	defer a.serversMu.Unlock()
	return a.servers[name]
}

func (a *fakeCaddyLogAPI) putCount() int {
	a.callOrderMu.Lock()
	defer a.callOrderMu.Unlock()
	n := 0
	for _, v := range a.callOrder {
		if v == "PUT" {
			n++
		}
	}
	return n
}

func (a *fakeCaddyLogAPI) registeredAddr(t *testing.T) string {
	t.Helper()
	raw := a.mu.Load()
	require.NotNil(t, raw, "no sink registered")
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(*raw), &parsed))
	writer, ok := parsed["writer"].(map[string]any)
	require.True(t, ok)
	addr, ok := writer["address"].(string)
	require.True(t, ok)
	return strings.TrimPrefix(addr, "tcp/")
}

func TestSetupLogSource_AutoRegistersAndReceivesPushedLogs(t *testing.T) {
	api := newFakeCaddyLogAPI(t)
	defer api.srv.Close()

	cfg := &config{addr: api.srv.URL}
	f := fetcher.NewHTTPFetcher(api.srv.URL, 0)

	uiCfg := ui.Config{}
	cleanup := setupLogSource(cfg, f, &uiCfg)
	defer cleanup()

	require.True(t, api.registered.Load(), "expected sink to be registered with Caddy")
	require.NotNil(t, uiCfg.LogBuffer, "expected a log buffer to be wired up")
	require.Contains(t, uiCfg.LogSource, "net ", "log source should be the net listener")

	listenAddr := api.registeredAddr(t)
	conn, err := net.Dial("tcp", listenAddr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	line := `{"level":"info","ts":1742472000.0,"msg":"handled","request":{"method":"GET","host":"a.com","uri":"/"},"status":200}` + "\n"
	_, err = conn.Write([]byte(line))
	require.NoError(t, err)

	deadline := time.Now().Add(2 * time.Second)
	for uiCfg.LogBuffer.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, 1, uiCfg.LogBuffer.Len(), "log line should reach the UI buffer")

	cleanup()
	// Cleanup runs again via defer — UnregisterEmberLogSink is idempotent.
	deadline = time.Now().Add(2 * time.Second)
	for !api.unregistered.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, api.unregistered.Load(), "expected sink to be unregistered on cleanup")

	// Since PUT on the sink endpoint is idempotent (Caddy replaces
	// any existing sink), Register should run without a prior defensive
	// DELETE: failing to register leaves the previous state untouched.
	api.callOrderMu.Lock()
	defer api.callOrderMu.Unlock()
	require.GreaterOrEqual(t, len(api.callOrder), 2)
	assert.Equal(t, "PUT", api.callOrder[0], "register must be the first admin API write")
	assert.Equal(t, "DELETE", api.callOrder[1], "cleanup DELETE follows the register")
}

func TestSetupLogSource_NoLogSourceForRemoteCaddyWithoutFlag(t *testing.T) {
	// Stub a remote Caddy (host != localhost). No --log-listen means we
	// cannot give Caddy an address it can reach, so we abstain.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config{addr: "http://prod.example.com:2019"}
	f := fetcher.NewHTTPFetcher(srv.URL, 0)

	uiCfg := ui.Config{}
	cleanup := setupLogSource(cfg, f, &uiCfg)
	defer cleanup()

	assert.Nil(t, uiCfg.LogBuffer, "remote Caddy without --log-listen must not auto-register")
	assert.Empty(t, uiCfg.LogSource)
}

func TestSetupLogSource_AutoEnablesAccessLogsAndRestores(t *testing.T) {
	api := newFakeCaddyLogAPI(t)
	defer api.srv.Close()
	api.addServer("srv0", "")                           // not configured: should be enabled
	api.addServer("srv1", "")                           // also empty: should be enabled
	api.addServer("user", `{"logger_names":{"x":"y"}}`) // user-defined: leave alone

	cfg := &config{addr: api.srv.URL}
	f := fetcher.NewHTTPFetcher(api.srv.URL, 0)

	uiCfg := ui.Config{}
	cleanup := setupLogSource(cfg, f, &uiCfg)

	assert.Equal(t, 1, api.serverPostCount("srv0"))
	assert.Equal(t, 1, api.serverPostCount("srv1"))
	assert.Equal(t, 0, api.serverPostCount("user"), "must not POST when user has logs config")
	assert.Equal(t, "{}", api.serverLogs("srv0"))
	assert.Equal(t, `{"logger_names":{"x":"y"}}`, api.serverLogs("user"))

	cleanup()

	assert.Equal(t, 1, api.serverDeleteCount("srv0"), "should restore srv0 we enabled")
	assert.Equal(t, 1, api.serverDeleteCount("srv1"), "should restore srv1 we enabled")
	assert.Equal(t, 0, api.serverDeleteCount("user"), "must not delete user-owned logs config")
}

// Make sure the cleanup function is non-nil and safe to call when no source
// is wired up.
func TestSetupLogSource_CleanupSafeWhenNoSource(t *testing.T) {
	cfg := &config{addr: "http://prod.example.com:2019"}
	cleanup := setupLogSource(cfg, &dummyFetcher{}, &ui.Config{})
	require.NotNil(t, cleanup)
	cleanup()
}

func TestSetupLogSource_WatchdogReregistersAfterCaddyReload(t *testing.T) {
	// Lower the watchdog interval for this test so we don't have to wait 30s.
	orig := sinkWatchdogInterval
	sinkWatchdogInterval = 100 * time.Millisecond
	defer func() { sinkWatchdogInterval = orig }()

	api := newFakeCaddyLogAPI(t)
	defer api.srv.Close()
	api.addServer("srv0", "")

	cfg := &config{addr: api.srv.URL}
	f := fetcher.NewHTTPFetcher(api.srv.URL, 0)

	uiCfg := ui.Config{}
	cleanup := setupLogSource(cfg, f, &uiCfg)
	defer cleanup()

	require.True(t, api.registered.Load())
	require.Equal(t, 1, api.putCount(), "initial registration should be a single PUT")

	// Simulate a Caddy reload that wipes the runtime sink.
	api.sinkGone.Store(true)

	// Wait for the watchdog to re-register the sink AND re-enable access
	// logs. The watchdog does them sequentially on each tick, so under -race
	// the PUT can be observable before the POST completes.
	deadline := time.Now().Add(2 * time.Second)
	for (api.putCount() < 2 || api.serverPostCount("srv0") < 2) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, api.putCount(), 2, "watchdog should re-register the sink after it disappears")
	assert.False(t, api.sinkGone.Load(), "re-registration should clear the sinkGone flag")
	assert.GreaterOrEqual(t, api.serverPostCount("srv0"), 2, "watchdog should re-enable access logs")
}

func TestSetupLogSource_UnresolvableHostFallsBackToPort(t *testing.T) {
	api := newFakeCaddyLogAPI(t)
	defer api.srv.Close()

	cfg := &config{
		addr:      api.srv.URL,
		logListen: "this-host-does-not-resolve.invalid:9555",
	}
	f := fetcher.NewHTTPFetcher(api.srv.URL, 0)

	uiCfg := ui.Config{}
	cleanup := setupLogSource(cfg, f, &uiCfg)
	defer cleanup()

	require.NotNil(t, uiCfg.LogBuffer, "should fall back to binding on :port when host is unresolvable")
	assert.Equal(t, "net this-host-does-not-resolve.invalid:9555", uiCfg.LogSource,
		"advertised address must be the original --log-listen value")

	addr := api.registeredAddr(t)
	assert.Equal(t, "this-host-does-not-resolve.invalid:9555", addr,
		"Caddy must receive the original address, not the local bind address")
}

func TestSetupLogSource_EnablesAccessLogsOnSecondTick(t *testing.T) {
	orig := sinkWatchdogInterval
	sinkWatchdogInterval = 100 * time.Millisecond
	defer func() { sinkWatchdogInterval = orig }()

	api := newFakeCaddyLogAPI(t)
	defer api.srv.Close()
	// No servers initially: simulates Caddy starting before its HTTP config
	// is fully loaded.
	api.unavailable.Store(true)

	cfg := &config{addr: api.srv.URL}
	f := fetcher.NewHTTPFetcher(api.srv.URL, 0)

	uiCfg := ui.Config{}
	cleanup := setupLogSource(cfg, f, &uiCfg)
	defer cleanup()

	// Caddy becomes reachable but has no servers yet.
	api.unavailable.Store(false)

	deadline := time.Now().Add(2 * time.Second)
	for !api.registered.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	require.True(t, api.registered.Load(), "sink should be registered")
	assert.Equal(t, 0, api.serverPostCount("srv0"), "no server exists yet")

	// Servers appear (delayed config load).
	api.addServer("srv0", "")

	deadline = time.Now().Add(2 * time.Second)
	for api.serverPostCount("srv0") == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, api.serverPostCount("srv0"), 1,
		"watchdog must retry enableAccessLogs even when the sink already exists")
}

func TestSetupLogSource_RecoversWhenCaddyStartsLate(t *testing.T) {
	orig := sinkWatchdogInterval
	sinkWatchdogInterval = 100 * time.Millisecond
	defer func() { sinkWatchdogInterval = orig }()

	api := newFakeCaddyLogAPI(t)
	defer api.srv.Close()
	api.addServer("srv0", "")
	api.unavailable.Store(true)

	cfg := &config{addr: api.srv.URL}
	f := fetcher.NewHTTPFetcher(api.srv.URL, 0)

	uiCfg := ui.Config{}
	cleanup := setupLogSource(cfg, f, &uiCfg)
	defer cleanup()

	require.NotNil(t, uiCfg.LogBuffer, "buffer must be created even when initial registration fails")
	require.Contains(t, uiCfg.LogSource, "net ")
	assert.False(t, api.registered.Load(), "sink should not be registered while Caddy is unavailable")

	// Simulate Caddy becoming available.
	api.unavailable.Store(false)

	// Wait for both conditions: sink registered AND access logs enabled.
	// The watchdog does them sequentially on each tick, so under -race the
	// first can be observable before the second completes.
	deadline := time.Now().Add(2 * time.Second)
	for (!api.registered.Load() || api.serverPostCount("srv0") == 0) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	assert.True(t, api.registered.Load(), "watchdog should register the sink once Caddy becomes available")
	assert.GreaterOrEqual(t, api.serverPostCount("srv0"), 1, "access logs should be enabled after late registration")
}

type dummyFetcher struct{}

func (dummyFetcher) Fetch(_ context.Context) (*fetcher.Snapshot, error) {
	return &fetcher.Snapshot{}, nil
}
