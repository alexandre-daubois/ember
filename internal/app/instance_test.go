package app

import (
	"context"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestParseAddrs_TLSSuffix_All(t *testing.T) {
	addrs, err := parseAddrs([]string{
		"web1=https://a,ca=/p/ca1.pem,cert=/p/c.pem,key=/p/k.pem",
		"web2=https://b,insecure",
	})
	require.NoError(t, err)
	require.Len(t, addrs, 2)

	assert.Equal(t, "/p/ca1.pem", addrs[0].tls.caCert)
	assert.Equal(t, "/p/c.pem", addrs[0].tls.clientCert)
	assert.Equal(t, "/p/k.pem", addrs[0].tls.clientKey)
	assert.False(t, addrs[0].tls.insecureSet)

	assert.True(t, addrs[1].tls.insecureSet)
	assert.True(t, addrs[1].tls.insecure)
}

func TestParseAddrs_TLSSuffix_InsecureExplicitFalse(t *testing.T) {
	addrs, err := parseAddrs([]string{"web1=https://a,insecure=false"})
	require.NoError(t, err)
	assert.True(t, addrs[0].tls.insecureSet)
	assert.False(t, addrs[0].tls.insecure)
}

func TestParseAddrs_TLSSuffix_InsecureBadValue(t *testing.T) {
	_, err := parseAddrs([]string{"web1=https://a,insecure=maybe"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "true|false")
}

func TestParseAddrs_TLSSuffix_UnknownKey(t *testing.T) {
	_, err := parseAddrs([]string{"web1=https://a,foo=bar"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown suffix")
}

func TestParseAddrs_TLSSuffix_EmptyPath(t *testing.T) {
	_, err := parseAddrs([]string{"web1=https://a,ca="})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty path")
}

func TestParseAddrs_TLSSuffix_CertWithoutKey(t *testing.T) {
	_, err := parseAddrs([]string{"web1=https://a,cert=/p/c.pem"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cert= and key= must be set together")
}

func TestParseAddrs_TLSSuffix_OnUnixSocketRejected(t *testing.T) {
	_, err := parseAddrs([]string{"sock=unix//run/caddy.sock,ca=/p/ca.pem"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS options cannot be used with Unix socket addresses")
}

func TestParseAddrs_TLSSuffix_InsecureOnUnixRejected(t *testing.T) {
	_, err := parseAddrs([]string{"sock=unix//run/caddy.sock,insecure"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS options cannot be used with Unix socket addresses")
}

func TestParseAddrs_TLSSuffix_AliasOptional(t *testing.T) {
	addrs, err := parseAddrs([]string{"https://web1.fr,ca=/p/ca.pem"})
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	assert.Equal(t, "web1_fr", addrs[0].name)
	assert.Equal(t, "https://web1.fr", addrs[0].url)
	assert.Equal(t, "/p/ca.pem", addrs[0].tls.caCert)
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

// TestNewInstances_PerInstanceCACert proves the acceptance criterion: two
// HTTPS instances signed by distinct CAs are scraped successfully when each
// --addr carries its own ca= suffix and no global --ca-cert is set.
func TestNewInstances_PerInstanceCACert(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			_, _ = w.Write([]byte("# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total{host=\"x\",code=\"200\"} 1\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	srv1 := httptest.NewTLSServer(handler)
	defer srv1.Close()
	srv2 := httptest.NewTLSServer(handler)
	defer srv2.Close()

	dir := t.TempDir()
	ca1 := filepath.Join(dir, "ca1.pem")
	ca2 := filepath.Join(dir, "ca2.pem")
	require.NoError(t, os.WriteFile(ca1, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv1.Certificate().Raw}), 0o600))
	require.NoError(t, os.WriteFile(ca2, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv2.Certificate().Raw}), 0o600))

	cfg := &config{
		addrs: []addrSpec{
			{name: "web1", url: srv1.URL, tls: addrTLS{caCert: ca1}},
			{name: "web2", url: srv2.URL, tls: addrTLS{caCert: ca2}},
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	instances, err := newInstances(context.Background(), cfg, "test")
	require.NoError(t, err)
	require.Len(t, instances, 2)

	for _, inst := range instances {
		snap, err := inst.fetcher.Fetch(context.Background())
		require.NoError(t, err, "instance %s should fetch via its own ca= suffix", inst.name)
		require.NotNil(t, snap)
		assert.Empty(t, snap.Errors, "per-instance CA handshake must succeed for %s", inst.name)
	}

	// Per-instance TLS options must be the ones stored on the instance so SIGHUP
	// reload re-reads the right files.
	assert.Equal(t, ca1, instances[0].tls.CACert)
	assert.Equal(t, ca2, instances[1].tls.CACert)
}

func TestEffectiveTLS_PerInstanceOverridesGlobal(t *testing.T) {
	cfg := &config{caCert: "/global/ca.pem", insecure: true}
	spec := addrSpec{tls: addrTLS{caCert: "/inst/ca.pem", insecureSet: true, insecure: false}}

	opts := effectiveTLS(spec, cfg)

	assert.Equal(t, "/inst/ca.pem", opts.CACert)
	assert.False(t, opts.Insecure)
}

func TestEffectiveTLS_FallbackToGlobalPerField(t *testing.T) {
	cfg := &config{caCert: "/global/ca.pem", clientCert: "/global/c.pem", clientKey: "/global/k.pem", insecure: true}
	spec := addrSpec{tls: addrTLS{clientCert: "/inst/c.pem", clientKey: "/inst/k.pem"}}

	opts := effectiveTLS(spec, cfg)

	assert.Equal(t, "/global/ca.pem", opts.CACert)
	assert.Equal(t, "/inst/c.pem", opts.ClientCert)
	assert.Equal(t, "/inst/k.pem", opts.ClientKey)
	assert.True(t, opts.Insecure, "insecureSet=false leaves the global value intact")
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

func TestResolveLocalListenerPID_LocalAddrPicksUpListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	port := ln.Addr().(*net.TCPAddr).Port
	cfg := &config{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	spec := addrSpec{name: "web1", url: "http://127.0.0.1:" + strconv.Itoa(port)}

	pid := resolveLocalListenerPID(context.Background(), cfg, spec)
	if pid == 0 {
		t.Skipf("connections lookup unavailable in this env")
	}
	assert.Equal(t, int32(os.Getpid()), pid)
}

func TestResolveLocalListenerPID_RemoteAddrSilentZero(t *testing.T) {
	cfg := &config{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	spec := addrSpec{name: "prod", url: "http://prod.example.com:2019"}
	assert.Equal(t, int32(0), resolveLocalListenerPID(context.Background(), cfg, spec))
}

func TestResolveLocalListenerPID_NoListenerSilentZero(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	cfg := &config{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	spec := addrSpec{name: "vacant", url: "http://127.0.0.1:" + strconv.Itoa(port)}
	assert.Equal(t, int32(0), resolveLocalListenerPID(context.Background(), cfg, spec))
}
