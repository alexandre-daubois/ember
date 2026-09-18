package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type mercureFeedMsg struct {
	on  bool
	err error
}

const (
	colMercureTime  = 13
	colMercureType  = 12
	colMercurePriv  = 6
	colMercureSize  = 8
	colMercureSent  = 6
	colMercureFixed = 1 + colMercureTime + colMercureType + colMercurePriv + colMercureSize + colMercureSent + 2
	minMercureTopic = 12
	maxMercureTopic = 48
	minMercureData  = 10

	colTopicDistinct = 8
	colTopicCount    = 10
	colTopicSent     = 8
	colTopicPriv     = 6
	colTopicAvg      = 10
	colTopicLastSeen = 8
	colTopicFixed    = 1 + colTopicDistinct + colTopicCount + colTopicSent + colTopicPriv + colTopicAvg + colTopicLastSeen + 2
)

func (a *App) mercureFeedWanted() bool {
	return a.activeTab == tabLogs && a.isMercureView()
}

// A failed start is not retried until the view is left and entered again, so
// a hub that refuses the sink is not hammered on every poll.
func (a *App) mercureFeedCmd() tea.Cmd {
	ctl := a.config.MercureFeedControl
	if ctl == nil {
		return nil
	}
	if !a.mercureFeedWanted() {
		a.mercureFeedFailed = false
	}
	want := a.mercureFeedWanted() && !a.mercureFeedFailed
	if want == a.mercureFeedOn {
		return nil
	}
	a.mercureFeedOn = want
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), globalFetchTimeout)
		defer cancel()
		if want {
			return mercureFeedMsg{on: true, err: ctl.Start(ctx)}
		}
		return mercureFeedMsg{on: false, err: ctl.Stop(ctx)}
	}
}

func (a *App) handleMercureFeedMsg(msg mercureFeedMsg) {
	if msg.err == nil {
		return
	}
	a.status = "⚠ mercure capture: " + msg.err.Error()
	if msg.on {
		a.mercureFeedOn = false
		a.mercureFeedFailed = true
	}
}

func (a *App) mercureCaptureStatus() string {
	switch {
	case a.config.MercureFeedControl == nil:
		return greyStyle.Render("○ capture: stdin")
	case a.mercureFeedFailed:
		return warnStyle.Render("○ capture FAILED")
	case a.mercureFeedOn:
		return greyStyle.Render("● capturing")
	default:
		return greyStyle.Render("○ capture off")
	}
}

func (a *App) jumpToMercureMessages() tea.Cmd {
	a.switchTab(tabLogs)
	a.logSel = logSel{kind: logSelMercure}
	a.logSidepanelFocused = false
	return a.switchTabCmd()
}

func (a *App) mercureUpdates() []model.MercureUpdate {
	if a.mercureFrozen {
		var out []model.MercureUpdate
		for _, u := range a.mercureSnapshot {
			if u.Matches(a.filter) {
				out = append(out, u)
			}
		}
		return out
	}
	if a.config.MercureFeed == nil {
		return nil
	}
	return a.config.MercureFeed.Snapshot(a.filter, a.pageSize())
}

func (a *App) mercureTopicStats() []model.TopicStat {
	if a.config.MercureFeed == nil {
		return nil
	}
	stats := a.config.MercureFeed.Topics()
	if a.filter != "" {
		needle := strings.ToLower(a.filter)
		kept := stats[:0]
		for _, s := range stats {
			if strings.Contains(strings.ToLower(s.Pattern), needle) {
				kept = append(kept, s)
			}
		}
		stats = kept
	}
	model.SortTopics(stats, a.topicSortBy)
	return stats
}

func (a *App) mercureListLen() int {
	switch a.logSel.kind {
	case logSelMercure:
		return len(a.mercureUpdates())
	case logSelMercureTopics:
		return len(a.mercureTopicStats())
	default:
		return 0
	}
}

