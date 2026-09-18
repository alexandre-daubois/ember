package ui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubFeedControl struct {
	starts, stops atomic.Int32
	err           error
}

func (s *stubFeedControl) Start(context.Context) error { s.starts.Add(1); return s.err }
func (s *stubFeedControl) Stop(context.Context) error  { s.stops.Add(1); return s.err }

func feedEntry(msg, id string, topics []string, data string) fetcher.LogEntry {
	return fetcher.LogEntry{
		Timestamp: time.Now(), Level: "debug", Logger: fetcher.MercureLoggerName, Message: msg,
		Mercure: &fetcher.MercureLogFields{UpdateID: id, Type: "book", Topics: topics, Data: data},
	}
}

// A failed start must leave the controller inactive, or the sink watchdog
// would install the sink behind the TUI's back.
func TestMercureFeed_ReenteringLogsTabDropsAStaleFrozenSnapshot(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"a"}, "x"))
	_, cmd := app.Update(key('m'))
	run(app, cmd)
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	require.True(t, app.mercureFrozen)

	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	run(app, cmd)
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	run(app, cmd)
	require.Equal(t, tabLogs, app.activeTab)
	assert.False(t, app.mercureFrozen)
}

// newFeedApp is a hub-only app with the Logs tab wired to a feed.
func newFeedApp(ctl MercureFeedControl) (*App, *model.MercureFeed) {
	feed := model.NewMercureFeed(50)
	app := newBareApp()
	app.logBuffer = model.NewLogBuffer(10)
	app.runtimeLogBuffer = model.NewLogBuffer(10)
	app.config.MercureFeed = feed
	app.config.MercureFeedControl = ctl
	app.Update(fetchMsg{snap: hubSnap(1, 1, 1)})
	return app, feed
}

func key(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// run executes the command a key produced so the resulting message reaches Update.
func run(app *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			run(app, c)
		}
	default:
		app.Update(msg)
	}
}

// selectMercure walks the sidepanel down to the Mercure row and runs the
// resulting commands.
func selectMercure(t *testing.T, app *App, kind logSelKind) {
	t.Helper()
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyTab})
	run(app, cmd)
	require.Equal(t, tabLogs, app.activeTab)
	require.True(t, app.logSidepanelFocused)
	for i := 0; i < 10 && app.logSel.kind != kind; i++ {
		_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyDown})
		run(app, cmd)
	}
	require.Equal(t, kind, app.logSel.kind)
}

func TestSidepanel_MercureRowsOnlyWithAHubAndAFeed(t *testing.T) {
	app := newBareApp()
	app.logBuffer = model.NewLogBuffer(10)
	app.Update(fetchMsg{snap: hubSnap(1, 1, 1)})
	for _, it := range app.sidepanelItems() {
		assert.NotEqual(t, logSelMercure, it.kind, "no feed, no row")
	}

	app.config.MercureFeed = model.NewMercureFeed(10)
	items := app.sidepanelItems()
	labels := make([]string, 0, len(items))
	for _, it := range items {
		labels = append(labels, strings.Repeat("  ", it.indent)+it.label)
	}
	assert.Equal(t, []string{"Runtime", "Access", "Mercure", "  By Topic"}, labels)

	plain := newBareApp()
	plain.logBuffer = model.NewLogBuffer(10)
	plain.config.MercureFeed = model.NewMercureFeed(10)
	for _, it := range plain.sidepanelItems() {
		assert.NotEqual(t, logSelMercure, it.kind, "no hub, no row")
	}
}

func TestMercureFeed_CaptureFollowsTheSidepanelSelection(t *testing.T) {
	ctl := &stubFeedControl{}
	app, _ := newFeedApp(ctl)

	selectMercure(t, app, logSelMercure)
	assert.True(t, app.mercureFeedOn)
	assert.Equal(t, int32(1), ctl.starts.Load())

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	run(app, cmd)
	assert.Equal(t, logSelMercureTopics, app.logSel.kind)
	assert.Equal(t, int32(1), ctl.starts.Load(), "both Mercure rows share one capture")
	assert.Equal(t, int32(0), ctl.stops.Load())

	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyUp})
	run(app, cmd)
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyUp})
	run(app, cmd)
	assert.Equal(t, logSelAccess, app.logSel.kind)
	assert.False(t, app.mercureFeedOn)
	assert.Equal(t, int32(1), ctl.stops.Load())
}

