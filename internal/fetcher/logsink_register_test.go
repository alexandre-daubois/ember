package fetcher

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterRuntimeLogSink_PayloadUsesExclude(t *testing.T) {
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
	require.NoError(t, f.RegisterRuntimeLogSink(context.Background(), emberRuntimeLogSinkName, "127.0.0.1:9210"))

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

func TestUnregisterLogSink_RuntimeOK(t *testing.T) {
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
	require.NoError(t, f.UnregisterLogSink(context.Background(), emberRuntimeLogSinkName))

	require.NotNil(t, path.Load())
	assert.Contains(t, *path.Load(), emberRuntimeLogSinkName)
}

func TestCheckLogSink_RuntimeFound(t *testing.T) {
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
	assert.True(t, f.CheckLogSink(context.Background(), emberRuntimeLogSinkName))
}

func TestRegisterAccessLogSink_PutsExpectedPayload(t *testing.T) {
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
	require.NoError(t, f.RegisterAccessLogSink(context.Background(), EmberLogSinkName, "127.0.0.1:9210"))

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

func TestRegisterAccessLogSink_MaliciousAddressCannotInjectJSON(t *testing.T) {
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
	require.NoError(t, f.RegisterAccessLogSink(context.Background(), EmberLogSinkName, evil))

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

func TestRegisterAccessLogSink_BootstrapsLoggingPath(t *testing.T) {
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
	require.NoError(t, f.RegisterAccessLogSink(context.Background(), EmberLogSinkName, "127.0.0.1:9210"))
	assert.True(t, sinkRegistered.Load(), "sink must be registered after bootstrap")
	assert.True(t, loggingExists, "/config/logging should have been created")
	assert.True(t, logsExists, "/config/logging/logs should have been created")
}

// Caddy answers a PUT over an existing key with a 409 and lets a POST replace it.
func TestRegisterAccessLogSink_ReplacesAStaleSink(t *testing.T) {
	var stored atomic.Pointer[string]
	stale := `{"writer":{"output":"net","address":"tcp/127.0.0.1:1"}}`
	stored.Store(&stale)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/config/logging/logs/"+EmberLogSinkName, r.URL.Path)
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"key already exists"}`)
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			s := string(body)
			stored.Store(&s)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.NoError(t, f.RegisterAccessLogSink(context.Background(), EmberLogSinkName, "127.0.0.1:9210"))
	assert.Contains(t, *stored.Load(), `"address":"tcp/127.0.0.1:9210"`)
}

func TestRegisterAccessLogSink_FailedReplaceIsAnError(t *testing.T) {
	var loggingWrites atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path != "/config/logging/logs/"+EmberLogSinkName:
			loggingWrites.Add(1)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusConflict)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.RegisterAccessLogSink(context.Background(), EmberLogSinkName, "127.0.0.1:9210")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Zero(t, loggingWrites.Load(), "an existing sink is no reason to write into the shared logging config")
}

func TestRegisterAccessLogSink_ServerError(t *testing.T) {
	var loggingWrites atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/logging" || r.URL.Path == "/config/logging/logs" {
			loggingWrites.Add(1)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.RegisterAccessLogSink(context.Background(), EmberLogSinkName, "127.0.0.1:9210")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Zero(t, loggingWrites.Load(),
		"a server fault is no reason to write into the shared logging config")
}

func TestRegisterAccessLogSink_TransportErrorDoesNotBootstrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	require.Error(t, f.RegisterAccessLogSink(context.Background(), EmberLogSinkName, "127.0.0.1:9210"),
		"an unreachable admin API must not fall through to the bootstrap PUT")
}

func TestUnregisterLogSink_OK(t *testing.T) {
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
	require.NoError(t, f.UnregisterLogSink(context.Background(), EmberLogSinkName))

	require.NotNil(t, got.Load())
	assert.True(t, strings.HasSuffix(*got.Load(), "/"+EmberLogSinkName))
}

func TestUnregisterLogSink_NotFoundIsFine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.NoError(t, f.UnregisterLogSink(context.Background(), EmberLogSinkName))
}

func TestUnregisterLogSink_OtherErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.UnregisterLogSink(context.Background(), EmberLogSinkName)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestCheckLogSink_ExistsReturnsTrue(t *testing.T) {
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
	assert.True(t, f.CheckLogSink(context.Background(), EmberLogSinkName))
}

func TestCheckLogSink_NullReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "null")
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.False(t, f.CheckLogSink(context.Background(), EmberLogSinkName))
}

func TestCheckLogSink_404ReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.False(t, f.CheckLogSink(context.Background(), EmberLogSinkName))
}

func TestCheckLogSink_ServerErrorReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	assert.False(t, f.CheckLogSink(context.Background(), EmberLogSinkName))
}
