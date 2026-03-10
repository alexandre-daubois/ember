package ui

import "github.com/charmbracelet/lipgloss"

var (
	subtle = lipgloss.AdaptiveColor{Light: "#7A6652", Dark: "#A0896E"}
	amber  = lipgloss.AdaptiveColor{Light: "#CC8800", Dark: "#FFAA00"}
	ember  = lipgloss.AdaptiveColor{Light: "#CC4400", Dark: "#FF6B35"}
	red    = lipgloss.AdaptiveColor{Light: "#CC0000", Dark: "#FF4444"}
	warn   = lipgloss.AdaptiveColor{Light: "#CC6600", Dark: "#FF8C00"}
	leak   = lipgloss.AdaptiveColor{Light: "#CC3300", Dark: "#FF6347"}

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ember)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ember).
			Padding(0, 1)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(subtle).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(subtle)

	selectedRowStyle = lipgloss.NewStyle().
				Reverse(true)

	busyStyle   = lipgloss.NewStyle().Foreground(red)
	idleStyle   = lipgloss.NewStyle().Foreground(amber)
	greyStyle   = lipgloss.NewStyle().Foreground(subtle)
	warnStyle   = lipgloss.NewStyle().Foreground(warn)
	dangerStyle = lipgloss.NewStyle().Bold(true).Foreground(red)
	leakStyle   = lipgloss.NewStyle().Foreground(leak).Bold(true)

	helpStyle    = lipgloss.NewStyle().Foreground(subtle)
	helpKeyStyle = lipgloss.NewStyle().Foreground(ember).Bold(true)

	separatorStyle = lipgloss.NewStyle().Foreground(subtle)

	zebraBg    = lipgloss.AdaptiveColor{Light: "#F5F0EB", Dark: "#1A1410"}
	zebraStyle = lipgloss.NewStyle().Background(zebraBg)
)
