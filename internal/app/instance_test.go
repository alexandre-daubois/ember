package app

import (
	"context"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAddrs_SingleHost_NoNameValidation(t *testing.T) {
	addrs, err := parseAddrs([]string{"http://127.0.0.1:2019"})
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	assert.Equal(t, "http://127.0.0.1:2019", addrs[0].url)
	// derived name starts with a digit; tolerated when N=1 since no label is emitted
	assert.Equal(t, "127_0_0_1_2019", addrs[0].name)
}

func TestParseAddrs_AliasValid(t *testing.T) {
	addrs, err := parseAddrs([]string{"web1=https://web1.fr", "web2=https://web2.fr"})
	require.NoError(t, err)
	require.Len(t, addrs, 2)
	assert.Equal(t, "web1", addrs[0].name)
	assert.Equal(t, "https://web1.fr", addrs[0].url)
	assert.Equal(t, "web2", addrs[1].name)
}

func TestParseAddrs_AliasWithUnderscoresAndDigits(t *testing.T) {
	addrs, err := parseAddrs([]string{"web_1=https://a", "web2_x=https://b"})
	require.NoError(t, err)
	assert.Equal(t, "web_1", addrs[0].name)
	assert.Equal(t, "web2_x", addrs[1].name)
}

func TestParseAddrs_AliasWithDashErrorMentionsAlias(t *testing.T) {
	_, err := parseAddrs([]string{"web-1=https://a", "web2=https://b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alias name")
	assert.Contains(t, err.Error(), "no hyphens")
}

func TestParseAddrs_AliasInvalidStartsWithDigit(t *testing.T) {
	_, err := parseAddrs([]string{"1web=https://a", "ok=https://b"})
	require.Error(t, err)
	// 1web doesn't match alias prefix (must start with letter), so it's parsed as URL
	assert.Contains(t, err.Error(), "must start with http://, https://, or unix//")
}

func TestParseAddrs_AliasInvalidChars(t *testing.T) {
	// alias regex requires [a-zA-Z][a-zA-Z0-9_-]*= which excludes "%", so the value
	// is parsed as a URL and rejected by the scheme check.
	_, err := parseAddrs([]string{"web%=https://a", "ok=https://b"})
	require.Error(t, err)
}

func TestParseAddrs_SlugAutoFromHost(t *testing.T) {
	addrs, err := parseAddrs([]string{"https://web1.fr", "https://web2.fr"})
	require.NoError(t, err)
	assert.Equal(t, "web1_fr", addrs[0].name)
	assert.Equal(t, "web2_fr", addrs[1].name)
}

func TestParseAddrs_SlugUnixSocket(t *testing.T) {
	addrs, err := parseAddrs([]string{"unix//run/caddy/admin.sock"})
	require.NoError(t, err)
	assert.Equal(t, "admin_sock", addrs[0].name)
}

func TestParseAddrs_Collision_RequiresAlias(t *testing.T) {
	_, err := parseAddrs([]string{"https://web1.fr", "https://web1.fr/api"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate instance name")
}

func TestParseAddrs_Empty(t *testing.T) {
	_, err := parseAddrs([]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--addr is required")
}

func TestParseAddrs_EmptyValue(t *testing.T) {
	_, err := parseAddrs([]string{""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestParseAddrs_MixedSchemes(t *testing.T) {
	addrs, err := parseAddrs([]string{
		"web1=https://web1.fr",
		"local=http://localhost:2019",
		"sock=unix//run/caddy/admin.sock",
	})
	require.NoError(t, err)
	require.Len(t, addrs, 3)
	assert.Equal(t, "web1", addrs[0].name)
	assert.Equal(t, "local", addrs[1].name)
	assert.Equal(t, "sock", addrs[2].name)
}

func TestParseAddrs_MultiHostStartingWithDigitRequiresAlias(t *testing.T) {
	_, err := parseAddrs([]string{"http://127.0.0.1:2019", "http://10.0.0.1:2019"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use alias=url to set explicitly")
}

func TestParseAddrs_InvalidScheme(t *testing.T) {
	_, err := parseAddrs([]string{"ftp://nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must start with http://, https://, or unix//")
}

func TestParseAddrs_UnixEmptyPath(t *testing.T) {
	_, err := parseAddrs([]string{"unix//"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty Unix socket path")
}

func TestNewInstances_SharedCACert_TwoHTTPSInstances(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			_, _ = w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total{host=\"x\",code=\"200\"} 1\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv1 := httptest.NewTLSServer(handler)
	defer srv1.Close()
	srv2 := httptest.NewTLSServer(handler)
	defer srv2.Close()

	bundle := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv1.Certificate().Raw}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv2.Certificate().Raw})...,
	)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, bundle, 0o600))

	cfg := &config{
		addrs: []addrSpec{
			{name: "web1", url: srv1.URL},
			{name: "web2", url: srv2.URL},
		},
		caCert: caFile,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	instances, err := newInstances(context.Background(), cfg, "test")
	require.NoError(t, err)
	require.Len(t, instances, 2)

	for _, inst := range instances {
		snap, err := inst.fetcher.Fetch(context.Background())
		require.NoError(t, err, "instance %s should fetch via shared --ca-cert", inst.name)
		require.NotNil(t, snap)
		assert.Empty(t, snap.Errors, "TLS handshake against shared CA must succeed for %s", inst.name)
	}
}

func TestSlugifyHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"web1.fr", "web1_fr"},
		{"WEB1.FR", "web1_fr"},
		{"caddy-prod.local:2019", "caddy_prod_local_2019"},
		{"127.0.0.1", "127_0_0_1"},
		{"admin.sock", "admin_sock"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, slugifyHost(tt.in), "slugifyHost(%q)", tt.in)
	}
}
