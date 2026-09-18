package ui

import (
	"fmt"
	"strings"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var mercureLabelStyle = lipgloss.NewStyle().Foreground(subtle).Width(20)

func mercureKV(label, value string) string {
	return "  " + mercureLabelStyle.Render(label) + detailValStyle.Render(value)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// configs is nil while the config fetch is in flight.
func renderMercureView(s *model.State, configs []fetcher.MercureConfig, subsHistory, updatesHistory []float64, width, height int, feedAvailable bool) string {
	if s.Current == nil || s.Current.Metrics.Mercure == nil {
		return greyStyle.Render(" Waiting for Mercure metrics...")
	}
	m := s.Current.Metrics.Mercure
	d := s.Derived

	lines := []string{
		sectionHeader("Hub activity", width),
		mercureKV("Subscribers", fmt.Sprintf("%-8s %s", formatNumber(int64(m.SubscribersConnected)), renderSparkline(subsHistory, sparklineSize))),
		mercureKV("New subscribers/s", fmt.Sprintf("%.1f", d.MercureSubscribersPerSec)),
		mercureKV("Updates/s", fmt.Sprintf("%-8s %s", fmt.Sprintf("%.1f", d.MercureUpdatesPerSec), renderSparkline(updatesHistory, sparklineSize))),
		mercureKV("Subscribers total", formatNumber(int64(m.SubscribersTotal))),
		mercureKV("Updates total", formatNumber(int64(m.UpdatesTotal))),
	}
	if feedAvailable {
		lines = append(lines, mercureKV("Updates feed", greyStyle.Render("press m: Logs › Mercure shows each update with its topics and payload")))
	}
	lines = append(lines, "")

	switch {
	case configs == nil:
		lines = append(lines, sectionHeader("Hub configuration", width), greyStyle.Render("  Loading hub config..."))
	case len(configs) == 0:
		lines = append(lines, sectionHeader("Hub configuration", width), greyStyle.Render("  No mercure handler found in the Caddy config"))
	default:
		for i, c := range configs {
			title := "Hub configuration"
			if len(configs) > 1 {
				title = fmt.Sprintf("Hub configuration %d/%d", i+1, len(configs))
			}
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, sectionHeader(title, width))
			lines = append(lines, renderMercureConfig(c)...)
		}
	}

	if len(lines) > height {
		lines = lines[:height]
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.Join(lines, "\n"))
}

func renderMercureConfig(c fetcher.MercureConfig) []string {
	var lines []string
	if c.Name != "" {
		lines = append(lines, mercureKV("Name", sanitizeControl(c.Name)))
	}
	lines = append(lines, mercureKV("Transport", sanitizeControl(c.Transport)))
	lines = append(lines, mercureKV("Anonymous", yesNo(c.Anonymous)))
	lines = append(lines, mercureKV("Subscriptions API", yesNo(c.Subscriptions)))
	lines = append(lines, mercureKV("Debugger", yesNo(c.Debugger)))
	playground := yesNo(c.Playground)
	if c.Playground {
		playground = warnStyle.Render("yes (dev only, insecure)")
	}
	lines = append(lines, mercureKV("Playground", playground))
	lines = append(lines, mercureKV("Heartbeat", orDefault(c.Heartbeat, "40s")))
	lines = append(lines, mercureKV("Write timeout", orDefault(c.WriteTimeout, "600s")))
	lines = append(lines, mercureKV("Dispatch timeout", orDefault(c.DispatchTimeout, "5s")))
	if c.ProtocolCompat > 0 {
		lines = append(lines, mercureKV("Protocol compat", fmt.Sprintf("v%d", c.ProtocolCompat)))
	}
	lines = append(lines, mercureKV("Issuers", joinOrDash(c.Issuers)))
	lines = append(lines, mercureKV("CORS origins", joinOrDash(c.CORSOrigins)))
	lines = append(lines, mercureKV("Publish origins", joinOrDash(c.PublishOrigins)))
	if len(c.PublicURLs) > 0 {
		lines = append(lines, mercureKV("Public URLs", joinOrDash(c.PublicURLs)))
	}
	return lines
}

func orDefault(v, def string) string {
	if v == "" {
		return def + greyStyle.Render(" (default)")
	}
	return v
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	return sanitizeControl(strings.Join(values, ", "))
}

func (a *App) mercureTabHelpBindings() []binding {
	var bindings []binding
	if a.config.MercureFeed != nil {
		bindings = append(bindings, binding{"m", "messages"})
	}
	pauseLabel := "pause"
	if a.paused {
		pauseLabel = "resume"
	}
	return append(bindings,
		binding{"p", pauseLabel},
		binding{"r", "refresh config"},
		binding{"g", "graphs"},
		binding{"Tab/S-Tab", "switch"},
		binding{"q", "quit"},
	)
}

func (a *App) handleMercureListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if cmd, ok := a.handleTabSwitch(key); ok {
		return a, cmd
	}

	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "p":
		a.paused = !a.paused
	case "r":
		a.mercureConfigs = nil
		return a, a.doFetchMercureConfig()
	case "m":
		if a.config.MercureFeed != nil {
			return a, a.jumpToMercureMessages()
		}
	case "g":
		a.prevMode = a.mode
		a.mode = viewGraph
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}
