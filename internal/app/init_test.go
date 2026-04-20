package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type initServer struct {
	metricsEnabled bool
	hasFrankenPHP  bool
	hasHTTPMetrics bool
	wildcardHost   bool
	servers        map[string]any
	fpConfig       *fetcher.FrankenPHPConfig
	enabledMetrics bool
}

func newInitTestServer(s *initServer) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/config/" && r.Method == http.MethodGet:
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]any{"admin": map[string]any{"listen": "localhost:2019"}})

		case r.URL.Path == "/config/apps/http/servers" && r.Method == http.MethodGet:
			if s.servers != nil {
				w.WriteHeader(200)
				json.NewEncoder(w).Encode(s.servers)
			} else {
				w.WriteHeader(404)
			}

		case r.URL.Path == "/config/apps/http/metrics" && r.Method == http.MethodGet:
			w.WriteHeader(200)
			if s.metricsEnabled || s.enabledMetrics {
				json.NewEncoder(w).Encode(map[string]any{})
			} else {
				w.Write([]byte("null"))
			}

		case r.URL.Path == "/config/apps/http/metrics" && r.Method == http.MethodPost:
			s.enabledMetrics = true
			w.WriteHeader(200)

		case r.URL.Path == "/frankenphp/threads" && r.Method == http.MethodGet:
			if s.hasFrankenPHP {
				w.WriteHeader(200)
				json.NewEncoder(w).Encode(fetcher.ThreadsResponse{
					ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0}},
				})
			} else {
				w.WriteHeader(404)
			}

		case r.URL.Path == "/config/apps/frankenphp" && r.Method == http.MethodGet:
			if s.fpConfig != nil {
				w.WriteHeader(200)
				json.NewEncoder(w).Encode(s.fpConfig)
			} else {
				w.WriteHeader(404)
			}

		case r.URL.Path == "/metrics" && r.Method == http.MethodGet:
			w.WriteHeader(200)
			if s.hasHTTPMetrics && s.wildcardHost {
				w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total{code=\"200\"} 100\n# TYPE caddy_http_request_duration_seconds histogram\ncaddy_http_request_duration_seconds_bucket{le=\"+Inf\"} 100\ncaddy_http_request_duration_seconds_sum 5.0\ncaddy_http_request_duration_seconds_count 100\n"))
			} else if s.hasHTTPMetrics {
				w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total{host=\"test.com\",code=\"200\"} 100\n"))
			}
			if s.hasFrankenPHP {
				w.Write([]byte("# TYPE frankenphp_total_threads counter\nfrankenphp_total_threads 20\n"))
			}

		default:
			w.WriteHeader(404)
		}
	}))
}

func TestRunInit_FullSetup(t *testing.T) {
	is := &initServer{
		metricsEnabled: true,
		hasFrankenPHP:  true,
		hasHTTPMetrics: true,
		servers:        map[string]any{"srv0": nil, "srv1": nil},
		fpConfig: &fetcher.FrankenPHPConfig{
			NumThreads: 20,
			Workers: []fetcher.FrankenPHPWorkerConfig{
				{FileName: "/app/worker.php", Num: 8},
			},
		},
	}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader(""), f, srv.URL, true)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Admin API reachable")
	assert.Contains(t, out, "2 HTTP server(s)")
	assert.Contains(t, out, "HTTP metrics enabled")
	assert.Contains(t, out, "FrankenPHP detected")
	assert.Contains(t, out, "20 threads")
	assert.Contains(t, out, "/app/worker.php")
	assert.Contains(t, out, "Ember is ready")
}

func TestRunInit_CaddyOnly(t *testing.T) {
	is := &initServer{
		metricsEnabled: true,
		hasHTTPMetrics: true,
		servers:        map[string]any{"main": nil},
	}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader(""), f, srv.URL, true)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "Admin API reachable")
	assert.Contains(t, out, "FrankenPHP not detected")
	assert.Contains(t, out, "Ember is ready")
}

func TestRunInit_EnablesMetrics(t *testing.T) {
	is := &initServer{
		metricsEnabled: false,
		servers:        map[string]any{"srv0": nil},
	}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader("y\n"), f, srv.URL, false)

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "not enabled")
	assert.Contains(t, out, "Enable HTTP metrics")
	assert.Contains(t, out, "HTTP metrics enabled")
	assert.True(t, is.enabledMetrics)
}

func TestRunInit_EnablesMetricsAutoYes(t *testing.T) {
	is := &initServer{metricsEnabled: false}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader(""), f, srv.URL, true)

	require.NoError(t, err)
	assert.True(t, is.enabledMetrics)
}

func TestRunInit_SkipsMetricsOnNo(t *testing.T) {
	is := &initServer{metricsEnabled: false}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader("n\n"), f, srv.URL, false)

	require.NoError(t, err)
	assert.False(t, is.enabledMetrics)
	assert.Contains(t, buf.String(), "Skipped")
	assert.Contains(t, buf.String(), "{ metrics }")
}