func TestMercureFeed_CaptureStopsWhenLeavingTheLogsTab(t *testing.T) {
	ctl := &stubFeedControl{}
	app, _ := newFeedApp(ctl)
	selectMercure(t, app, logSelMercure)

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyTab})
	run(app, cmd)
	assert.NotEqual(t, tabLogs, app.activeTab)
	assert.False(t, app.mercureFeedOn)
	assert.Equal(t, int32(1), ctl.stops.Load())

	// Polls while elsewhere never touch the sink.
	_, cmd = app.Update(fetchMsg{snap: hubSnap(1, 1, 1)})
	run(app, cmd)
	assert.Equal(t, int32(1), ctl.starts.Load())
}

func TestMercureFeed_MShortcutJumpsToTheMessages(t *testing.T) {
	ctl := &stubFeedControl{}
	app, _ := newFeedApp(ctl)
	require.Equal(t, tabMercure, app.activeTab)

	_, cmd := app.Update(key('m'))
	run(app, cmd)

	assert.Equal(t, tabLogs, app.activeTab)
	assert.Equal(t, logSelMercure, app.logSel.kind)
	assert.False(t, app.logSidepanelFocused, "the table has focus so ↑/↓ browse updates right away")
	assert.True(t, app.mercureFeedOn)
	assert.Equal(t, int32(1), ctl.starts.Load())
	assert.Contains(t, stripANSI(app.View()), "capturing")
}

func TestMercureFeed_MShortcutNeedsAFeed(t *testing.T) {
	app := newBareApp()
	app.Update(fetchMsg{snap: hubSnap(1, 1, 1)})
	app.Update(key('m'))
	assert.Equal(t, tabMercure, app.activeTab)
	assert.NotContains(t, stripANSI(app.View()), "press m")
}

func TestMercureFeed_FailedStartIsNotRetriedUntilReselected(t *testing.T) {
	ctl := &stubFeedControl{err: errors.New("HTTP 403")}
	app, _ := newFeedApp(ctl)
	_, cmd := app.Update(key('m'))
	run(app, cmd)

	assert.True(t, app.mercureFeedFailed)
	assert.False(t, app.mercureFeedOn)
	assert.Contains(t, app.status, "mercure capture: HTTP 403")
	out := stripANSI(app.View())
	assert.Contains(t, out, "capture FAILED")
	assert.Contains(t, out, "refused the DEBUG log sink")

	_, cmd = app.Update(fetchMsg{snap: hubSnap(1, 1, 1)})
	run(app, cmd)
	assert.Equal(t, int32(1), ctl.starts.Load(), "no retry while the row stays selected")

	ctl.err = nil
	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyUp})
	run(app, cmd)
	require.NotEqual(t, logSelMercure, app.logSel.kind)
	assert.False(t, app.mercureFeedFailed)
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyDown})
	run(app, cmd)
	assert.Equal(t, int32(2), ctl.starts.Load())
}

func TestMercureFeed_StdinHasNoControl(t *testing.T) {
	app, _ := newFeedApp(nil)
	assert.Contains(t, stripANSI(app.View()), "m messages")
	_, cmd := app.Update(key('m'))
	assert.Nil(t, cmd)
	assert.Equal(t, logSelMercure, app.logSel.kind)
	out := stripANSI(app.View())
	assert.Contains(t, out, "capture: stdin")
	assert.Contains(t, out, "DEBUG level")
}

func TestMercureMessages_Render(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"https://example.com/books/1", "alt"}, "{\"title\":\"Dune\"}\nline2"))
	feed.Track(feedEntry("Update sent", "u1", []string{"https://example.com/books/1", "alt"}, ""))
	app.width = 200
	_, cmd := app.Update(key('m'))
	run(app, cmd)

	out := stripANSI(app.View())
	assert.Contains(t, out, ">Mercure")
	assert.Regexp(t, `https://example\.com/books/1 \+1\s+book `, out, "the alternate count sits right after a short topic")
	assert.Contains(t, out, `{"title":"Dune"}⏎line2`, "payload line breaks stay on one row")
	assert.Regexp(t, `22 B\s+1\s+\{`, out, "size then recipients then data")
	assert.Contains(t, out, "Enter detail")
}

func TestMercureTopics_HeaderKeepsItsColumnsNextToTheCaptureChip(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"a"}, "x"))
	selectMercure(t, app, logSelMercureTopics)
	out := stripANSI(app.View())
	assert.Contains(t, out, "Topics Updates ▼    Sent  Priv  Avg size  Last", "every label keeps a gap from its neighbour")
	assert.Contains(t, out, "capturing")
}

