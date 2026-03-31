package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderTabBar_NoCounts(t *testing.T) {
	tabs := []tab{tabCaddy, tabFrankenPHP}
	result := renderTabBar(tabs, tabCaddy, 80, nil, nil)

	assert.Contains(t, result, "Caddy")
	assert.Contains(t, result, "FrankenPHP")
}

func TestRenderTabBar_WithCounts(t *testing.T) {
	tabs := []tab{tabCaddy, tabFrankenPHP}
	counts := map[tab]string{
		tabCaddy:      "5 hosts",
		tabFrankenPHP: "12 threads",
	}
	result := renderTabBar(tabs, tabCaddy, 80, counts, nil)

	assert.Contains(t, result, "Caddy (5 hosts)")
	assert.Contains(t, result, "FrankenPHP (12 threads)")
}

func TestRenderTabBar_PartialCounts(t *testing.T) {
	tabs := []tab{tabCaddy, tabFrankenPHP}
	counts := map[tab]string{
		tabCaddy: "3 hosts",
	}
	result := renderTabBar(tabs, tabCaddy, 80, counts, nil)

	assert.Contains(t, result, "Caddy (3 hosts)")
	assert.NotContains(t, result, "FrankenPHP (")
}

func TestRenderTabBar_EmptyCountIgnored(t *testing.T) {
	tabs := []tab{tabCaddy}
	counts := map[tab]string{
		tabCaddy: "",
	}
	result := renderTabBar(tabs, tabCaddy, 80, counts, nil)

	assert.NotContains(t, result, "()")
}