func (a *App) mercureEmptyHint() string {
	switch {
	case a.filter != "" && a.config.MercureFeed != nil && a.config.MercureFeed.Len() > 0:
		return "No matching updates (filter: " + a.filter + ")"
	case a.mercureFeedFailed:
		return "The hub refused the DEBUG log sink (see the status line). Select another row and come back to retry."
	case a.config.MercureFeedControl == nil:
		return "Waiting for hub updates on stdin: they need the Caddy logger http.handlers.mercure at DEBUG level."
	default:
		return "Waiting for updates... the hub logs each update it dispatches while this row is selected."
	}
}

func (a *App) mercureHeaderStatus() string {
	var parts []string
	f := a.config.MercureFeed
	if a.logSel.kind == logSelMercure && f != nil {
		if n := f.Dropped(); n > 0 {
			parts = append(parts, warnStyle.Render(fmt.Sprintf("dropped: %d", n)))
		}
	}
	if a.filter != "" {
		parts = append(parts, helpStyle.Render("filter: "+a.filter))
	}
	parts = append(parts, a.mercureCaptureStatus())
	if a.mercureFrozen && f != nil {
		label := "● PAUSED"
		if fresh := f.WriteCount() - a.mercureFrozenAt; fresh > 0 {
			label += fmt.Sprintf(" · %d new", fresh)
		}
		parts = append(parts, pausedBadgeStyle.Render(label))
	}
	return strings.Join(parts, "  ")
}

func (a *App) renderMercureMessages(width, height int) string {
	all := a.mercureUpdates()
	bodyHeight := max(height-logHeaderHeight, 1)
	visible, localCursor := sliceViewport(all, a.cursor, bodyHeight, a.mercureFrozen)
	if a.logSidepanelFocused {
		localCursor = -1
	}

	// renderLogHeader truncates the labels under the right-hand chips.
	status := a.mercureHeaderStatus()
	topicW, dataW := mercureColumnLayout(width - lipgloss.Width(status) - 2)
	labels := fmt.Sprintf(" %-*s%-*s%-*s%-*s%*s%*s  %-*s",
		colMercureTime, "Time", topicW, "Topic", colMercureType, "Type", colMercurePriv, "Priv",
		colMercureSize, "Size", colMercureSent, "Sent", dataW, "Data")
	headerLine := renderLogHeader(labels, status, width)

	if len(visible) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, headerLine, greyStyle.Render(" "+a.mercureEmptyHint()))
	}
	rows := make([]string, 0, len(visible))
	for i, u := range visible {
		rows = append(rows, formatMercureRow(u, width, topicW, dataW, i == localCursor))
	}
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, strings.Join(rows, "\n"))
}

func mercureColumnLayout(width int) (topicW, dataW int) {
	flex := max(width-colMercureFixed, minMercureTopic+minMercureData)
	topicW = min(max(flex/2, minMercureTopic), maxMercureTopic)
	return topicW, flex - topicW
}

// The +n count survives truncation, and the last cell stays blank so a
// full-width topic never touches the Type column.
func mercureTopicCell(topics []string, width int) string {
	width--
	if len(topics) == 0 {
		return fitCellLeft("", width) + " "
	}
	cell := topics[0]
	if len(topics) > 1 {
		suffix := fmt.Sprintf(" +%d", len(topics)-1)
		if runewidth.StringWidth(cell)+len(suffix) > width {
			return fitCellLeft(cell, width-len(suffix)) + suffix + " "
		}
		cell += suffix
	}
	return fitCellLeft(cell, width) + " "
}

// A payload's line breaks would wrap the table row.
var lineBreaks = strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\r", "⏎")

func formatMercureRow(u model.MercureUpdate, width, topicW, dataW int, selected bool) string {
	prefix := " "
	if selected {
		prefix = ">"
	}
	priv := "-"
	if u.Private {
		priv = "yes"
	}
	typ := u.Type
	if typ == "" {
		typ = "-"
	}
	row := prefix + fitCellLeft(u.Time.Local().Format("15:04:05.000"), colMercureTime) +
		mercureTopicCell(u.Topics, topicW) +
		fitCellLeft(typ, colMercureType) +
		fitCellLeft(priv, colMercurePriv) +
		fitCellRight(formatBytes(int64(len(u.Data))), colMercureSize) +
		fitCellRight(fmt.Sprintf("%d", u.Recipients), colMercureSent) + "  " +
		fitCellLeft(lineBreaks.Replace(u.Data), dataW)
	if selected {
		return selectedRowStyle.Width(width).Render(row)
	}
	return row
}

