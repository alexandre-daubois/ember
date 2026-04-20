package fetcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckAdminAPI_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/" {
			w.WriteHeader(200)
			w.Write([]byte("{}"))
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.CheckAdminAPI(context.Background())
	require.NoError(t, err)
}

func TestCheckAdminAPI_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.CheckAdminAPI(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unreachable")
}

func TestCheckMetricsEnabled_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/http/metrics" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.CheckMetricsEnabled(context.Background())
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestCheckMetricsEnabled_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/http/metrics" {
			w.WriteHeader(200)
			w.Write([]byte("null"))
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.CheckMetricsEnabled(context.Background())
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestCheckMetricsEnabled_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.CheckMetricsEnabled(context.Background())
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestEnableMetrics_OK(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/http/metrics" && r.Method == http.MethodPost {
			called = true
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.EnableMetrics(context.Background())
	require.NoError(t, err)
	assert.True(t, called)
}

func TestEnableMetrics_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	err := f.EnableMetrics(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestFetchFrankenPHPConfig_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/frankenphp" {
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(FrankenPHPConfig{
				NumThreads: 16,
				Workers: []FrankenPHPWorkerConfig{
					{FileName: "/app/worker.php", Name: "main", Num: 8},
				},
			})
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	cfg, err := f.FetchFrankenPHPConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, 16, cfg.NumThreads)
	require.Len(t, cfg.Workers, 1)
	assert.Equal(t, "/app/worker.php", cfg.Workers[0].FileName)
	assert.Equal(t, 8, cfg.Workers[0].Num)
}

func TestFetchFrankenPHPConfig_NotAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	cfg, err := f.FetchFrankenPHPConfig(context.Background())
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestFetchFrankenPHPConfig_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/config/apps/frankenphp" {
			w.WriteHeader(200)
			w.Write([]byte("not json"))
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	cfg, err := f.FetchFrankenPHPConfig(context.Background())
	require.Error(t, err, "garbled response must surface as an error rather than nil cfg")
	assert.Nil(t, cfg)
}

func TestCheckMetricsEnabled_InvalidJSONTreatedAsDisabled(t *testing.T) {
	// Caddy can return a 200 with non-JSON when admin is mis-routed; the
	// safest fallback is "metrics off" so the operator is prompted to fix it
	// rather than silently believing metrics are wired up.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	enabled, err := f.CheckMetricsEnabled(context.Background())
	require.NoError(t, err)
	assert.False(t, enabled)
}
