package fetcher

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestCAPEM(t *testing.T) ([]byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "Test Root CA"},
		Issuer:                pkix.Name{CommonName: "Test Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return pemBytes, cert
}

func generateTestLeafCertPEM(t *testing.T, dnsNames []string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return pemBytes, key
}

func TestFetchPKICertificates_OK(t *testing.T) {
	rootPEM, _ := generateTestCAPEM(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/pki/certificate_authorities":
			json.NewEncoder(w).Encode(map[string]json.RawMessage{
				"local": json.RawMessage(`{}`),
			})
		case "/pki/ca/local":
			json.NewEncoder(w).Encode(pkiCAInfo{
				ID:              "local",
				Name:            "Caddy Local Authority",
				RootCertificate: string(rootPEM),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	certs := f.FetchPKICertificates(context.Background())
	require.Len(t, certs, 1)
	assert.Equal(t, "Test Root CA", certs[0].Subject)
	assert.Equal(t, "pki", certs[0].Source)
	assert.Equal(t, "local", certs[0].Host)
	assert.True(t, certs[0].IsCA)
	assert.False(t, certs[0].AutoRenew)
}

func TestFetchPKICertificates_MultipleCAs(t *testing.T) {
	rootPEM, _ := generateTestCAPEM(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/pki/certificate_authorities":
			json.NewEncoder(w).Encode(map[string]json.RawMessage{
				"local":  json.RawMessage(`{}`),
				"custom": json.RawMessage(`{}`),
			})
		case "/pki/ca/local", "/pki/ca/custom":
			caID := r.URL.Path[len("/pki/ca/"):]
			json.NewEncoder(w).Encode(pkiCAInfo{
				ID:              caID,
				RootCertificate: string(rootPEM),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	certs := f.FetchPKICertificates(context.Background())
	assert.Len(t, certs, 2)
}

func TestFetchPKICertificates_NoPKI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	certs := f.FetchPKICertificates(context.Background())
	assert.Empty(t, certs)
}

func TestFetchPKICertificates_InvalidPEM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/pki/certificate_authorities":
			json.NewEncoder(w).Encode(map[string]json.RawMessage{
				"local": json.RawMessage(`{}`),
			})
		case "/pki/ca/local":
			json.NewEncoder(w).Encode(pkiCAInfo{
				ID:              "local",
				RootCertificate: "not valid PEM data",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	certs := f.FetchPKICertificates(context.Background())
	assert.Empty(t, certs)
}

func TestParsePEMCertificates_Valid(t *testing.T) {
	pemBytes, _ := generateTestCAPEM(t)
	certs, err := parsePEMCertificates(pemBytes)
	require.NoError(t, err)
	require.Len(t, certs, 1)
	assert.Equal(t, "Test Root CA", certs[0].Subject.CommonName)
	assert.True(t, certs[0].IsCA)
}

func TestParsePEMCertificates_Empty(t *testing.T) {
	certs, err := parsePEMCertificates(nil)
	require.NoError(t, err)
	assert.Nil(t, certs)
}

func TestParsePEMCertificates_NoPEMData(t *testing.T) {
	certs, err := parsePEMCertificates([]byte("this is not PEM"))
	require.NoError(t, err)
	assert.Nil(t, certs)
}

func TestParsePEMCertificates_InvalidDER(t *testing.T) {
	bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("bad DER")})
	_, err := parsePEMCertificates(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse certificate")
}

func TestParsePEMCertificates_MultipleCerts(t *testing.T) {
	pem1, _ := generateTestCAPEM(t)
	pem2, _ := generateTestCAPEM(t)
	combined := append(pem1, pem2...)

	certs, err := parsePEMCertificates(combined)
	require.NoError(t, err)
	assert.Len(t, certs, 2)
}

func TestDialTLSCertificates_OK(t *testing.T) {
	certPEM, key := generateTestLeafCertPEM(t, []string{"example.local"})
	cert, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: mustMarshalECKey(t, key),
	}))
	require.NoError(t, err)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// complete the TLS handshake before closing
			if tlsConn, ok := conn.(*tls.Conn); ok {
				_ = tlsConn.Handshake()
			}
			conn.Close()
		}
	}()

	_, port, _ := net.SplitHostPort(listener.Addr().String())

	// create a server that returns the listen port
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/http/servers":
			json.NewEncoder(w).Encode(map[string]json.RawMessage{
				"srv0": json.RawMessage(`{"listen":["` + listener.Addr().String() + `"]}`),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	certs := f.DialTLSCertificates(context.Background(), []string{"127.0.0.1"})
	require.Len(t, certs, 1)
	assert.Equal(t, "tls", certs[0].Source)
	assert.Equal(t, net.JoinHostPort("127.0.0.1", port), certs[0].Host)
	assert.Contains(t, certs[0].DNSNames, "example.local")
}

func TestDialTLSCertificates_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/http/servers":
			json.NewEncoder(w).Encode(map[string]json.RawMessage{
				"srv0": json.RawMessage(`{"listen":[":19999"]}`),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	certs := f.DialTLSCertificates(context.Background(), []string{"192.0.2.1"})
	assert.Empty(t, certs)
}

func TestDialTLSCertificates_SkipsWildcard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/apps/http/servers":
			json.NewEncoder(w).Encode(map[string]json.RawMessage{
				"srv0": json.RawMessage(`{"listen":[":443"]}`),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	f := NewHTTPFetcher(srv.URL, 0)
	certs := f.DialTLSCertificates(context.Background(), []string{"*", ""})
	assert.Empty(t, certs)
}

func TestExtractListenPorts(t *testing.T) {
	servers := map[string]json.RawMessage{
		"srv0": json.RawMessage(`{"listen":[":443",":8443"]}`),
		"srv1": json.RawMessage(`{"listen":[":80"]}`),
	}
	ports := extractListenPorts(servers)
	assert.Contains(t, ports, "443")
	assert.Contains(t, ports, "8443")
	assert.NotContains(t, ports, "80")
}

func TestExtractListenPorts_Empty(t *testing.T) {
	ports := extractListenPorts(nil)
	assert.Empty(t, ports)
}

func TestExtractListenPorts_SkipsUnixSockets(t *testing.T) {
	servers := map[string]json.RawMessage{
		"srv0": json.RawMessage(`{"listen":["unix//run/caddy.sock",":443"]}`),
	}
	ports := extractListenPorts(servers)
	assert.Equal(t, []string{"443"}, ports)
}

func TestIsNumericPort(t *testing.T) {
	assert.True(t, isNumericPort("443"))
	assert.True(t, isNumericPort("8080"))
	assert.False(t, isNumericPort(""))
	assert.False(t, isNumericPort("/run/caddy.sock"))
	assert.False(t, isNumericPort("abc"))
}

func TestIsAutoRenewedIssuer(t *testing.T) {
	assert.True(t, isAutoRenewedIssuer("Let's Encrypt Authority X3"))
	assert.True(t, isAutoRenewedIssuer("ZeroSSL ECC Domain"))
	assert.True(t, isAutoRenewedIssuer("Caddy Local Authority"))
	assert.False(t, isAutoRenewedIssuer("DigiCert Global Root"))
	assert.False(t, isAutoRenewedIssuer(""))
}

func mustMarshalECKey(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	b, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return b
}
