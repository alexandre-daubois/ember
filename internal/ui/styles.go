package ui

import "github.com/charmbracelet/lipgloss"

var (
	subtle     = lipgloss.AdaptiveColor{Light: "#555555", Dark: "#888888"}
	green      = lipgloss.AdaptiveColor{Light: "#00A500", Dark: "#00FF00"}
	red        = lipgloss.AdaptiveColor{Light: "#CC0000", Dark: "#FF5555"}
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(subtle).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(subtle)

	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#333366"))

	yellow = lipgloss.AdaptiveColor{Light: "#CCAA00", Dark: "#FFFF55"}

	busyStyle   = lipgloss.NewStyle().Foreground(red)
	idleStyle   = lipgloss.NewStyle().Foreground(green)
	greyStyle   = lipgloss.NewStyle().Foreground(subtle)
	warnStyle   = lipgloss.NewStyle().Foreground(yellow)
	dangerStyle = lipgloss.NewStyle().Bold(true).Foreground(red)
	leakStyle   = lipgloss.NewStyle().Foreground(yellow).Bold(true)

	helpStyle = lipgloss.NewStyle().Foreground(subtle)
)