func TestMercureTopics_RenderGroupsByPattern(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"https://example.com/books/1"}, "x"))
	feed.Track(feedEntry("Update published", "u2", []string{"https://example.com/books/2"}, "xy"))
	feed.Track(feedEntry("Update published", "u3", []string{"demo"}, "x"))
	selectMercure(t, app, logSelMercureTopics)
	app.viewTime = time.Now().Add(5 * time.Second)

	out := stripANSI(app.View())
	assert.Contains(t, out, "Updates ▼")
	assert.Contains(t, out, "https://example.com/books/:id")
	assert.NotContains(t, out, "books/1")
	assert.Regexp(t, `books/:id\s+2\s+2`, out, "two distinct topics, two updates")
	assert.Regexp(t, `demo\s+1\s+1`, out)
	assert.Contains(t, out, "5s ago")
	assert.Contains(t, out, "sort(updates)")
}

func TestMercureMessages_EmptyHints(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	_, cmd := app.Update(key('m'))
	run(app, cmd)
	assert.Contains(t, stripANSI(app.View()), "Waiting for updates")

	feed.Track(feedEntry("Update published", "u1", []string{"a"}, "x"))
	app.filter = "zzz"
	assert.Contains(t, stripANSI(app.View()), "No matching updates (filter: zzz)")
}

func TestMercureMessages_FreezeDetailAndClear(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	_, cmd := app.Update(key('m'))
	run(app, cmd)
	feed.Track(feedEntry("Update published", "u1", []string{"a"}, `{"n":1}`))
	feed.Track(feedEntry("Update published", "u2", []string{"a"}, "second"))

	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.True(t, app.mercureFrozen)
	assert.Equal(t, 1, app.cursor)
	feed.Track(feedEntry("Update published", "u3", []string{"a"}, "third"))
	out := stripANSI(app.View())
	assert.Contains(t, out, "PAUSED · 1 new")
	assert.NotContains(t, out, "third", "a frozen list does not slide")

	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, viewDetail, app.mode)
	out = stripANSI(app.View())
	assert.Contains(t, out, "Mercure › update")
	assert.Contains(t, out, "u1", "the cursor sits on the older row")
	assert.Contains(t, out, `"n": 1`, "JSON payloads are re-indented")
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, viewList, app.mode)

	app.Update(key('f'))
	assert.False(t, app.mercureFrozen)
	assert.Contains(t, stripANSI(app.View()), "third")
	assert.NotContains(t, stripANSI(app.View()), "PAUSED")

	app.Update(key('p'))
	assert.True(t, app.mercureFrozen)
	app.Update(key('p'))
	assert.False(t, app.mercureFrozen)

	app.Update(key('c'))
	assert.Equal(t, 0, feed.Len())
	assert.Equal(t, "mercure updates cleared", app.status)
}

func TestMercureMessages_EnterOnEmptyListStaysInList(t *testing.T) {
	app, _ := newFeedApp(&stubFeedControl{})
	_, cmd := app.Update(key('m'))
	run(app, cmd)
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, viewList, app.mode)
}

func TestMercureMessages_LeftReturnsToTheSidepanelAndResumes(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"a"}, "x"))
	_, cmd := app.Update(key('m'))
	run(app, cmd)
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	require.True(t, app.mercureFrozen)

	app.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.True(t, app.logSidepanelFocused)
	assert.False(t, app.mercureFrozen)
	assert.True(t, app.mercureFeedOn, "focusing the sidepanel keeps the row selected, so the capture stays")
}

func TestMercureMessages_Filter(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"orders"}, "paid"))
	feed.Track(feedEntry("Update published", "u2", []string{"users"}, "hello"))
	_, cmd := app.Update(key('m'))
	run(app, cmd)

	app.Update(key('/'))
	require.Equal(t, viewFilter, app.mode)
	app.Update(key('u'))
	app.Update(key('s'))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	out := stripANSI(app.View())
	assert.Contains(t, out, "users")
	assert.NotContains(t, out, "orders")
	assert.Contains(t, out, "filter: us")
	assert.Equal(t, 1, app.listLen())
}

func TestMercureTopics_SortFilterAndClear(t *testing.T) {
	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"zeta"}, "x"))
	feed.Track(feedEntry("Update published", "u2", []string{"alpha"}, "x"))
	feed.Track(feedEntry("Update published", "u3", []string{"alpha"}, "x"))
	selectMercure(t, app, logSelMercureTopics)
	app.Update(tea.KeyMsg{Type: tea.KeyRight})
	require.False(t, app.logSidepanelFocused)

	assert.Equal(t, "alpha", app.mercureTopicStats()[0].Pattern)
	app.Update(key('s'))
	assert.Equal(t, model.SortByTopicName, app.topicSortBy)
	app.Update(key('S'))
	app.Update(key('S'))
	assert.Equal(t, model.SortByTopicLastSeen, app.topicSortBy)
	assert.Contains(t, stripANSI(app.View()), "sort(last seen)")

	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.False(t, app.mercureFrozen, "an aggregation never freezes")
	assert.Equal(t, 1, app.cursor)

	app.Update(key('p'))
	assert.False(t, app.mercureFrozen)

	app.filter = "zet"
	assert.Equal(t, 1, app.listLen())

	app.Update(key('c'))
	assert.Empty(t, app.mercureTopicStats())
}

