package fetcher

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTLSConfig_NothingSet(t *testing.T) {
	cfg, err := BuildTLSConfig(TLSOptions{})
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestBuildTLSConfig_InsecureOnly(t *testing.T) {
	cfg, err := BuildTLSConfig(TLSOptions{Insecure: true})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.InsecureSkipVerify)
}

func TestBuildTLSConfig_CACert(t *testing.T) {
	caFile := generateTestCA(t)

	cfg, err := BuildTLSConfig(TLSOptions{CACert: caFile})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
}

func TestBuildTLSConfig_CACertNotFound(t *testing.T) {
	_, err := BuildTLSConfig(TLSOptions{CACert: "/nonexistent/ca.pem"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read CA cert")
}

func TestBuildTLSConfig_CACertInvalid(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(f, []byte("not a cert"), 0o600))

	_, err := BuildTLSConfig(TLSOptions{CACert: f})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CA cert")
}

func TestBuildTLSConfig_ClientCert(t *testing.T) {
	certFile, keyFile := generateTestClientCert(t)

	cfg, err := BuildTLSConfig(TLSOptions{ClientCert: certFile, ClientKey: keyFile})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Certificates, 1)
}

func TestBuildTLSConfig_ClientCertBadPath(t *testing.T) {
	_, err := BuildTLSConfig(TLSOptions{ClientCert: "/bad/cert.pem", ClientKey: "/bad/key.pem"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load client cert")
}

func TestBuildTLSConfig_Full(t *testing.T) {
	caFile := generateTestCA(t)
	certFile, keyFile := generateTestClientCert(t)

	cfg, err := BuildTLSConfig(TLSOptions{
		CACert:     caFile,
		ClientCert: certFile,
		ClientKey:  keyFile,
		Insecure:   false,
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
	assert.Len(t, cfg.Certificates, 1)
	assert.False(t, cfg.InsecureSkipVerify)
}

func TestSetTLSConfig(t *testing.T) {
	f := NewHTTPFetcher("https://localhost:2019", 0)

	cfg, err := BuildTLSConfig(TLSOptions{Insecure: true})
	require.NoError(t, err)

	f.SetTLSConfig(cfg)

	tr := f.transport.inner.Load()
	require.NotNil(t, tr)
	require.NotNil(t, tr.TLSClientConfig)
	assert.True(t, tr.TLSClientConfig.InsecureSkipVerify)
}

// TestSetTLSConfig_ConcurrentWithFetch reproduces the SIGHUP-during-Fetch
// scenario that motivated the atomic transport swap. A pool of goroutines
// hammers the server while another goroutine swaps the TLS config in a loop.
// Run with -race; any unsynchronized access on the transport field will fail.
func TestSetTLSConfig_ConcurrentWithFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var (
		stop      atomic.Bool
		wg        sync.WaitGroup
		fetches   atomic.Int64
		swaps     atomic.Int64
		fetchErrs atomic.Int64
	)

	for range 8 {
		wg.Go(func() {
			for !stop.Load() {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/", nil)
				if err != nil {
					fetchErrs.Add(1)
					return
				}
				resp, err := f.httpClient.Do(req)
				if err != nil {
					fetchErrs.Add(1)
					continue
				}
				_ = resp.Body.Close()
				fetches.Add(1)
			}
		})
	}

	wg.Go(func() {
		insecureCfg, err := BuildTLSConfig(TLSOptions{Insecure: true})
		require.NoError(t, err)
		caCfg, err := BuildTLSConfig(TLSOptions{CACert: generateTestCA(t)})
		require.NoError(t, err)
		toggle := false
		for !stop.Load() {
			if toggle {
				f.SetTLSConfig(insecureCfg)
			} else {
				f.SetTLSConfig(caCfg)
			}
			toggle = !toggle
			swaps.Add(1)
		}
	})

	time.Sleep(500 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	assert.Greater(t, fetches.Load(), int64(0), "should have completed at least one fetch")
	assert.Greater(t, swaps.Load(), int64(0), "should have completed at least one swap")
	// Some fetches may legitimately fail when the underlying connection is
	// closed mid-flight by CloseIdleConnections; that is the documented
	// behavior, not a race. We just want no panics and no -race report.
	t.Logf("fetches=%d swaps=%d errs=%d", fetches.Load(), swaps.Load(), fetchErrs.Load())
}

func generateTestCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test CA"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	f := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(f, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}), 0o600))
	return f
}

func generateTestClientCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Test Client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath = filepath.Join(dir, "client.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}), 0o600))

	keyBytes, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPath = filepath.Join(dir, "client-key.pem")
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}), 0o600))

	return certPath, keyPath
}
