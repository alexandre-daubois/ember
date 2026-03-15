package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderTabBar_NoCounts(t *testing.T) {
	tabs := []Tab{TabCaddy, TabFrankenPHP}
	result := renderTabBar(tabs, TabCaddy, 80, nil)

	assert.Contains(t, result, "Caddy")
	assert.Contains(t, result, "FrankenPHP")
}

func TestRenderTabBar_WithCounts(t *testing.T) {
	tabs := []Tab{TabCaddy, TabFrankenPHP}
	counts := map[Tab]string{
		TabCaddy:      "5 hosts",
		TabFrankenPHP: "12 threads",
	}
	result := renderTabBar(tabs, TabCaddy, 80, counts)

	assert.Contains(t, result, "Caddy (5 hosts)")
	assert.Contains(t, result, "FrankenPHP (12 threads)")
}

func TestRenderTabBar_PartialCounts(t *testing.T) {
	tabs := []Tab{TabCaddy, TabFrankenPHP}
	counts := map[Tab]string{
		TabCaddy: "3 hosts",
	}
	result := renderTabBar(tabs, TabCaddy, 80, counts)

	assert.Contains(t, result, "Caddy (3 hosts)")
	assert.NotContains(t, result, "FrankenPHP (")
}

func TestRenderTabBar_EmptyCountIgnored(t *testing.T) {
	tabs := []Tab{TabCaddy}
	counts := map[Tab]string{
		TabCaddy: "",
	}
	result := renderTabBar(tabs, TabCaddy, 80, counts)

	assert.NotContains(t, result, "()")
}
