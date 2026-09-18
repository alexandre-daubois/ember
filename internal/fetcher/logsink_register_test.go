package fetcher

import (
	"context"
	"encoding/json"
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

func TestRegisterEmberRuntimeLogSink_PayloadUsesExclude(t *testing.T) {
	var captured atomic.Pointer[string]

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/logging/logs/"+emberRuntimeLogSinkName {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.RegisterEmberRuntimeLogSink(context.Background(), "127.0.0.1:9210"))

	got := captured.Load()
	require.NotNil(t, got)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(*got), &parsed))

	// The runtime sink must exclude access logs so it doesn't duplicate
	// what the __ember__ sink already streams.
	exclude, ok := parsed["exclude"].([]any)
	require.True(t, ok, "runtime sink payload must have exclude")
	require.Len(t, exclude, 1)
	assert.Equal(t, "http.log.access", exclude[0])
	_, hasInclude := parsed["include"]
	assert.False(t, hasInclude, "runtime sink must not constrain with include")
}

func TestUnregisterEmberRuntimeLogSink_OK(t *testing.T) {
	var path atomic.Pointer[string]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p := r.URL.Path
		path.Store(&p)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.UnregisterEmberRuntimeLogSink(context.Background()))

	require.NotNil(t, path.Load())
	assert.Contains(t, *path.Load(), emberRuntimeLogSinkName)
}

func TestCheckEmberRuntimeLogSink_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, emberRuntimeLogSinkName) {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"writer":{}}`)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.True(t, f.CheckEmberRuntimeLogSink(context.Background()))
}

func TestRegisterEmberLogSink_PutsExpectedPayload(t *testing.T) {
	var captured atomic.Pointer[string]

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/logging/logs/"+EmberLogSinkName {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.RegisterEmberLogSink(context.Background(), "127.0.0.1:9210"))

	got := captured.Load()
	require.NotNil(t, got)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(*got), &parsed))

	writer, ok := parsed["writer"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "net", writer["output"])
	assert.Equal(t, "tcp/127.0.0.1:9210", writer["address"])
	assert.Equal(t, true, writer["soft_start"])

	encoder, ok := parsed["encoder"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "json", encoder["format"])

	include, ok := parsed["include"].([]any)
	require.True(t, ok)
	require.Len(t, include, 1)
	assert.Equal(t, "http.log.access", include[0])
}

func TestRegisterEmberLogSink_MaliciousAddressCannotInjectJSON(t *testing.T) {
	// Prove the payload is built via json.Marshal, not string interpolation:
	// an address containing JSON metacharacters must end up escaped inside
	// the "address" string, not breaking out into sibling fields.
	var captured atomic.Pointer[string]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		captured.Store(&s)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	evil := `127.0.0.1:1","output":"evil","x":"`
	require.NoError(t, f.RegisterEmberLogSink(context.Background(), evil))

	got := captured.Load()
	require.NotNil(t, got)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(*got), &parsed))

	writer, ok := parsed["writer"].(map[string]any)
	require.True(t, ok)
	// The full evil string must land inside address, with writer.output
	// still equal to "net" (not "evil").
	assert.Equal(t, "net", writer["output"])
	assert.Equal(t, "tcp/"+evil, writer["address"])
	// And no sibling injected keys at the top level.
	_, hasX := parsed["x"]
	assert.False(t, hasX, "must not allow injecting top-level keys")
}

func TestRegisterEmberLogSink_BootstrapsLoggingPath(t *testing.T) {
	// Simulates a Caddy config with no logging section: the first PUT to
	// /config/logging/logs/__ember__ fails, then the bootstrap creates the
	// parent path, and the retry succeeds.
	loggingExists := false
	logsExists := false
	var sinkRegistered atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/config/logging/logs/"+EmberLogSinkName && r.Method == http.MethodPut:
			if !logsExists {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid traversal path"}`)
				return
			}
			sinkRegistered.Store(true)
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/config/logging/logs" && r.Method == http.MethodPut:
			if !loggingExists {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"invalid traversal path"}`)
				return
			}
			logsExists = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/config/logging" && r.Method == http.MethodPut:
			loggingExists = true
			logsExists = true
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.RegisterEmberLogSink(context.Background(), "127.0.0.1:9210"))
	assert.True(t, sinkRegistered.Load(), "sink must be registered after bootstrap")
	assert.True(t, loggingExists, "/config/logging should have been created")
	assert.True(t, logsExists, "/config/logging/logs should have been created")
}

func TestRegisterEmberLogSink_ServerError(t *testing.T) {
	var loggingWrites atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/logging" || r.URL.Path == "/config/logging/logs" {
			loggingWrites.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.RegisterEmberLogSink(context.Background(), "127.0.0.1:9210")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Zero(t, loggingWrites.Load(),
		"a server fault is no reason to write into the shared logging config")
}

func TestRegisterEmberLogSink_TransportErrorDoesNotBootstrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.Error(t, f.RegisterEmberLogSink(context.Background(), "127.0.0.1:9210"),
		"an unreachable admin API must not fall through to the bootstrap PUT")
}

func TestUnregisterEmberLogSink_OK(t *testing.T) {
	var got atomic.Pointer[string]

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		got.Store(&path)
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.UnregisterEmberLogSink(context.Background()))

	require.NotNil(t, got.Load())
	assert.True(t, strings.HasSuffix(*got.Load(), "/"+EmberLogSinkName))
}

func TestUnregisterEmberLogSink_NotFoundIsFine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.NoError(t, f.UnregisterEmberLogSink(context.Background()))
}

func TestUnregisterEmberLogSink_OtherErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.UnregisterEmberLogSink(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestCheckEmberLogSink_ExistsReturnsTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/logging/logs/"+EmberLogSinkName || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"writer":{"output":"net"}}`)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.True(t, f.CheckEmberLogSink(context.Background()))
}

