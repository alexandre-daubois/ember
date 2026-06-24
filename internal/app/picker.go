package app

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var errPickerAborted = errors.New("endpoint selection aborted")

// selectEndpoint reduces the file fleet to a single instance for the
// single-instance TUI: the configured default when set, otherwise an
// interactive picker.
func selectEndpoint(cfg *config) (addrSpec, error) {
	if cfg.configDefault != "" {
		for _, s := range cfg.addrs {
			if s.name == cfg.configDefault {
				return s, nil
			}
		}
		return addrSpec{}, fmt.Errorf("default endpoint %q not found in config (available: %s)", cfg.configDefault, specNames(cfg.addrs))
	}
	return runEndpointPicker(cfg.addrs, cfg.noColor)
}

func specNames(specs []addrSpec) string {
	names := make([]string, len(specs))
	for i, s := range specs {
		names[i] = s.name
	}
	return strings.Join(names, ", ")
}

func runEndpointPicker(specs []addrSpec, noColor bool) (addrSpec, error) {
	res, err := tea.NewProgram(pickerModel{specs: specs, noColor: noColor, chosen: -1}, tea.WithAltScreen()).Run()
	if err != nil {
		return addrSpec{}, err
	}
	final, ok := res.(pickerModel)
	if !ok || final.chosen < 0 {
		return addrSpec{}, errPickerAborted
	}
	return specs[final.chosen], nil
}

// pickerModel is a minimal list selector. The "> " caret marks the cursor so
// the selection stays legible when NO_COLOR strips the reverse-video styling.
type pickerModel struct {
	specs   []addrSpec
	cursor  int
	chosen  int
	noColor bool
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.chosen = -1
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.specs)-1 {
			m.cursor++
		}
	case "enter":
		m.chosen = m.cursor
		return m, tea.Quit
	}
	return m, nil
}

var pickerSelectedStyle = lipgloss.NewStyle().Reverse(true)

func (m pickerModel) View() string {
	var b strings.Builder
	b.WriteString("Select a Caddy instance:\n\n")
	for i, s := range m.specs {
		caret := "  "
		if i == m.cursor {
			caret = "> "
		}
		line := fmt.Sprintf("%s%-16s %s", caret, s.name, s.url)
		if i == m.cursor && !m.noColor {
			line = pickerSelectedStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("\n↑/↓ navigate · enter select · q quit\n")
	return b.String()
}
