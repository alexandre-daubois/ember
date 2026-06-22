package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
)

// evil carries, at the very front so column truncation can't drop it, an OSC
// window-title spoof (ESC ] … BEL), a CSI screen-erase (ESC [ 2J) and a stray
// BEL — the three shapes the advisory's PoC abuses.
const evil = "\x1b]0;PWNED\x07\x1b[2J/api/v1/users?q=attack"

// assertNoEscapeInjection fails if attacker-controlled terminal-escape
// introducers survive into rendered output. BEL (0x07) and the OSC introducer
// (ESC ]) are the discriminators: legitimate lipgloss styling only ever emits
// SGR (ESC [ … m), so neither can appear unless an untrusted field leaked an
// escape sequence through unneutralised. Asserting on the raw output (not
// stripANSI'd) is essential — stripANSI would remove the evidence either way.
func assertNoEscapeInjection(t *testing.T, where, out string) {
	t.Helper()
	assert.NotContainsf(t, out, "\x07", "%s: BEL byte must not reach the terminal", where)
	assert.NotContainsf(t, out, "\x1b]", "%s: OSC escape introducer must not reach the terminal", where)
	// ESC must be gone from the payload entirely; the inert remnant stays.
	assert.NotContainsf(t, out, "\x1b[2J", "%s: injected CSI must not reach the terminal", where)
}

func TestRenderSinksNeutralizeControlBytes(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name string
		out  func() string
	}{
		{"formatLogRow access URI", func() string {
			return formatLogRow(fetcher.LogEntry{Method: "GET", Host: "h", URI: evil, Status: 200}, 120, 60, false, false)
		}},
		{"formatLogRow access URI selected", func() string {
			return formatLogRow(fetcher.LogEntry{Method: "GET", Host: "h", URI: evil, Status: 200}, 120, 60, true, false)
		}},
		{"formatLogRow host+method", func() string {
			return formatLogRow(fetcher.LogEntry{Method: evil, Host: evil, URI: "/", Status: 500}, 120, 60, false, false)
		}},
		{"formatLogRow parse error raw line", func() string {
			return formatLogRow(fetcher.LogEntry{ParseError: true, RawLine: evil}, 120, 60, false, false)
		}},
		{"formatRuntimeLogRow message+logger", func() string {
			return formatRuntimeLogRow(fetcher.LogEntry{Level: "info", Logger: evil, Message: evil}, 120, 60, false)
		}},
		{"formatRuntimeLogRow parse error raw line", func() string {
			return formatRuntimeLogRow(fetcher.LogEntry{ParseError: true, RawLine: evil}, 120, 60, false)
		}},
		{"formatRouteRow method+pattern+host", func() string {
			s := model.RouteStat{Key: model.RouteKey{Host: evil, Method: evil, Pattern: evil}, Count: 1, Status2xx: 1}
			return formatRouteRow(s, 120, 40, 0, true, false)
		}},
		{"formatRouteRow selected", func() string {
			s := model.RouteStat{Key: model.RouteKey{Host: evil, Method: evil, Pattern: evil}, Count: 1, Status2xx: 1}
			return formatRouteRow(s, 120, 40, 0, true, true)
		}},
		{"formatCertRow subject+issuer", func() string {
			c := fetcher.CertificateInfo{Subject: evil, Issuer: evil, NotAfter: now.Add(48 * time.Hour), Source: "tls"}
			return formatCertRow(c, 120, 40, false)
		}},
		{"formatCertRow DNS name", func() string {
			c := fetcher.CertificateInfo{DNSNames: []string{evil}, NotAfter: now.Add(48 * time.Hour), Source: "tls"}
			return formatCertRow(c, 120, 40, false)
		}},
		{"formatHostRow host", func() string {
			return formatHostRow(model.HostDerived{Host: evil, StatusCodes: map[int]float64{}}, 120, 40, false, nil)
		}},
		{"renderHostDetailPanel host+method", func() string {
			h := model.HostDerived{Host: evil, MethodRates: map[string]float64{evil: 1}, StatusCodes: map[int]float64{}}
			return renderHostDetailPanel(h, 80, 40)
		}},
		{"formatThreadRow currentURI+method", func() string {
			th := fetcher.ThreadDebugState{IsBusy: true, CurrentMethod: evil, CurrentURI: evil}
			return formatThreadRow(th, 120, 60, renderOpts{}, false)
		}},
		{"renderDetailPanel currentURI", func() string {
			th := fetcher.ThreadDebugState{IsBusy: true, CurrentMethod: evil, CurrentURI: evil}
			return renderDetailPanel(th, 60, 20, nil, now)
		}},
		{"renderSidepanel host label", func() string {
			items := []sidepanelItem{{kind: logSelAccessHost, label: evil, host: evil, indent: 1}}
			return renderSidepanel(items, 0, false, sidepanelFixedWidth, 10)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.out()
			// Sanity: the payload must actually reach rendering (a row that
			// silently dropped the field would pass the security check for the
			// wrong reason). The inert remnant survives once ESC is stripped.
			assert.Contains(t, out, "PWNED", "neutralised payload remnant should still render")
			assertNoEscapeInjection(t, tc.name, out)
		})
	}
}

