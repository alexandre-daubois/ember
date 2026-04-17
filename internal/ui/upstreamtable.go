package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	colUpstreamCheck  = 14
	colUpstreamHealth = 12
	colUpstreamLB     = 14
	colUpstreamDown   = 10
	colUpstreamFixed  = 1 + colUpstreamCheck + colUpstreamHealth + colUpstreamLB + colUpstreamDown
)

func upstreamAddressWidth(totalWidth int) int {
	w := totalWidth - colUpstreamFixed
	if w < 15 {
		w = 15
	}
	return w
}

type upstreamRenderOpts struct {
	rpConfigs []fetcher.ReverseProxyConfig
	downSince map[string]time.Time
	viewTime  time.Time
}

func renderUpstreamTable(upstreams []model.UpstreamDerived, cursor, width, height int, sortBy model.UpstreamSortField, opts upstreamRenderOpts) string {
	addrW := upstreamAddressWidth(width)
	configMap := buildUpstreamConfigMap(opts.rpConfigs)

	colHead := func(label string, field model.UpstreamSortField, w int, right bool) string {
		if sortBy == field {
			label += " ▼"
		}
		if right {
			return fmt.Sprintf("%*s", w, label)
		}
		return fmt.Sprintf("%-*s", w, label)
	}

	header := fmt.Sprintf(" %-*s%*s%*s%*s%*s",
		addrW, colHead("Upstream", model.SortByUpstreamAddress, addrW, false),
		colUpstreamCheck, "Check",
		colUpstreamLB, "LB",
		colUpstreamHealth, colHead("Health", model.SortByUpstreamHealth, colUpstreamHealth, true),
		colUpstreamDown, "Down",
	)

	headerLine := tableHeaderStyle.Width(width).Render(header)

	var rows []string
	for i, u := range upstreams {
		rows = append(rows, formatUpstreamRow(u, width, addrW, i == cursor, i%2 == 1, configMap, opts))
	}

	bodyHeight := height - lipgloss.Height(headerLine)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	start := 0
	if cursor > bodyHeight-1 {
		start = cursor - bodyHeight + 1
	}
	end := start + bodyHeight
	if end > len(rows) {
		end = len(rows)
		start = end - bodyHeight
		if start < 0 {
			start = 0
		}
	}

	content := strings.Join(rows[start:end], "\n")
	if h := lipgloss.Height(content); h < bodyHeight {
		content += strings.Repeat("\n", bodyHeight-h)
	}
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, content)
}

type upstreamConfigInfo struct {
	lbPolicy       string
	healthURI      string
	healthInterval string
}

// buildUpstreamConfigMap indexes reverse proxy config by dial address.
// If the same address appears in multiple reverse_proxy handlers with different
// settings, later entries overwrite earlier ones — displaying the config of an
// arbitrary matching handler. This is an accepted limitation: the upstream health
// metric typically omits the handler label, so there's no reliable way to
// correlate a metric row with the specific handler that produced it.
func buildUpstreamConfigMap(configs []fetcher.ReverseProxyConfig) map[string]upstreamConfigInfo {
	if len(configs) == 0 {
		return nil
	}
	m := make(map[string]upstreamConfigInfo)
	for _, rp := range configs {
		info := upstreamConfigInfo{
			lbPolicy:       rp.LBPolicy,
			healthURI:      rp.HealthURI,
			healthInterval: rp.HealthInterval,
		}
		for _, u := range rp.Upstreams {
			m[u.Address] = info
		}
	}
	return m
}

