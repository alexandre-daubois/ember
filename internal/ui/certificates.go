package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	colCertExpires = 12
	colCertDays    = 10
	colCertSource  = 6
	colCertIssuer  = 40
	colCertAuto    = 6
	colCertFixed   = 1 + colCertExpires + colCertDays + colCertSource + colCertIssuer + colCertAuto
	minCertRows    = 10
	expiryWarnDays = 30
	expiryCritDays = 7
)

func certDomainWidth(totalWidth int) int {
	w := totalWidth - colCertFixed
	if w < 15 {
		w = 15
	}
	return w
}

func renderCertificateTable(certs []fetcher.CertificateInfo, cursor, width int, sortBy model.CertSortField) string {
	domW := certDomainWidth(width)

	colHead := func(label string, field model.CertSortField, w int, right bool) string {
		if sortBy == field {
			label += " ▼"
		}
		if right {
			return fmt.Sprintf("%*s", w, label)
		}
		return fmt.Sprintf("%-*s", w, label)
	}

	header := fmt.Sprintf(" %-*s%*s%*s%*s%*s%*s",
		domW, colHead("Domain", model.SortByCertDomain, domW, false),
		colCertExpires, colHead("Expires", model.SortByCertExpiry, colCertExpires, true),
		colCertDays, "Days",
		colCertSource, colHead("Src", model.SortByCertSource, colCertSource, true),
		colCertIssuer, colHead("Issuer", model.SortByCertIssuer, colCertIssuer, true),
		colCertAuto, "Auto",
	)

	headerLine := tableHeaderStyle.Width(width).Render(header)

	var rows []string
	for i, c := range certs {
		rows = append(rows, formatCertRow(c, width, domW, i == cursor, i%2 == 1))
	}

	for i := len(certs); i < minCertRows; i++ {
		emptyRow := fmt.Sprintf(" %-*s%*s%*s%*s%*s%*s",
			domW, "", colCertExpires, "", colCertDays, "",
			colCertSource, "", colCertIssuer, "", colCertAuto, "")
		style := lipgloss.NewStyle()
		if i%2 == 1 {
			style = zebraStyle
		}
		rows = append(rows, style.Width(width).Render(emptyRow))
	}

	content := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, content)
}

func formatCertRow(c fetcher.CertificateInfo, width, domW int, selected, zebra bool) string {
	domain := c.Subject
	if len(c.DNSNames) > 0 {
		domain = c.DNSNames[0]
	}
	if len(domain) > domW-1 {
		domain = domain[:domW-2] + "…"
	}

	expires := c.NotAfter.Format("2006-01-02")
	days := daysUntilExpiry(c.NotAfter)
	daysStr := fmt.Sprintf("%d", days)
	if days < 0 {
		daysStr = "expired"
	}

	src := strings.ToUpper(c.Source)
	issuer := c.Issuer
	if len(issuer) > colCertIssuer-1 {
		issuer = issuer[:colCertIssuer-2] + "…"
	}

	autoStr := "—"
	if c.AutoRenew {
		autoStr = "yes"
	}

	prefix := " "
	if selected {
		prefix = ">"
	}

	domPart := fmt.Sprintf("%s%-*s", prefix, domW, domain)
	expPart := fmt.Sprintf("%*s", colCertExpires, expires)
	daysPart := fmt.Sprintf("%*s", colCertDays, daysStr)
	srcPart := fmt.Sprintf("%*s", colCertSource, src)
	issuerPart := fmt.Sprintf("%*s", colCertIssuer, issuer)
	autoPart := fmt.Sprintf("%*s", colCertAuto, autoStr)

	if selected {
		row := domPart + expPart + daysPart + srcPart + issuerPart + autoPart
		return selectedRowStyle.Width(width).Render(row)
	}

	style := lipgloss.NewStyle()
	if zebra {
		style = zebraStyle
	}

	styledDays := expiryStyle(days).Render(daysPart)

	row := style.Render(domPart) +
		style.Render(expPart) +
		styledDays +
		style.Render(srcPart) +
		style.Render(issuerPart) +
		style.Render(autoPart)

	if zebra {
		return zebraStyle.Width(width).Render(row)
	}
	return row
}

