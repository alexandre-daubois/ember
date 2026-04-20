package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderTabBar_NoCounts(t *testing.T) {
	tabs := []tab{tabCaddy, tabFrankenPHP}
	result := renderTabBar(tabs, tabCaddy, 80, nil)

	assert.Contains(t, result, "Caddy")
	assert.Contains(t, result, "FrankenPHP")
}

func TestRenderTabBar_WithCounts(t *testing.T) {
	tabs := []tab{tabCaddy, tabFrankenPHP}
	counts := map[tab]string{
		tabCaddy:      "5 hosts",
		tabFrankenPHP: "12 threads",
	}
	result := renderTabBar(tabs, tabCaddy, 80, counts)

	assert.Contains(t, result, "Caddy (5 hosts)")
	assert.Contains(t, result, "FrankenPHP (12 threads)")
}

func TestRenderTabBar_PartialCounts(t *testing.T) {
	tabs := []tab{tabCaddy, tabFrankenPHP}
	counts := map[tab]string{
		tabCaddy: "3 hosts",
	}
	result := renderTabBar(tabs, tabCaddy, 80, counts)

	assert.Contains(t, result, "Caddy (3 hosts)")
	assert.NotContains(t, result, "FrankenPHP (")
}

func TestRenderTabBar_EmptyCountIgnored(t *testing.T) {
	tabs := []tab{tabCaddy}
	counts := map[tab]string{
		tabCaddy: "",
	}
	result := renderTabBar(tabs, tabCaddy, 80, counts)

	assert.NotContains(t, result, "()")
}

func TestTabLabel_AllKnown(t *testing.T) {
	cases := map[tab]string{
		tabCaddy:        "Caddy",
		tabConfig:       "Caddy Config",
		tabCertificates: "Certificates",
		tabUpstreams:    "Upstreams",
		tabLogs:         "Logs",
		tabFrankenPHP:   "FrankenPHP",
	}
	for tb, want := range cases {
		assert.Equal(t, want, tabLabel(tb))
	}
}

func TestTabLabel_UnknownReturnsPlaceholder(t *testing.T) {
	// Unknown tab values must render as a stable placeholder rather than
	// an empty string so a coding mistake stays visible in the bar.
	assert.Equal(t, "?", tabLabel(tab(999)))
}