func TestRenderMercureUpdatePanel_TruncatesLongPayloads(t *testing.T) {
	u := model.MercureUpdate{
		ID: "urn:uuid:1", Private: true, Recipients: 3,
		Topics: []string{"a", "b"}, Data: strings.Repeat("payload ", 200), DataTruncated: true,
		Time: time.Now(),
	}
	out := stripANSI(renderMercureUpdatePanel(u, 60, 24))
	assert.Contains(t, out, "urn:uuid:1")
	assert.Contains(t, out, "3 subscribers")
	assert.Contains(t, out, "Private   yes")
	assert.Contains(t, out, ", truncated")
	assert.Contains(t, out, "more lines")
	assert.Equal(t, 24, lipgloss.Height(out))
	for _, line := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 60)
	}

	empty := stripANSI(renderMercureUpdatePanel(model.MercureUpdate{Time: time.Now()}, 60, 20))
	assert.Contains(t, empty, "—")
}

func TestMercureDataLines_JSONIsIndentedAndInvalidIsKept(t *testing.T) {
	lines := mercureDataLines(`{"a":{"b":[1,2]}}`, 60)
	assert.Equal(t, "{", lines[0])
	assert.Contains(t, strings.Join(lines, "\n"), `  "a": {`)

	raw := mercureDataLines("line one\x1b[31m\nline two", 60)
	assert.Equal(t, []string{"line one[31m", "line two"}, raw, "the escape byte goes, the line break stays")
}

func TestMercureFeed_NoColorEmitsNoANSI(t *testing.T) {
	prev := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(prev)
	lipgloss.SetColorProfile(termenv.Ascii)

	app, feed := newFeedApp(&stubFeedControl{})
	feed.Track(feedEntry("Update published", "u1", []string{"a"}, "x"))
	_, cmd := app.Update(key('m'))
	run(app, cmd)
	app.Update(tea.KeyMsg{Type: tea.KeyDown})

	out := app.View()
	assert.NotContains(t, out, "\x1b[")
	assert.Contains(t, out, ">Mercure", "the selected sidepanel row keeps its caret")
	assert.Contains(t, out, "● capturing", "the capture state is a glyph and a word, not a colour")
	assert.Contains(t, out, "● PAUSED")
}

func TestSliceViewport(t *testing.T) {
	all := []int{0, 1, 2, 3, 4, 5}
	visible, cur := sliceViewport(all, 4, 3, false)
	assert.Equal(t, []int{0, 1, 2}, visible)
	assert.Equal(t, 0, cur)

	visible, cur = sliceViewport(all, 4, 3, true)
	assert.Equal(t, []int{2, 3, 4}, visible)
	assert.Equal(t, 2, cur)

	visible, cur = sliceViewport(all, 99, 3, true)
	assert.Equal(t, []int{3, 4, 5}, visible)
	assert.Equal(t, 2, cur)

	visible, cur = sliceViewport([]int{}, 0, 3, true)
	assert.Empty(t, visible)
	assert.Equal(t, 0, cur)
}

func TestMercureColumnLayout(t *testing.T) {
	topicW, dataW := mercureColumnLayout(200)
	assert.Equal(t, maxMercureTopic, topicW)
	assert.Equal(t, 200-colMercureFixed-maxMercureTopic, dataW)

	topicW, dataW = mercureColumnLayout(20)
	assert.Equal(t, minMercureTopic, topicW)
	assert.Equal(t, minMercureData, dataW)
}

func TestMercureTopicCell(t *testing.T) {
	assert.Equal(t, "abc       ", mercureTopicCell([]string{"abc"}, 10))
	assert.Equal(t, "abc +2    ", mercureTopicCell([]string{"abc", "x", "y"}, 10), "a short topic keeps its count next to it")
	assert.Equal(t, "abcde… +2 ", mercureTopicCell([]string{"abcdefghij", "x", "y"}, 10), "a long topic gives way to the count")
	assert.Equal(t, "          ", mercureTopicCell(nil, 10))
}

func TestFormatAgo(t *testing.T) {
	assert.Equal(t, "now", formatAgo(500*time.Millisecond))
	assert.Equal(t, "5s ago", formatAgo(5*time.Second))
	assert.Equal(t, "2m ago", formatAgo(150*time.Second))
	assert.Equal(t, "3h ago", formatAgo(3*time.Hour+time.Minute))
}