// TestRenderLogsTabNeutralizesURI exercises the full default-tab View() path
// end to end (Scenario A): a remote request URI carrying escape sequences,
// reflected through the access buffer, must not survive into the frame.
func TestRenderLogsTabNeutralizesURI(t *testing.T) {
	buf := model.NewLogBuffer(8)
	buf.Append(fetcher.LogEntry{
		Timestamp: time.Now(),
		Host:      "victim.example.com",
		Method:    "GET",
		URI:       evil,
		Status:    200,
		Duration:  0.01,
	})
	app := newLogsApp(buf)

	out := app.renderLogsTab(120, 20)
	assert.Contains(t, out, "PWNED")
	assertNoEscapeInjection(t, "renderLogsTab", out)
}

// TestRenderRoutesViewNeutralizes covers the By Route aggregation path, whose
// keys (method/pattern/host) are derived from the same access logs.
func TestRenderRoutesViewNeutralizes(t *testing.T) {
	app := newLogsApp(model.NewLogBuffer(8))
	app.routeAggregator.Track(fetcher.LogEntry{
		Logger: "http.log.access.log0",
		Host:   "victim.example.com",
		Method: "GET",
		URI:    evil,
		Status: 200,
	})
	app.logSel = logSel{kind: logSelRoutes}

	out := app.renderLogsTab(120, 20)
	assertNoEscapeInjection(t, "renderRoutesView", out)
}

// TestViewStatusLineNeutralizes covers the status line, which concatenates
// .Error() strings that can transitively carry externally-sourced bytes.
func TestViewStatusLineNeutralizes(t *testing.T) {
	app := newLogsApp(model.NewLogBuffer(8))

	app.status = "metrics parse failed: " + evil
	assertNoEscapeInjection(t, "View status", app.View())

	app.status = ""
	app.err = errors.New(evil)
	assertNoEscapeInjection(t, "View err", app.View())
}

// TestLogsEmptyHintNeutralizesHost covers the per-host empty hint, which echoes
// the selected (attacker-controlled) host outside the fitCellLeft path.
func TestLogsEmptyHintNeutralizesHost(t *testing.T) {
	buf := model.NewLogBuffer(8)
	buf.Append(fetcher.LogEntry{Timestamp: time.Now(), Host: "other", Method: "GET", URI: "/", Status: 200})
	app := newLogsApp(buf)
	app.logSel = logSel{kind: logSelAccessHost, host: evil}

	hint := app.logsEmptyHint(0)
	assert.Contains(t, hint, "No access logs yet for", "expected the per-host empty hint")
	assertNoEscapeInjection(t, "logsEmptyHint", hint)
}