func (a *App) renderMercureTopics(width, height int) string {
	stats := a.mercureTopicStats()
	bodyHeight := max(height-logHeaderHeight, 1)
	visible, localCursor := sliceViewport(stats, a.cursor, bodyHeight, true)
	if a.logSidepanelFocused {
		localCursor = -1
	}

	status := a.mercureHeaderStatus()
	patternW := max(width-colTopicFixed-lipgloss.Width(status)-2, minMercureTopic)
	mark := func(label string, field model.TopicSortField) string {
		if a.topicSortBy == field {
			return label + " ▼"
		}
		return label
	}
	labels := fmt.Sprintf(" %-*s%*s%*s%*s%*s%*s  %-*s",
		patternW, mark("Topic pattern", model.SortByTopicName),
		colTopicDistinct, "Topics",
		colTopicCount, mark("Updates", model.SortByTopicCount),
		colTopicSent, mark("Sent", model.SortByTopicRecipients),
		colTopicPriv, "Priv",
		colTopicAvg, "Avg size",
		colTopicLastSeen, mark("Last", model.SortByTopicLastSeen))
	headerLine := renderLogHeader(labels, status, width)

	if len(visible) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, headerLine, greyStyle.Render(" "+a.mercureEmptyHint()))
	}
	rows := make([]string, 0, len(visible))
	for i, s := range visible {
		selected := i == localCursor
		prefix := " "
		if selected {
			prefix = ">"
		}
		row := prefix + highlightPattern(fitCellLeft(s.Pattern, patternW), selected) +
			fitCellRight(formatNumber(int64(s.Topics)), colTopicDistinct) +
			fitCellRight(formatNumber(int64(s.Count)), colTopicCount) +
			fitCellRight(formatNumber(int64(s.Recipients)), colTopicSent) +
			fitCellRight(formatNumber(int64(s.Private)), colTopicPriv) +
			fitCellRight(formatBytes(s.AvgBytes()), colTopicAvg) + "  " +
			fitCellLeft(formatAgo(a.viewTime.Sub(s.LastSeen)), colTopicLastSeen)
		if selected {
			row = selectedRowStyle.Width(width).Render(row)
		}
		rows = append(rows, row)
	}
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, strings.Join(rows, "\n"))
}