func TestRunInit_AccessLogs_AnnouncesHotSink(t *testing.T) {
	is := &initServer{metricsEnabled: true, servers: map[string]any{"srv0": nil}}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	require.NoError(t, runInit(context.Background(), &buf, strings.NewReader(""), f, srv.URL, true))

	out := buf.String()
	assert.Contains(t, out, "Access logs")
	assert.Contains(t, out, "hot-registered")
}

func TestRunInit_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader(""), f, srv.URL, true)

	require.Error(t, err)
	assert.Contains(t, buf.String(), "✗")
}

func TestPromptYesNo_AutoYes(t *testing.T) {
	var buf bytes.Buffer
	assert.True(t, promptYesNo(&buf, strings.NewReader(""), "test?", true))
}

func TestPromptYesNo_EmptyIsYes(t *testing.T) {
	var buf bytes.Buffer
	assert.True(t, promptYesNo(&buf, strings.NewReader("\n"), "test?", false))
}

func TestPromptYesNo_YIsYes(t *testing.T) {
	var buf bytes.Buffer
	assert.True(t, promptYesNo(&buf, strings.NewReader("y\n"), "test?", false))
}

func TestPromptYesNo_YesIsYes(t *testing.T) {
	var buf bytes.Buffer
	assert.True(t, promptYesNo(&buf, strings.NewReader("yes\n"), "test?", false))
}

func TestPromptYesNo_NIsNo(t *testing.T) {
	var buf bytes.Buffer
	assert.False(t, promptYesNo(&buf, strings.NewReader("n\n"), "test?", false))
}

func TestPromptYesNo_EOFIsNo(t *testing.T) {
	var buf bytes.Buffer
	assert.False(t, promptYesNo(&buf, strings.NewReader(""), "test?", false))
}

func TestRun_InitCmd_ExecutesRunE(t *testing.T) {
	is := &initServer{
		metricsEnabled: true,
		hasHTTPMetrics: true,
		servers:        map[string]any{"main": nil},
	}
	srv := newInitTestServer(is)
	defer srv.Close()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"init", "--addr", srv.URL, "-y"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "Ember is ready",
		"init command must complete the full pipeline against a stubbed admin API")
}

func TestRun_InitCmd_QuietSuppressesOutput(t *testing.T) {
	is := &initServer{
		metricsEnabled: true,
		hasHTTPMetrics: true,
		servers:        map[string]any{"main": nil},
	}
	srv := newInitTestServer(is)
	defer srv.Close()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"init", "--addr", srv.URL, "-yq"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	require.NoError(t, cmd.Execute())
	assert.NotContains(t, buf.String(), "Ember is ready",
		"-q must keep stdout silent on success — only the exit code carries the verdict")
}

func TestRun_InitHelp(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"init", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "admin API")
	assert.Contains(t, out, "--yes")
}

func TestRun_InitInheritsAddr(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"init", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "--addr")
}

func TestRun_InitQuietFlag(t *testing.T) {
	cmd := newRootCmd("1.0.0")
	cmd.SetArgs([]string{"init", "--help"})
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "--quiet")
	assert.Contains(t, buf.String(), "-q")
}

func TestRunInit_WildcardHostWarning(t *testing.T) {
	is := &initServer{metricsEnabled: true, hasHTTPMetrics: true, wildcardHost: true, servers: map[string]any{"srv0": nil}}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader(""), f, srv.URL, true)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "All traffic grouped under")
	assert.Contains(t, buf.String(), "host matchers")
}

func TestRunInit_NoWildcardWarningWithRealHosts(t *testing.T) {
	is := &initServer{metricsEnabled: true, hasHTTPMetrics: true, servers: map[string]any{"srv0": nil}}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	var buf bytes.Buffer
	err := runInit(context.Background(), &buf, strings.NewReader(""), f, srv.URL, true)

	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "All traffic grouped under")
}

func TestHasWildcardHost(t *testing.T) {
	assert.False(t, hasWildcardHost(nil))
	assert.False(t, hasWildcardHost(map[string]*fetcher.HostMetrics{}))
	assert.True(t, hasWildcardHost(map[string]*fetcher.HostMetrics{"*": {Host: "*", RequestsTotal: 100}}))
	assert.True(t, hasWildcardHost(map[string]*fetcher.HostMetrics{"*": {RequestsTotal: 50}, "example.com": {}}))
	assert.False(t, hasWildcardHost(map[string]*fetcher.HostMetrics{"*": {RequestsTotal: 0}}))
	assert.False(t, hasWildcardHost(map[string]*fetcher.HostMetrics{"example.com": {RequestsTotal: 100}}))
}

func TestRunInit_Quiet(t *testing.T) {
	is := &initServer{metricsEnabled: true, hasFrankenPHP: false, hasHTTPMetrics: true}
	srv := newInitTestServer(is)
	defer srv.Close()

	f := fetcher.NewHTTPFetcher(srv.URL, 0)
	err := runInit(context.Background(), io.Discard, strings.NewReader(""), f, srv.URL, false)

	require.NoError(t, err)
}
