package fetcher

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeServerLogsState models the state of /config/apps/http/servers/<srv>/logs
// for one server, with thread-safe access for tests.
type fakeServerLogsState struct {
	mu      sync.Mutex
	current string // raw JSON or "" when unset
	posts   int
	deletes int
}

func newFakeServerLogsHandler(t *testing.T, srv string, state *fakeServerLogsState) http.Handler {
	t.Helper()
	prefix := "/config/apps/http/servers/" + srv + "/logs"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix {
			http.NotFound(w, r)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			if state.current == "" {
				_, _ = io.WriteString(w, "null")
			} else {
				_, _ = io.WriteString(w, state.current)
			}
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			state.current = strings.TrimSpace(string(body))
			state.posts++
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			state.current = ""
			state.deletes++
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func TestEnableServerAccessLogs_NotConfigured_PostsEmpty(t *testing.T) {
	state := &fakeServerLogsState{}
	srv := httptest.NewServer(newFakeServerLogsHandler(t, "srv0", state))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.EnableServerAccessLogs(context.Background(), "srv0")
	require.NoError(t, err)
	assert.True(t, enabled, "should report enabled when we install the empty block")
	assert.Equal(t, 1, state.posts)
	assert.Equal(t, "{}", state.current)
}

func TestEnableServerAccessLogs_AlreadyConfigured_LeavesAlone(t *testing.T) {
	state := &fakeServerLogsState{current: `{"default_logger_name":"user-logger"}`}
	srv := httptest.NewServer(newFakeServerLogsHandler(t, "srv0", state))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.EnableServerAccessLogs(context.Background(), "srv0")
	require.NoError(t, err)
	assert.False(t, enabled, "must not overwrite a user-defined logs block")
	assert.Equal(t, 0, state.posts)
}

func TestRestoreServerAccessLogs_DeletesEmptyBlockWeInstalled(t *testing.T) {
	state := &fakeServerLogsState{current: "{}"}
	srv := httptest.NewServer(newFakeServerLogsHandler(t, "srv0", state))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.RestoreServerAccessLogs(context.Background(), "srv0"))
	assert.Equal(t, 1, state.deletes)
}

func TestRestoreServerAccessLogs_LeavesUserChangesAlone(t *testing.T) {
	// Simulate: we POSTed {} earlier; user (or another tool) changed it
	// before our cleanup ran. We must not delete what we did not install.
	state := &fakeServerLogsState{current: `{"default_logger_name":"user-logger"}`}
	srv := httptest.NewServer(newFakeServerLogsHandler(t, "srv0", state))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.RestoreServerAccessLogs(context.Background(), "srv0"))
	assert.Equal(t, 0, state.deletes)
	assert.Equal(t, `{"default_logger_name":"user-logger"}`, state.current)
}

func TestRestoreServerAccessLogs_NotFoundIsFine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "{}")
		case http.MethodDelete:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.NoError(t, f.RestoreServerAccessLogs(context.Background(), "srv0"))
}

func TestEnableServerAccessLogs_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "null")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.EnableServerAccessLogs(context.Background(), "srv0")
	require.Error(t, err)
	assert.False(t, enabled)
}

func TestEnableServerAccessLogs_GetNon200_PropagatesError(t *testing.T) {
	// A non-200 GET must surface as an error instead of silently falling
	// through to the POST, so transient admin API failures do not flip state.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		t.Fatalf("unexpected %s after failed GET", r.Method)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.EnableServerAccessLogs(context.Background(), "srv0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.False(t, enabled)
}

func TestRestoreServerAccessLogs_CanonicalizedEmptyObjectStillRestores(t *testing.T) {
	// Older or newer Caddy versions may serialize {} with whitespace or
	// trailing newlines; the JSON-equivalent comparison must handle them.
	for _, variant := range []string{"{}", " {}\n", "{\n}", "{ }"} {
		t.Run(variant, func(t *testing.T) {
			state := &fakeServerLogsState{current: variant}
			srv := httptest.NewServer(newFakeServerLogsHandler(t, "srv0", state))
			defer srv.Close()

			f := NewHTTPFetcher(srv.URL, 0)
			require.NoError(t, f.RestoreServerAccessLogs(context.Background(), "srv0"))
			assert.Equal(t, 1, state.deletes, "variant=%q should have been recognized as empty", variant)
		})
	}
}

func TestRestoreServerAccessLogs_NullOrEmptyResponseSkipsDelete(t *testing.T) {
	// A null or empty response means the logs config is already gone -- the
	// restore is a no-op, we must not send a DELETE.
	for _, variant := range []string{"null", "", "   "} {
		t.Run(variant, func(t *testing.T) {
			state := &fakeServerLogsState{current: variant}
			srv := httptest.NewServer(newFakeServerLogsHandler(t, "srv0", state))
			defer srv.Close()

			f := NewHTTPFetcher(srv.URL, 0)
			require.NoError(t, f.RestoreServerAccessLogs(context.Background(), "srv0"))
			assert.Equal(t, 0, state.deletes)
		})
	}
}

func TestEnableServerAccessLogs_EscapesServerName(t *testing.T) {
	// Caddy lets users pick any string as a server name (it's a JSON object
	// key). We must URL-escape it so names with slashes, spaces or unicode
	// do not break path routing on the admin API.
	var capturedPath atomic.Pointer[string]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		capturedPath.Store(&p)
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "null")
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.EnableServerAccessLogs(context.Background(), "my/weird name")
	require.NoError(t, err)
	assert.True(t, enabled)

	p := capturedPath.Load()
	require.NotNil(t, p)
	// Go's net/http auto-decodes r.URL.Path, so we get the decoded form
	// back -- which proves the client sent the escaped form on the wire.
	assert.Equal(t, "/config/apps/http/servers/my/weird name/logs", *p)
}

func TestEnableServerAccessLogs_CanonicalizedEmptyObjectTreatedAsUnset(t *testing.T) {
	// If GET returns an empty object variant, we should still proceed with
	// the POST (the config is effectively empty even if not "null").
	for _, variant := range []string{"{}", "{\n}", " { } "} {
		t.Run(variant, func(t *testing.T) {
			state := &fakeServerLogsState{current: variant}
			srv := httptest.NewServer(newFakeServerLogsHandler(t, "srv0", state))
			defer srv.Close()

			f := NewHTTPFetcher(srv.URL, 0)
			enabled, err := f.EnableServerAccessLogs(context.Background(), "srv0")
			require.NoError(t, err)
			assert.True(t, enabled, "variant=%q should be treated as empty", variant)
		})
	}
}
