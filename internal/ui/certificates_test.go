package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func sampleCerts() []fetcher.CertificateInfo {
	return []fetcher.CertificateInfo{
		{
			Subject:   "example.com",
			Issuer:    "Let's Encrypt Authority X3",
			DNSNames:  []string{"example.com", "www.example.com"},
			NotBefore: time.Now().Add(-30 * 24 * time.Hour),
			NotAfter:  time.Now().Add(60 * 24 * time.Hour),
			Source:    "tls",
			Host:      "example.com:443",
			AutoRenew: true,
		},
		{
			Subject:   "Caddy Local Authority",
			Issuer:    "Caddy Local Authority",
			NotBefore: time.Now().Add(-365 * 24 * time.Hour),
			NotAfter:  time.Now().Add(5 * 24 * time.Hour),
			Source:    "pki",
			Host:      "local",
			IsCA:      true,
		},
	}
}

func TestRenderCertificateTable_Empty(t *testing.T) {
	out := renderCertificateTable(nil, 0, 120, 20, model.SortByCertDomain)
	plain := stripANSI(out)
	assert.Contains(t, plain, "Domain")
	assert.Contains(t, plain, "Expires")
	assert.Contains(t, plain, "Src")
}

func TestRenderCertificateTable_WithCerts(t *testing.T) {
	certs := sampleCerts()
	out := renderCertificateTable(certs, 0, 120, 20, model.SortByCertDomain)
	plain := stripANSI(out)
	assert.Contains(t, plain, "example.com")
	assert.Contains(t, plain, "Caddy Local Authority")
}

func TestRenderCertificateTable_SortIndicator(t *testing.T) {
	certs := sampleCerts()
	out := renderCertificateTable(certs, 0, 120, 20, model.SortByCertExpiry)
	plain := stripANSI(out)
	assert.Contains(t, plain, "Expires ▼")
}

func TestRenderCertificateTable_ViewportClipping(t *testing.T) {
	certs := make([]fetcher.CertificateInfo, 50)
	for i := range certs {
		certs[i] = fetcher.CertificateInfo{
			Subject:  fmt.Sprintf("domain%d.com", i),
			Issuer:   "Test CA",
			NotAfter: time.Now().Add(60 * 24 * time.Hour),
			Source:   "tls",
		}
	}

	out := renderCertificateTable(certs, 25, 120, 15, model.SortByCertDomain)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	assert.Less(t, len(lines), 50, "viewport must clip output")
	assert.Contains(t, stripANSI(out), ">", "cursor row must be visible")
}

func TestRenderCertificateTable_ViewportCursorAtEnd(t *testing.T) {
	certs := make([]fetcher.CertificateInfo, 30)
	for i := range certs {
		certs[i] = fetcher.CertificateInfo{
			Subject:  fmt.Sprintf("domain%d.com", i),
			Issuer:   "Test CA",
			NotAfter: time.Now().Add(60 * 24 * time.Hour),
			Source:   "tls",
		}
	}

	out := renderCertificateTable(certs, 29, 120, 15, model.SortByCertDomain)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	assert.Less(t, len(lines), 30, "viewport must clip output")
	assert.Contains(t, stripANSI(out), ">", "cursor row must be visible")
}

func TestSortCerts_ByDomain(t *testing.T) {
	certs := []fetcher.CertificateInfo{
		{Subject: "zoo.com", DNSNames: []string{"zoo.com"}},
		{Subject: "alpha.com", DNSNames: []string{"alpha.com"}},
	}
	sorted := sortCerts(certs, model.SortByCertDomain)
	assert.Equal(t, "alpha.com", certDomain(sorted[0]))
	assert.Equal(t, "zoo.com", certDomain(sorted[1]))
}

func TestSortCerts_ByExpiry(t *testing.T) {
	now := time.Now()
	certs := []fetcher.CertificateInfo{
		{Subject: "later.com", NotAfter: now.Add(90 * 24 * time.Hour)},
		{Subject: "sooner.com", NotAfter: now.Add(10 * 24 * time.Hour)},
	}
	sorted := sortCerts(certs, model.SortByCertExpiry)
	assert.Equal(t, "sooner.com", sorted[0].Subject)
	assert.Equal(t, "later.com", sorted[1].Subject)
}

