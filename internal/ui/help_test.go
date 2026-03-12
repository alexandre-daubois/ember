package ui

import (
	"testing"

	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestRenderHelp_ContainsAllBindings(t *testing.T) {
	out := renderHelp(model.SortByIndex, model.SortByHost, false, 120, TabFrankenPHP)
	plain := stripANSI(out)

	assert.Contains(t, plain, "navigate")
	assert.Contains(t, plain, "sort(index)")
	assert.Contains(t, plain, "pause")
	assert.Contains(t, plain, "restart")
	assert.Contains(t, plain, "filter")
	assert.Contains(t, plain, "quit")
}

func TestRenderHelp_ShowsCurrentSortField(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByMemory, model.SortByHost, false, 120, TabFrankenPHP))
	assert.Contains(t, out, "sort(memory)")
}

func TestRenderHelp_PausedShowsResume(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, true, 120, TabFrankenPHP))
	assert.Contains(t, out, "resume")
	assert.NotContains(t, out, "pause")
}

func TestRenderHelp_RespectsWidth(t *testing.T) {
	out := renderHelp(model.SortByIndex, model.SortByHost, false, 200, TabFrankenPHP)
	assert.Equal(t, 200, lipgloss.Width(out))
}

func TestRenderHelp_SeparatorsPresent(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, false, 120, TabFrankenPHP))
	assert.Contains(t, out, "·")
}
