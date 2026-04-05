package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ember)
	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(subtle)
)

func tabLabel(t tab, pluginTabs []*pluginTab) string {
	switch t {
	case tabCaddy:
		return "Caddy"
	case tabConfig:
		return "Caddy Config"
	case tabCertificates:
		return "Certificates"
	case tabFrankenPHP:
		return "FrankenPHP"
	default:
		for _, pt := range pluginTabs {
			if pt.tabID == t {
				return pt.tabName
			}
		}
		return "?"
	}
}

func renderTabBar(tabs []tab, active tab, width int, counts map[tab]string, pluginTabs []*pluginTab) string {
	var parts []string
	for _, t := range tabs {
		label := tabLabel(t, pluginTabs)
		if c, ok := counts[t]; ok && c != "" {
			label += " (" + c + ")"
		}
		if t == active {
			parts = append(parts, activeTabStyle.Render(" ["+label+"]"))
		} else {
			parts = append(parts, inactiveTabStyle.Render("  "+label+" "))
		}
	}
	bar := strings.Join(parts, "")
	return lipgloss.NewStyle().Width(width).Render(bar)
}