func TestSortCerts_BySource(t *testing.T) {
	certs := []fetcher.CertificateInfo{
		{Subject: "b.com", Source: "tls"},
		{Subject: "a.com", Source: "pki"},
	}
	sorted := sortCerts(certs, model.SortByCertSource)
	assert.Equal(t, "pki", sorted[0].Source)
	assert.Equal(t, "tls", sorted[1].Source)
}

func TestSortCerts_ByIssuer(t *testing.T) {
	certs := []fetcher.CertificateInfo{
		{Subject: "a.com", Issuer: "ZeroSSL"},
		{Subject: "b.com", Issuer: "Caddy"},
	}
	sorted := sortCerts(certs, model.SortByCertIssuer)
	assert.Equal(t, "Caddy", sorted[0].Issuer)
	assert.Equal(t, "ZeroSSL", sorted[1].Issuer)
}

func TestFilteredCerts(t *testing.T) {
	app := &App{
		certificates: sampleCerts(),
		filter:       "example",
		tabs:         []tab{tabCaddy, tabConfig, tabCertificates},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}, tabCertificates: {}},
		activeTab:    tabCertificates,
		history:      newHistoryStore(),
	}
	result := app.filteredCerts()
	assert.Len(t, result, 1)
	assert.Equal(t, "example.com", certDomain(result[0]))
}

func TestFilteredCerts_CaseInsensitive(t *testing.T) {
	app := &App{
		certificates: sampleCerts(),
		filter:       "CADDY",
		tabs:         []tab{tabCaddy, tabConfig, tabCertificates},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}, tabCertificates: {}},
		activeTab:    tabCertificates,
		history:      newHistoryStore(),
	}
	result := app.filteredCerts()
	assert.Len(t, result, 1)
}

func TestFilteredCerts_NoFilter(t *testing.T) {
	app := &App{
		certificates: sampleCerts(),
		tabs:         []tab{tabCaddy, tabConfig, tabCertificates},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}, tabCertificates: {}},
		activeTab:    tabCertificates,
		history:      newHistoryStore(),
	}
	result := app.filteredCerts()
	assert.Len(t, result, 2)
}

func TestDaysUntilExpiry(t *testing.T) {
	assert.Equal(t, 30, daysUntilExpiry(time.Now().Add(30*24*time.Hour+time.Hour)))
	assert.Equal(t, 0, daysUntilExpiry(time.Now().Add(12*time.Hour)))
	assert.Equal(t, -1, daysUntilExpiry(time.Now().Add(-12*time.Hour)))
	assert.Equal(t, -2, daysUntilExpiry(time.Now().Add(-36*time.Hour)))
}

func TestExpiryStyle_Green(t *testing.T) {
	s := expiryStyle(60)
	assert.Equal(t, okStyle, s)
}

func TestExpiryStyle_Warn(t *testing.T) {
	s := expiryStyle(15)
	assert.Equal(t, warnStyle, s)
}

func TestExpiryStyle_Danger(t *testing.T) {
	s := expiryStyle(3)
	assert.Equal(t, dangerStyle, s)
}

func TestExpiryStyle_Expired(t *testing.T) {
	s := expiryStyle(-1)
	assert.Equal(t, dangerStyle, s)
}

func TestCertDomain_PrefersDNSNames(t *testing.T) {
	c := fetcher.CertificateInfo{
		Subject:  "fallback.com",
		DNSNames: []string{"primary.com"},
	}
	assert.Equal(t, "primary.com", certDomain(c))
}

func TestCertDomain_FallsBackToSubject(t *testing.T) {
	c := fetcher.CertificateInfo{Subject: "only-subject.com"}
	assert.Equal(t, "only-subject.com", certDomain(c))
}

func TestCertExpiryWarning_None(t *testing.T) {
	certs := []fetcher.CertificateInfo{
		{NotAfter: time.Now().Add(60 * 24 * time.Hour)},
	}
	assert.Empty(t, certExpiryWarning(certs))
}

func TestCertExpiryWarning_One(t *testing.T) {
	certs := []fetcher.CertificateInfo{
		{NotAfter: time.Now().Add(3 * 24 * time.Hour)},
		{NotAfter: time.Now().Add(60 * 24 * time.Hour)},
	}
	assert.Contains(t, certExpiryWarning(certs), "1 certificate expiring")
}

