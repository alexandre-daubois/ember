package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var emptyOpts = upstreamRenderOpts{viewTime: time.Now()}

func TestRenderUpstreamTable_Header(t *testing.T) {
	out := stripANSI(renderUpstreamTable(nil, 0, 100, 20, model.SortByUpstreamAddress, emptyOpts))
	assert.Contains(t, out, "Upstream")
	assert.Contains(t, out, "Check")
	assert.Contains(t, out, "LB")
	assert.Contains(t, out, "Health")
	assert.Contains(t, out, "Down")
}

func TestRenderUpstreamTable_SortIndicator(t *testing.T) {
	out := stripANSI(renderUpstreamTable(nil, 0, 100, 20, model.SortByUpstreamHealth, emptyOpts))
	assert.Contains(t, out, "Health ▼")
}

func TestRenderUpstreamTable_Rows(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "10.0.0.1:8080", Handler: "rp", Healthy: true},
		{Address: "10.0.0.2:8080", Handler: "rp", Healthy: false},
	}
	out := stripANSI(renderUpstreamTable(upstreams, 0, 100, 20, model.SortByUpstreamAddress, emptyOpts))
	assert.Contains(t, out, "10.0.0.1:8080")
	assert.Contains(t, out, "10.0.0.2:8080")
	assert.Contains(t, out, "● healthy")
	assert.Contains(t, out, "○ down")
}

func TestRenderUpstreamTable_HealthChanged(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "10.0.0.1:8080", Handler: "rp", Healthy: false, HealthChanged: true},
	}
	out := stripANSI(renderUpstreamTable(upstreams, 0, 100, 20, model.SortByUpstreamAddress, emptyOpts))
	assert.Contains(t, out, "○ down !")
}

func TestRenderUpstreamTable_FillsRequestedHeight(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "10.0.0.1:8080", Handler: "rp", Healthy: true},
	}
	out := renderUpstreamTable(upstreams, 0, 100, 20, model.SortByUpstreamAddress, emptyOpts)
	assert.Equal(t, 20, lipgloss.Height(out),
		"a single row must still fill the requested height with empty padding")

	out = renderUpstreamTable(nil, 0, 100, 20, model.SortByUpstreamAddress, emptyOpts)
	assert.Equal(t, 20, lipgloss.Height(out),
		"empty data must still fill the requested height")
}

func TestRenderUpstreamTable_ViewportClipping(t *testing.T) {
	upstreams := make([]model.UpstreamDerived, 50)
	for i := range upstreams {
		upstreams[i] = model.UpstreamDerived{Address: fmt.Sprintf("10.0.0.%d:8080", i), Handler: "rp", Healthy: true}
	}

	out := renderUpstreamTable(upstreams, 25, 100, 15, model.SortByUpstreamAddress, emptyOpts)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	assert.Less(t, len(lines), 50, "viewport must clip output")
	assert.Contains(t, stripANSI(out), ">", "cursor row must be visible")
}

func TestRenderUpstreamTable_ViewportCursorAtEnd(t *testing.T) {
	upstreams := make([]model.UpstreamDerived, 30)
	for i := range upstreams {
		upstreams[i] = model.UpstreamDerived{Address: fmt.Sprintf("10.0.0.%d:8080", i), Handler: "rp", Healthy: true}
	}

	out := renderUpstreamTable(upstreams, 29, 100, 15, model.SortByUpstreamAddress, emptyOpts)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	assert.Less(t, len(lines), 30, "viewport must clip output")
	assert.Contains(t, stripANSI(out), ">", "cursor row must be visible")
}

func TestRenderUpstreamTable_WithCheckAndLB(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "10.0.0.1:8080", Healthy: true},
	}
	opts := upstreamRenderOpts{
		rpConfigs: []fetcher.ReverseProxyConfig{
			{
				LBPolicy:       "round_robin",
				HealthURI:      "/health",
				HealthInterval: "5s",
				Upstreams: []fetcher.ReverseProxyUpstreamConfig{
					{Address: "10.0.0.1:8080"},
				},
			},
		},
		viewTime: time.Now(),
	}
	out := stripANSI(renderUpstreamTable(upstreams, 0, 120, 20, model.SortByUpstreamAddress, opts))
	assert.Contains(t, out, "/health @5s")
	assert.Contains(t, out, "round_robin")
}

func TestRenderUpstreamTable_WithDownSince(t *testing.T) {
	now := time.Now()
	upstreams := []model.UpstreamDerived{
		{Address: "10.0.0.1:8080", Healthy: false},
	}
	opts := upstreamRenderOpts{
		downSince: map[string]time.Time{
			"10.0.0.1:8080": now.Add(-2 * time.Minute),
		},
		viewTime: now,
	}
	out := stripANSI(renderUpstreamTable(upstreams, 0, 100, 20, model.SortByUpstreamAddress, opts))
	assert.Contains(t, out, "2m")
}

