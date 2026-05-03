package fetcher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type pkiCAInfo struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	RootCertificate         string `json:"root_certificate"`
	IntermediateCertificate string `json:"intermediate_certificate"`
}

// FetchPKICertificates retrieves internal CA certificates from the Caddy PKI admin API.
// It is best-effort: failures to discover CAs or fetch individual certificates are
// silently skipped and partial results are returned.
func (f *HTTPFetcher) FetchPKICertificates(ctx context.Context) []CertificateInfo {
	caIDs, err := f.discoverCAIDs(ctx)
	if err != nil || len(caIDs) == 0 {
		caIDs = []string{"local"}
	}

	var certs []CertificateInfo
	for _, id := range caIDs {
		info, err := f.fetchCAInfo(ctx, id)
		if err != nil {
			continue
		}
		certs = append(certs, certInfosFromCA(info)...)
	}
	return certs
}

func (f *HTTPFetcher) discoverCAIDs(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := newGetRequest(ctx, f.baseURL+"/config/apps/pki/certificate_authorities")
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discover CAs: HTTP %d", resp.StatusCode)
	}

	var cas map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&cas); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cas))
	for id := range cas {
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *HTTPFetcher) fetchCAInfo(ctx context.Context, caID string) (*pkiCAInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := newGetRequest(ctx, f.baseURL+"/pki/ca/"+caID)
	if err != nil {
		return nil, err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch CA %s: HTTP %d", caID, resp.StatusCode)
	}

	var info pkiCAInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func certInfosFromCA(info *pkiCAInfo) []CertificateInfo {
	var certs []CertificateInfo
	for _, pemStr := range []string{info.RootCertificate, info.IntermediateCertificate} {
		if pemStr == "" {
			continue
		}
		parsed, err := parsePEMCertificates([]byte(pemStr))
		if err != nil || len(parsed) == 0 {
			continue
		}
		for _, c := range parsed {
			certs = append(certs, CertificateInfo{
				Subject:   c.Subject.CommonName,
				Issuer:    c.Issuer.CommonName,
				DNSNames:  c.DNSNames,
				NotBefore: c.NotBefore,
				NotAfter:  c.NotAfter,
				Serial:    c.SerialNumber.String(),
				IsCA:      c.IsCA,
				Source:    "pki",
				Host:      info.ID,
			})
		}
	}
	return certs
}

func parsePEMCertificates(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		certs = append(certs, c)
	}
	return certs, nil
}

// DialTLSCertificates connects to the given hosts via TLS and returns
// the leaf certificate information for each reachable host.
func (f *HTTPFetcher) DialTLSCertificates(ctx context.Context, hosts []string) []CertificateInfo {
	addrs := f.resolveTLSAddresses(ctx, hosts)
	if len(addrs) == 0 {
		return nil
	}

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)

	var (
		mu      sync.Mutex
		results []CertificateInfo
	)

	g, gCtx := errgroup.WithContext(ctx)
	for _, a := range addrs {
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-gCtx.Done():
				return nil
			}

			certs := dialAndInspect(gCtx, a)
			if len(certs) > 0 {
				mu.Lock()
				results = append(results, certs...)
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()
	return results
}

type tlsTarget struct {
	addr     string
	hostname string
}

func (f *HTTPFetcher) resolveTLSAddresses(ctx context.Context, hosts []string) []tlsTarget {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := newGetRequest(ctx, f.baseURL+"/config/apps/http/servers")
	if err != nil {
		return nil
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var servers map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&servers); err != nil {
		return nil
	}

	listenPorts := extractListenPorts(servers)
	if len(listenPorts) == 0 {
		listenPorts = []string{"443"}
	}

	var targets []tlsTarget
	seen := make(map[string]struct{})
	for _, host := range hosts {
		if host == "*" || host == "" {
			continue
		}
		for _, port := range listenPorts {
			addr := net.JoinHostPort(host, port)
			if _, ok := seen[addr]; ok {
				continue
			}
			seen[addr] = struct{}{}
			targets = append(targets, tlsTarget{addr: addr, hostname: host})
		}
	}
	return targets
}

func extractListenPorts(servers map[string]json.RawMessage) []string {
	type serverDef struct {
		Listen []string `json:"listen"`
	}
	seen := make(map[string]struct{})
	var ports []string
	for _, raw := range servers {
		var s serverDef
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		for _, listen := range s.Listen {
			_, port, err := net.SplitHostPort(listen)
			if err != nil {
				port = strings.TrimPrefix(listen, ":")
			}
			if port == "" || port == "80" || !isNumericPort(port) {
				continue
			}
			if _, ok := seen[port]; ok {
				continue
			}
			seen[port] = struct{}{}
			ports = append(ports, port)
		}
	}
	return ports
}

const tlsDialTimeout = 3 * time.Second

func dialAndInspect(ctx context.Context, target tlsTarget) []CertificateInfo {
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: tlsDialTimeout},
		Config: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // intentional: we inspect the cert, not trust it
			ServerName:         target.hostname,
		},
	}
	rawConn, err := d.DialContext(ctx, "tcp", target.addr)
	if err != nil {
		return nil
	}
	defer func() { _ = rawConn.Close() }()

	tlsConn, ok := rawConn.(*tls.Conn)
	if !ok {
		return nil
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil
	}

	// only inspect the leaf certificate
	leaf := state.PeerCertificates[0]

	domain := target.hostname
	if len(leaf.DNSNames) > 0 {
		domain = leaf.DNSNames[0]
	}

	return []CertificateInfo{{
		Subject:   domain,
		Issuer:    leaf.Issuer.CommonName,
		DNSNames:  leaf.DNSNames,
		NotBefore: leaf.NotBefore,
		NotAfter:  leaf.NotAfter,
		Serial:    leaf.SerialNumber.String(),
		IsCA:      leaf.IsCA,
		Source:    "tls",
		Host:      target.addr,
		AutoRenew: isAutoRenewedIssuer(leaf.Issuer.CommonName),
	}}
}

func isAutoRenewedIssuer(issuer string) bool {
	lower := strings.ToLower(issuer)
	return strings.Contains(lower, "let's encrypt") ||
		strings.Contains(lower, "zerossl") ||
		strings.Contains(lower, "caddy")
}

func isNumericPort(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func newGetRequest(ctx context.Context, url string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
}
