package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func threeSpecs() []addrSpec {
	return []addrSpec{
		{name: "alpha", url: "http://a"},
		{name: "beta", url: "http://b"},
		{name: "gamma", url: "http://c"},
	}
}

func TestSelectEndpoint_DefaultFound(t *testing.T) {
	cfg := &config{configDefault: "beta", addrs: threeSpecs()}
	spec, err := selectEndpoint(cfg)
	require.NoError(t, err)
	assert.Equal(t, "beta", spec.name)
}

func TestSelectEndpoint_DefaultNotFound(t *testing.T) {
	cfg := &config{configDefault: "ghost", addrs: threeSpecs()}
	_, err := selectEndpoint(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "alpha, beta, gamma")
}

func sendKey(m pickerModel, msg tea.Msg) pickerModel {
	next, _ := m.Update(msg)
	return next.(pickerModel)
}

func TestPickerModel_NavigateDown(t *testing.T) {
	m := pickerModel{specs: threeSpecs(), chosen: -1}
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, m.cursor)
}

func TestPickerModel_NavigateDownClamps(t *testing.T) {
	m := pickerModel{specs: threeSpecs(), cursor: 2, chosen: -1}
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, m.cursor, "cursor must not move past the last item")
}

func TestPickerModel_NavigateUpClamps(t *testing.T) {
	m := pickerModel{specs: threeSpecs(), cursor: 0, chosen: -1}
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, m.cursor, "cursor must not move before the first item")
}

func TestPickerModel_NavigateWithJK(t *testing.T) {
	m := pickerModel{specs: threeSpecs(), chosen: -1}
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, 1, m.cursor)
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	assert.Equal(t, 0, m.cursor)
}

func TestPickerModel_Enter(t *testing.T) {
	m := pickerModel{specs: threeSpecs(), cursor: 1, chosen: -1}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, 1, next.(pickerModel).chosen)
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestPickerModel_Quit(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	} {
		next, cmd := pickerModel{specs: threeSpecs(), cursor: 1, chosen: -1}.Update(msg)
		assert.Equal(t, -1, next.(pickerModel).chosen, "abort keeps chosen at -1")
		require.NotNil(t, cmd)
		assert.IsType(t, tea.QuitMsg{}, cmd())
	}
}

func TestPickerModel_ViewShowsAllAndCaret(t *testing.T) {
	m := pickerModel{specs: threeSpecs(), cursor: 1, chosen: -1}
	view := m.View()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		assert.Contains(t, view, name)
	}
	// the caret marks the cursor line, which is what keeps the picker usable
	// under NO_COLOR; assert it sits on the second (beta) line.
	lines := strings.Split(view, "\n")
	var betaLine string
	for _, l := range lines {
		if strings.Contains(l, "beta") {
			betaLine = l
		}
	}
	assert.Contains(t, betaLine, "> ")
}

func TestPickerModel_ViewNoColorHasNoEscapes(t *testing.T) {
	m := pickerModel{specs: threeSpecs(), cursor: 0, chosen: -1, noColor: true}
	view := m.View()
	assert.NotContains(t, view, "\x1b[", "NO_COLOR view must not emit ANSI escapes")
	assert.Contains(t, view, "> alpha")
}