func TestRenderUpstreamTable_HealthyNoDownDuration(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "10.0.0.1:8080", Healthy: true},
	}
	opts := upstreamRenderOpts{
		downSince: map[string]time.Time{},
		viewTime:  time.Now(),
	}
	out := stripANSI(renderUpstreamTable(upstreams, 0, 100, 20, model.SortByUpstreamAddress, opts))
	assert.NotContains(t, out, "0s")
}

func TestFormatDownDuration(t *testing.T) {
	assert.Equal(t, "5s", formatDownDuration(5*time.Second))
	assert.Equal(t, "2m30s", formatDownDuration(2*time.Minute+30*time.Second))
	assert.Equal(t, "1h5m", formatDownDuration(1*time.Hour+5*time.Minute))
}

func TestFormatDownDuration_NegativeClampsToZero(t *testing.T) {
	assert.Equal(t, "0s", formatDownDuration(-5*time.Second),
		"a clock skew must never surface as a negative duration in the UI")
	assert.Equal(t, "0s", formatDownDuration(-1*time.Hour))
}

func TestSortUpstreams_ByAddress(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "z.internal:8080"},
		{Address: "a.internal:8080"},
		{Address: "m.internal:8080"},
	}
	sorted := sortUpstreams(upstreams, model.SortByUpstreamAddress)
	require.Len(t, sorted, 3)
	assert.Equal(t, "a.internal:8080", sorted[0].Address)
	assert.Equal(t, "m.internal:8080", sorted[1].Address)
	assert.Equal(t, "z.internal:8080", sorted[2].Address)
}

func TestSortUpstreams_ByHealth(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "down1", Healthy: false},
		{Address: "up1", Healthy: true},
		{Address: "down2", Healthy: false},
	}
	sorted := sortUpstreams(upstreams, model.SortByUpstreamHealth)
	assert.True(t, sorted[0].Healthy, "healthy should come first")
	assert.False(t, sorted[1].Healthy)
}

func TestSortUpstreams_TiebreakerKeepsDeterministicOrder(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "a:80", Handler: "rp_1", Healthy: true},
		{Address: "a:80", Handler: "rp_0", Healthy: true},
		{Address: "a:80", Handler: "rp_2", Healthy: false},
	}

	sortedAddr := sortUpstreams(upstreams, model.SortByUpstreamAddress)
	require.Len(t, sortedAddr, 3)
	assert.Equal(t, "rp_0", sortedAddr[0].Handler,
		"ties on address must break on handler ascending")
	assert.Equal(t, "rp_1", sortedAddr[1].Handler)
	assert.Equal(t, "rp_2", sortedAddr[2].Handler)

	sortedHealth := sortUpstreams(upstreams, model.SortByUpstreamHealth)
	require.Len(t, sortedHealth, 3)
	assert.True(t, sortedHealth[0].Healthy, "healthy rows must come first")
	assert.True(t, sortedHealth[1].Healthy)
	assert.Equal(t, "rp_0", sortedHealth[0].Handler,
		"healthy ties must break on handler")
	assert.Equal(t, "rp_1", sortedHealth[1].Handler)
}

func TestSortUpstreams_DoesNotMutateOriginal(t *testing.T) {
	upstreams := []model.UpstreamDerived{
		{Address: "z"},
		{Address: "a"},
	}
	sorted := sortUpstreams(upstreams, model.SortByUpstreamAddress)
	assert.Equal(t, "z", upstreams[0].Address)
	assert.Equal(t, "a", sorted[0].Address)
}

func TestBuildUpstreamConfigMap(t *testing.T) {
	configs := []fetcher.ReverseProxyConfig{
		{
			LBPolicy:       "least_conn",
			HealthURI:      "/health",
			HealthInterval: "5s",
			Upstreams: []fetcher.ReverseProxyUpstreamConfig{
				{Address: "10.0.0.1:8080"},
				{Address: "10.0.0.2:8080"},
			},
		},
	}
	m := buildUpstreamConfigMap(configs)
	require.Len(t, m, 2)
	assert.Equal(t, "least_conn", m["10.0.0.1:8080"].lbPolicy)
	assert.Equal(t, "/health", m["10.0.0.1:8080"].healthURI)
	assert.Equal(t, "5s", m["10.0.0.1:8080"].healthInterval)
}

func TestBuildUpstreamConfigMap_Empty(t *testing.T) {
	assert.Nil(t, buildUpstreamConfigMap(nil))
}