func TestCheckEmberLogSink_NullReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "null")
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.False(t, f.CheckEmberLogSink(context.Background()))
}

func TestCheckEmberLogSink_404ReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.False(t, f.CheckEmberLogSink(context.Background()))
}

func TestCheckEmberLogSink_ServerErrorReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.False(t, f.CheckEmberLogSink(context.Background()))
}

func TestRegisterEmberMercureLogSink_NarrowsRuntimeAndAddsDebugSink(t *testing.T) {
	bodies := make(map[string]string)
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// PUT answers 409 on an existing key in Caddy; the runtime sink is
		// always there, so both writes must be set-or-replace POSTs.
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusConflict)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies[r.URL.Path] = string(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.RegisterEmberMercureLogSink(context.Background(), "127.0.0.1:9210"))

	var runtime, mercure map[string]any
	require.NoError(t, json.Unmarshal([]byte(bodies["/config/logging/logs/"+emberRuntimeLogSinkName]), &runtime))
	require.NoError(t, json.Unmarshal([]byte(bodies["/config/logging/logs/"+emberMercureLogSinkName]), &mercure))

	assert.Equal(t, []any{"http.log.access", MercureLoggerName}, runtime["exclude"], "the hub's INFO lines must come from one sink only")
	assert.Equal(t, []any{MercureLoggerName}, mercure["include"])
	assert.Equal(t, "DEBUG", mercure["level"])
	_, hasExclude := mercure["exclude"]
	assert.False(t, hasExclude)
	writer := mercure["writer"].(map[string]any)
	assert.Equal(t, "tcp/127.0.0.1:9210", writer["address"])
}

func TestUnregisterEmberMercureLogSink_RestoresRuntimeSink(t *testing.T) {
	var deleted []string
	var puts []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			puts = append(puts, r.URL.Path+" "+string(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.UnregisterEmberMercureLogSink(context.Background(), "127.0.0.1:9210"))

	require.Len(t, deleted, 1)
	assert.Contains(t, deleted[0], emberMercureLogSinkName)
	require.Len(t, puts, 1)
	assert.Contains(t, puts[0], emberRuntimeLogSinkName)
	assert.Contains(t, puts[0], `"exclude":["http.log.access"]`)
	assert.NotContains(t, puts[0], MercureLoggerName)
}

func TestUnregisterEmberMercureLogSink_NoRestoreWithoutAddr(t *testing.T) {
	var puts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			puts.Add(1)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.UnregisterEmberMercureLogSink(context.Background(), ""), "a 404 on the delete is not a failure")
	assert.Equal(t, int32(0), puts.Load())
}