func TestCertExpiryWarning_Multiple(t *testing.T) {
	certs := []fetcher.CertificateInfo{
		{NotAfter: time.Now().Add(2 * 24 * time.Hour)},
		{NotAfter: time.Now().Add(-24 * time.Hour)},
	}
	assert.Contains(t, certExpiryWarning(certs), "2 certificates expiring")
}

func newCertApp() *App {
	return &App{
		certificates: sampleCerts(),
		tabs:         []tab{tabCaddy, tabConfig, tabCertificates},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}, tabCertificates: {}},
		activeTab:    tabCertificates,
		history:      newHistoryStore(),
		width:        120,
		height:       40,
	}
}

func TestHandleCertListKey_Sort(t *testing.T) {
	app := newCertApp()
	assert.Equal(t, model.SortByCertDomain, app.certSortBy)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	assert.Equal(t, model.SortByCertExpiry, app.certSortBy)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	assert.Equal(t, model.SortByCertDomain, app.certSortBy)
}

func TestHandleCertListKey_Navigate(t *testing.T) {
	app := newCertApp()
	assert.Equal(t, 0, app.cursor)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, app.cursor)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, app.cursor)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, len(app.filteredCerts())-1, app.cursor)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, app.cursor)
}

func TestHandleCertListKey_Pause(t *testing.T) {
	app := newCertApp()
	assert.False(t, app.paused)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.True(t, app.paused)
}

func TestHandleCertListKey_Filter(t *testing.T) {
	app := newCertApp()

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, viewFilter, app.mode)
}

func TestHandleCertListKey_Refresh(t *testing.T) {
	app := newCertApp()
	app.certificates = sampleCerts()

	_, cmd := app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Nil(t, app.certificates)
	assert.NotNil(t, cmd)
}

func TestHandleCertListKey_TabSwitch(t *testing.T) {
	app := newCertApp()

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	assert.Equal(t, tabCaddy, app.activeTab)
}

func TestHandleCertListKey_Help(t *testing.T) {
	app := newCertApp()

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, viewHelp, app.mode)
}

func TestHandleCertListKey_Graph(t *testing.T) {
	app := newCertApp()

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	assert.Equal(t, viewGraph, app.mode)
}

func TestHandleCertListKey_EmptyList(t *testing.T) {
	app := &App{
		certificates: []fetcher.CertificateInfo{},
		tabs:         []tab{tabCaddy, tabConfig, tabCertificates},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}, tabCertificates: {}},
		activeTab:    tabCertificates,
		history:      newHistoryStore(),
		width:        120,
		height:       40,
	}

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 0, app.cursor)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, 0, app.cursor)

	app.handleCertListKey(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, 0, app.cursor)
}

func TestCertExpiryWarning_ExactBoundary(t *testing.T) {
	certs := []fetcher.CertificateInfo{
		{NotAfter: time.Now().Add(7*24*time.Hour + time.Hour)},
	}
	assert.Empty(t, certExpiryWarning(certs), "above 7 days should not warn")

	certs = []fetcher.CertificateInfo{
		{NotAfter: time.Now().Add(6*24*time.Hour + time.Hour)},
	}
	assert.NotEmpty(t, certExpiryWarning(certs), "below 7 days should warn")
}

func TestFormatCertRow_Truncation(t *testing.T) {
	c := fetcher.CertificateInfo{
		Subject:  "very-long-domain-name-that-exceeds-column-width.example.com",
		Issuer:   "Very Long Issuer Name That Should Be Truncated For Display",
		NotAfter: time.Now().Add(60 * 24 * time.Hour),
		Source:   "tls",
	}
	row := stripANSI(formatCertRow(c, 100, 20, false, false))
	assert.Contains(t, row, "…")
}

func TestFormatCertRow_AutoRenew(t *testing.T) {
	c := fetcher.CertificateInfo{
		Subject:   "example.com",
		NotAfter:  time.Now().Add(60 * 24 * time.Hour),
		Source:    "tls",
		AutoRenew: true,
	}
	row := stripANSI(formatCertRow(c, 120, 30, false, false))
	assert.Contains(t, row, "yes")
}

func TestFormatCertRow_Expired(t *testing.T) {
	c := fetcher.CertificateInfo{
		Subject:  "expired.com",
		NotAfter: time.Now().Add(-48 * time.Hour),
		Source:   "tls",
	}
	row := stripANSI(formatCertRow(c, 120, 30, false, false))
	assert.Contains(t, row, "expired")
}