func formatUpstreamRow(u model.UpstreamDerived, width, addrW int, selected, zebra bool, configMap map[string]upstreamConfigInfo, opts upstreamRenderOpts) string {
	addr := u.Address
	if len(addr) > addrW-1 {
		addr = addr[:addrW-2] + "…"
	}

	checkStr := "—"
	lbStr := "—"
	if info, ok := configMap[u.Address]; ok {
		if info.healthURI != "" {
			checkStr = info.healthURI
			if info.healthInterval != "" {
				checkStr += " @" + info.healthInterval
			}
		}
		if info.lbPolicy != "" {
			lbStr = info.lbPolicy
		}
	}
	if len(checkStr) > colUpstreamCheck-1 {
		checkStr = checkStr[:colUpstreamCheck-2] + "…"
	}
	if len(lbStr) > colUpstreamLB-1 {
		lbStr = lbStr[:colUpstreamLB-2] + "…"
	}

	var healthStr string
	if u.Healthy {
		healthStr = "● healthy"
	} else {
		healthStr = "○ down"
	}
	if u.HealthChanged {
		healthStr += " !"
	}

	downStr := "—"
	if !u.Healthy {
		if since, ok := opts.downSince[upstreamKey(u)]; ok {
			downStr = formatDownDuration(opts.viewTime.Sub(since))
		}
	}

	prefix := " "
	if selected {
		prefix = ">"
	}

	addrPart := fmt.Sprintf("%s%-*s", prefix, addrW, addr)
	checkPart := fmt.Sprintf("%*s", colUpstreamCheck, checkStr)
	lbPart := fmt.Sprintf("%*s", colUpstreamLB, lbStr)
	healthPart := fmt.Sprintf("%*s", colUpstreamHealth, healthStr)
	downPart := fmt.Sprintf("%*s", colUpstreamDown, downStr)

	if selected {
		row := addrPart + checkPart + lbPart + healthPart + downPart
		return selectedRowStyle.Width(width).Render(row)
	}

	healthStyle := okStyle
	if !u.Healthy {
		healthStyle = dangerStyle
	}
	downStyleFg := lipgloss.NewStyle()
	if !u.Healthy && downStr != "—" {
		downStyleFg = dangerStyle
	}

	baseStyle := lipgloss.NewStyle()
	if zebra {
		baseStyle = zebraStyle
		healthStyle = healthStyle.Background(zebraBg)
		downStyleFg = downStyleFg.Background(zebraBg)
	}

	row := baseStyle.Render(addrPart) +
		baseStyle.Render(checkPart) +
		baseStyle.Render(lbPart) +
		healthStyle.Render(healthPart) +
		downStyleFg.Render(downPart)

	if zebra {
		return zebraStyle.Width(width).Render(row)
	}
	return row
}

func formatDownDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func sortUpstreams(upstreams []model.UpstreamDerived, by model.UpstreamSortField) []model.UpstreamDerived {
	sorted := make([]model.UpstreamDerived, len(upstreams))
	copy(sorted, upstreams)

	// Input comes from map iteration (non-deterministic order), so every
	// sort path needs a deterministic tiebreaker to keep the cursor stable
	// across refreshes.
	slices.SortStableFunc(sorted, func(a, b model.UpstreamDerived) int {
		if by == model.SortByUpstreamHealth {
			ha, hb := 0, 0
			if a.Healthy {
				ha = 1
			}
			if b.Healthy {
				hb = 1
			}
			if c := cmp.Compare(hb, ha); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(a.Address, b.Address); c != 0 {
			return c
		}
		return cmp.Compare(a.Handler, b.Handler)
	})

	return sorted
}

func (a *App) handleUpstreamListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	upstreams := a.filteredUpstreams()
	maxIdx := len(upstreams) - 1
	if maxIdx < 0 {
		maxIdx = 0
	}

	key := msg.String()

	if cmd, ok := a.handleTabSwitch(key); ok {
		return a, cmd
	}

	moveCursor(key, &a.cursor, maxIdx, a.pageSize())

	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "s":
		a.upstreamSortBy = a.upstreamSortBy.Next()
	case "S":
		a.upstreamSortBy = a.upstreamSortBy.Prev()
	case "p":
		a.paused = !a.paused
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "r":
		a.rpConfigs = nil
		return a, a.doFetchRPConfig()
	case "g":
		a.prevMode = a.mode
		a.mode = viewGraph
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}