func formatAgo(d time.Duration) string {
	switch {
	case d < time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// A live (unfrozen) list pins the newest row at the top and ignores the cursor.
func sliceViewport[T any](all []T, cursor, bodyHeight int, scrollable bool) ([]T, int) {
	if bodyHeight <= 0 || len(all) == 0 {
		return all, 0
	}
	if !scrollable {
		return all[:min(len(all), bodyHeight)], 0
	}
	cursor = min(max(cursor, 0), len(all)-1)
	offset := max(cursor-bodyHeight+1, 0)
	return all[offset:min(offset+bodyHeight, len(all))], cursor - offset
}

func (a *App) freezeMercure() {
	f := a.config.MercureFeed
	if f == nil || a.mercureFrozen {
		return
	}
	a.mercureSnapshot = f.Snapshot("", 0)
	a.mercureFrozenAt = f.WriteCount()
	a.mercureFrozen = true
	a.cursor = 0
}

func (a *App) resumeMercure() {
	a.mercureFrozen = false
	a.mercureSnapshot = nil
	a.mercureFrozenAt = 0
	a.cursor = 0
}

// The topic list is an aggregation and never freezes.
func (a *App) handleMercureLogsKey(key string) tea.Cmd {
	messages := a.logSel.kind == logSelMercure
	switch key {
	case "q", "ctrl+c":
		return tea.Quit
	case "left", "h":
		a.logSidepanelFocused = true
		a.resumeMercure()
		return nil
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
		return nil
	case "f", "home":
		if messages && a.mercureFrozen {
			a.resumeMercure()
			return nil
		}
	case "up", "down", "pgup", "pgdown", "end", "j", "k":
		if messages && !a.mercureFrozen && a.config.MercureFeed != nil && a.config.MercureFeed.Len() > 0 {
			a.freezeMercure()
		}
	}
	moveCursor(key, &a.cursor, max(a.mercureListLen()-1, 0), a.pageSize())

	switch key {
	case "enter":
		// The panel must describe one fixed row, not whatever slides under the cursor.
		if messages && a.mercureListLen() > 0 {
			a.freezeMercure()
			a.mode = viewDetail
		}
	case "p":
		switch {
		case !messages:
		case a.mercureFrozen:
			a.resumeMercure()
		default:
			a.freezeMercure()
		}
	case "s":
		if !messages {
			a.topicSortBy = a.topicSortBy.Next()
		}
	case "S":
		if !messages {
			a.topicSortBy = a.topicSortBy.Prev()
		}
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "c":
		if f := a.config.MercureFeed; f != nil {
			f.Clear()
			a.resumeMercure()
			a.status = "mercure updates cleared"
		}
	}
	return nil
}

func (a *App) mercureLogsHelpBindings() []binding {
	bindings := []binding{{"↑/↓", "navigate"}, {"←/→", "panel"}, {"/", "filter"}}
	if a.logSel.kind == logSelMercure {
		pauseLabel := "pause"
		if a.mercureFrozen {
			pauseLabel = "resume"
		}
		bindings = append(bindings, binding{"Enter", "detail"}, binding{"p", pauseLabel})
		if a.mercureFrozen {
			bindings = append(bindings, binding{"f", "follow"})
		}
	} else {
		bindings = append(bindings, binding{"s/S", "sort(" + a.topicSortBy.String() + ")"})
	}
	return append(bindings, binding{"c", "clear"}, binding{"Tab/S-Tab", "switch"}, binding{"q", "quit"})
}

func renderMercureUpdatePanel(u model.MercureUpdate, width, height int) string {
	inner := max(width-4, 10)

	lines := []string{
		greyStyle.Render("Mercure › ") + titleStyle.Render("update"),
		"",
		sectionHeader("Update", inner),
		detailKV("ID", sanitizeControl(u.ID)),
		detailKV("Time", u.Time.Local().Format("15:04:05.000")),
		detailKV("Type", sanitizeControl(orDash(u.Type))),
		detailKV("Private", yesNo(u.Private)),
		detailKV("Sent to", fmt.Sprintf("%d subscribers", u.Recipients)),
		"",
		sectionHeader("Topics", inner),
	}
	for _, t := range u.Topics {
		lines = append(lines, "  "+truncateURI(sanitizeControl(t), inner-2))
	}
	dataTitle := fmt.Sprintf("Data (%s)", formatBytes(int64(len(u.Data))))
	if u.DataTruncated {
		dataTitle += ", truncated"
	}
	lines = append(lines, "", sectionHeader(dataTitle, inner))

	available := height - 2 - len(lines)
	switch dataLines := mercureDataLines(u.Data, inner-2); {
	case len(dataLines) == 0:
		lines = append(lines, greyStyle.Render("  —"))
	case len(dataLines) > available && available > 1:
		for _, l := range dataLines[:available-1] {
			lines = append(lines, "  "+l)
		}
		lines = append(lines, greyStyle.Render(fmt.Sprintf("  … %d more lines", len(dataLines)-available+1)))
	default:
		for _, l := range dataLines {
			lines = append(lines, "  "+l)
		}
	}

	return boxStyle.Width(width - 2).Render(fitPanelContent(strings.Join(lines, "\n"), height))
}

// Lines are split before sanitizeControl runs, which eats line breaks along
// with the control bytes.
func mercureDataLines(data string, width int) []string {
	if data == "" {
		return nil
	}
	if json.Valid([]byte(data)) {
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(data), "", "  "); err == nil {
			data = buf.String()
		}
	}
	wrap := lipgloss.NewStyle().Width(max(width, 1))
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		for _, l := range strings.Split(wrap.Render(sanitizeControl(line)), "\n") {
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