func daysUntilExpiry(notAfter time.Time) int {
	d := time.Until(notAfter)
	if d < 0 {
		// floor toward negative infinity for expired certs so that
		// <24h past expiry correctly returns -1 instead of 0.
		return -int((-d).Hours()/24) - 1
	}
	return int(d.Hours() / 24)
}

func expiryStyle(days int) lipgloss.Style {
	switch {
	case days < 0:
		return dangerStyle
	case days < expiryCritDays:
		return dangerStyle
	case days < expiryWarnDays:
		return warnStyle
	default:
		return okStyle
	}
}

func sortCerts(certs []fetcher.CertificateInfo, by model.CertSortField) []fetcher.CertificateInfo {
	sorted := make([]fetcher.CertificateInfo, len(certs))
	copy(sorted, certs)

	slices.SortStableFunc(sorted, func(a, b fetcher.CertificateInfo) int {
		switch by {
		case model.SortByCertExpiry:
			return a.NotAfter.Compare(b.NotAfter)
		case model.SortByCertSource:
			return cmp.Compare(a.Source, b.Source)
		case model.SortByCertIssuer:
			return cmp.Compare(a.Issuer, b.Issuer)
		default:
			return cmp.Compare(certDomain(a), certDomain(b))
		}
	})

	return sorted
}

func certDomain(c fetcher.CertificateInfo) string {
	if len(c.DNSNames) > 0 {
		return c.DNSNames[0]
	}
	return c.Subject
}

func (a *App) filteredCerts() []fetcher.CertificateInfo {
	certs := sortCerts(a.certificates, a.certSortBy)
	if a.filter == "" {
		return certs
	}
	f := strings.ToLower(a.filter)
	var result []fetcher.CertificateInfo
	for _, c := range certs {
		if strings.Contains(strings.ToLower(certDomain(c)), f) ||
			strings.Contains(strings.ToLower(c.Issuer), f) ||
			strings.Contains(strings.ToLower(c.Source), f) {
			result = append(result, c)
		}
	}
	return result
}

func certExpiryWarning(certs []fetcher.CertificateInfo) string {
	var expiring int
	for _, c := range certs {
		if daysUntilExpiry(c.NotAfter) < expiryCritDays {
			expiring++
		}
	}
	if expiring == 0 {
		return ""
	}
	if expiring == 1 {
		return "⚠ 1 certificate expiring soon"
	}
	return fmt.Sprintf("⚠ %d certificates expiring soon", expiring)
}

func (a *App) handleCertListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	certs := a.filteredCerts()
	maxIdx := len(certs) - 1
	if maxIdx < 0 {
		maxIdx = 0
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "tab":
		a.nextTab()
		return a, a.switchTabCmd()
	case "shift+tab":
		a.prevTab()
		return a, a.switchTabCmd()
	case "1":
		if len(a.tabs) > 0 {
			a.switchTab(a.tabs[0])
		}
		return a, a.switchTabCmd()
	case "2":
		if len(a.tabs) > 1 {
			a.switchTab(a.tabs[1])
		}
		return a, a.switchTabCmd()
	case "3":
		if len(a.tabs) > 2 {
			a.switchTab(a.tabs[2])
		}
		return a, a.switchTabCmd()
	case "4":
		if len(a.tabs) > 3 {
			a.switchTab(a.tabs[3])
		}
		return a, a.switchTabCmd()
	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		if a.cursor < maxIdx {
			a.cursor++
		}
	case "home":
		a.cursor = 0
	case "end":
		a.cursor = maxIdx
	case "pgup":
		a.cursor -= a.pageSize()
		if a.cursor < 0 {
			a.cursor = 0
		}
	case "pgdown":
		a.cursor += a.pageSize()
		if a.cursor > maxIdx {
			a.cursor = maxIdx
		}
	case "s":
		a.certSortBy = a.certSortBy.Next()
	case "S":
		a.certSortBy = a.certSortBy.Prev()
	case "p":
		a.paused = !a.paused
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "r":
		a.certificates = nil
		return a, a.doFetchCertificates()
	case "g":
		a.prevMode = a.mode
		a.mode = viewGraph
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}
