package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseJSONTree_SimpleObject(t *testing.T) {
	root, err := parseJSONTree(json.RawMessage(`{"key":"value"}`))
	require.NoError(t, err)

	assert.Equal(t, jsonObject, root.kind)
	assert.Equal(t, 0, root.depth)
	require.Len(t, root.children, 1)

	child := root.children[0]
	assert.Equal(t, "key", child.key)
	assert.Equal(t, jsonString, child.kind)
	assert.Equal(t, "value", child.value)
	assert.Equal(t, 1, child.depth)
	assert.Equal(t, root, child.parent)
}

func TestParseJSONTree_NestedObject(t *testing.T) {
	root, err := parseJSONTree(json.RawMessage(`{"a":{"b":{"c":"deep"}}}`))
	require.NoError(t, err)

	a := root.children[0]
	assert.Equal(t, 1, a.depth)
	b := a.children[0]
	assert.Equal(t, 2, b.depth)
	c := b.children[0]
	assert.Equal(t, 3, c.depth)
	assert.Equal(t, "deep", c.value)
}

func TestParseJSONTree_Array(t *testing.T) {
	root, err := parseJSONTree(json.RawMessage(`{"items":[1,2,3]}`))
	require.NoError(t, err)

	items := root.children[0]
	assert.Equal(t, jsonArray, items.kind)
	require.Len(t, items.children, 3)

	for i, child := range items.children {
		assert.Equal(t, i, child.index)
		assert.Equal(t, jsonNumber, child.kind)
	}
}

func TestParseJSONTree_AllTypes(t *testing.T) {
	raw := `{"s":"hello","n":42,"f":3.14,"b":true,"nil":null}`
	root, err := parseJSONTree(json.RawMessage(raw))
	require.NoError(t, err)

	byKey := make(map[string]*jsonNode)
	for _, c := range root.children {
		byKey[c.key] = c
	}

	assert.Equal(t, jsonString, byKey["s"].kind)
	assert.Equal(t, "hello", byKey["s"].value)

	assert.Equal(t, jsonNumber, byKey["n"].kind)
	assert.Equal(t, "42", byKey["n"].value)

	assert.Equal(t, jsonNumber, byKey["f"].kind)
	assert.Equal(t, "3.14", byKey["f"].value)

	assert.Equal(t, jsonBool, byKey["b"].kind)
	assert.Equal(t, "true", byKey["b"].value)

	assert.Equal(t, jsonNull, byKey["nil"].kind)
	assert.Equal(t, "null", byKey["nil"].value)
}

func TestParseJSONTree_EmptyObjectAndArray(t *testing.T) {
	root, err := parseJSONTree(json.RawMessage(`{"obj":{},"arr":[]}`))
	require.NoError(t, err)

	byKey := make(map[string]*jsonNode)
	for _, c := range root.children {
		byKey[c.key] = c
	}

	assert.Equal(t, jsonObject, byKey["obj"].kind)
	assert.Empty(t, byKey["obj"].children)

	assert.Equal(t, jsonArray, byKey["arr"].kind)
	assert.Empty(t, byKey["arr"].children)
}

func TestParseJSONTree_SortedKeys(t *testing.T) {
	root, err := parseJSONTree(json.RawMessage(`{"z":1,"a":2,"m":3}`))
	require.NoError(t, err)

	require.Len(t, root.children, 3)
	assert.Equal(t, "a", root.children[0].key)
	assert.Equal(t, "m", root.children[1].key)
	assert.Equal(t, "z", root.children[2].key)
}

func TestFlattenVisible_AllCollapsed(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1,"b":2}`))
	root.expanded = false

	visible := flattenVisible(root)
	assert.Len(t, visible, 1)
	assert.Equal(t, root, visible[0])
}

func TestFlattenVisible_RootExpanded(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1,"b":2}`))
	root.expanded = true

	visible := flattenVisible(root)
	assert.Len(t, visible, 3)
	assert.Equal(t, root, visible[0])
	assert.Equal(t, "a", visible[1].key)
	assert.Equal(t, "b", visible[2].key)
}

func TestFlattenVisible_DeepNesting(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":{"b":"c"},"d":"e"}`))
	root.expanded = true
	root.children[0].expanded = true

	visible := flattenVisible(root)
	require.Len(t, visible, 4)
	assert.Equal(t, root, visible[0])
	assert.Equal(t, "a", visible[1].key)
	assert.Equal(t, "b", visible[2].key)
	assert.Equal(t, "d", visible[3].key)
}

func TestFlattenVisible_NilRoot(t *testing.T) {
	visible := flattenVisible(nil)
	assert.Nil(t, visible)
}

func TestToggleExpand(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":{"b":1}}`))
	a := root.children[0]
	assert.False(t, a.expanded)

	a.expanded = true
	assert.True(t, a.expanded)

	a.expanded = false
	assert.False(t, a.expanded)
}

func TestExpandAll_CollapseAll(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":{"b":{"c":1}}}`))

	expandAll(root)
	assert.True(t, root.expanded)
	assert.True(t, root.children[0].expanded)
	assert.True(t, root.children[0].children[0].expanded)

	collapseAll(root)
	assert.False(t, root.expanded)
	assert.False(t, root.children[0].expanded)
	assert.False(t, root.children[0].children[0].expanded)
}

func TestExpandAll_NilRoot(t *testing.T) {
	assert.NotPanics(t, func() { expandAll(nil) })
	assert.NotPanics(t, func() { collapseAll(nil) })
}

func TestRenderConfigTree_ContainsKeys(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"apps":{"http":{}},"admin":{"listen":"localhost:2019"}}`))
	root.expanded = true

	out := renderConfigTree(root, 0, 120, 40, "", false)
	plain := stripANSI(out)

	assert.Contains(t, plain, "apps")
	assert.Contains(t, plain, "admin")
}

func TestRenderConfigTree_EmptyRoot(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{}`))
	root.expanded = true

	assert.NotPanics(t, func() {
		renderConfigTree(root, 0, 120, 40, "", false)
	})
}

func TestRenderConfigTree_NilRoot(t *testing.T) {
	out := renderConfigTree(nil, 0, 120, 40, "", false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "No config loaded")
}

func TestRenderConfigTree_ShowsCollapsedSummary(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"obj":{"a":1,"b":2}}`))
	root.expanded = true

	out := renderConfigTree(root, 0, 120, 40, "", false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "{2 keys}")
}

func TestRenderConfigTree_ShowsArraySummary(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"arr":[1,2,3]}`))
	root.expanded = true

	out := renderConfigTree(root, 0, 120, 40, "", false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "[3 items]")
}

func TestRenderConfigTree_FilterMode(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1}`))
	root.expanded = true

	out := renderConfigTree(root, 0, 120, 40, "test", true)
	plain := stripANSI(out)
	assert.Contains(t, plain, "Search: test")
}

func TestRenderConfigTree_FilterMatches(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"http":1,"tls":2}`))
	root.expanded = true

	out := renderConfigTree(root, 0, 120, 40, "http", false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "1 matches")
}

func TestConfigSearchMatches(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"http":"server","tls":"cert","admin":"api"}`))
	root.expanded = true

	matches := configSearchMatches(root, "http")
	assert.Len(t, matches, 1)

	matches = configSearchMatches(root, "")
	assert.Nil(t, matches)

	matches = configSearchMatches(root, "nonexistent")
	assert.Empty(t, matches)
}

func TestConfigSearchMatches_CaseInsensitive(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"HTTP":"Server"}`))
	root.expanded = true

	matches := configSearchMatches(root, "http")
	assert.Len(t, matches, 1)

	matches = configSearchMatches(root, "server")
	assert.Len(t, matches, 1)
}

func TestParseJSONTree_InvalidJSON(t *testing.T) {
	_, err := parseJSONTree(json.RawMessage(`not json`))
	require.Error(t, err)
}

func TestParseJSONTree_NullJSON(t *testing.T) {
	root, err := parseJSONTree(json.RawMessage(`null`))
	require.NoError(t, err)
	assert.Equal(t, jsonNull, root.kind)
}

func TestHandleConfigKey_Navigation(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1,"b":2,"c":3}`))
	root.expanded = true

	app := &App{
		activeTab:    tabConfig,
		tabs:         []tab{tabCaddy, tabConfig},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:   root,
		configCursor: 0,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, 1, app.configCursor)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, 2, app.configCursor)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	assert.Equal(t, 1, app.configCursor)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, 3, app.configCursor)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, app.configCursor)
}

func TestHandleConfigKey_ShiftTab(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1}`))
	root.expanded = true

	app := &App{
		activeTab:  tabConfig,
		tabs:       []tab{tabCaddy, tabConfig},
		tabStates:  map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot: root,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tabCaddy, app.activeTab)
}

func TestHandleConfigKey_ExpandCollapse(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"obj":{"a":1}}`))
	root.expanded = true

	app := &App{
		activeTab:    tabConfig,
		tabs:         []tab{tabCaddy, tabConfig},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:   root,
		configCursor: 1,
	}

	obj := root.children[0]
	assert.False(t, obj.expanded)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.True(t, obj.expanded)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyLeft})
	assert.False(t, obj.expanded)
}

func TestHandleConfigKey_CollapseJumpsToParent(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"obj":{"a":1}}`))
	root.expanded = true
	root.children[0].expanded = true

	app := &App{
		activeTab:    tabConfig,
		tabs:         []tab{tabCaddy, tabConfig},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:   root,
		configCursor: 2,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyLeft})

	assert.Equal(t, 1, app.configCursor)
}

func TestHandleConfigKey_EscClearsFilter(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1}`))
	root.expanded = true

	app := &App{
		activeTab:        tabConfig,
		tabs:             []tab{tabCaddy, tabConfig},
		tabStates:        map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:       root,
		configFilter:     "test",
		configFilterMode: false,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Empty(t, app.configFilter, "Esc should clear the search filter")
}

func TestHandleConfigKey_EscCancelsFilterMode(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1}`))
	root.expanded = true

	app := &App{
		activeTab:        tabConfig,
		tabs:             []tab{tabCaddy, tabConfig},
		tabStates:        map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:       root,
		configFilter:     "test",
		configFilterMode: true,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.False(t, app.configFilterMode)
	assert.Empty(t, app.configFilter)
}

func TestHandleConfigKey_ExpandOnLeaf(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1}`))
	root.expanded = true

	app := &App{
		activeTab:    tabConfig,
		tabs:         []tab{tabCaddy, tabConfig},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:   root,
		configCursor: 1,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, root.children[0].expanded)
}

func TestHandleConfigKey_ExpandAllCollapseAll(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":{"b":{"c":1}}}`))
	root.expanded = true

	app := &App{
		activeTab:    tabConfig,
		tabs:         []tab{tabCaddy, tabConfig},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:   root,
		configCursor: 0,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.True(t, root.children[0].expanded)
	assert.True(t, root.children[0].children[0].expanded)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	assert.False(t, root.expanded)
	assert.False(t, root.children[0].expanded)
}

func TestHandleConfigKey_Quit(t *testing.T) {
	app := &App{
		activeTab: tabConfig,
		tabs:      []tab{tabCaddy, tabConfig},
		tabStates: map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
	}

	_, cmd := app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd)
}

func TestHandleConfigKey_HelpToggle(t *testing.T) {
	app := &App{
		activeTab: tabConfig,
		tabs:      []tab{tabCaddy, tabConfig},
		tabStates: map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, viewHelp, app.mode)
	assert.Equal(t, viewList, app.prevMode)
}

func TestHandleConfigKey_FilterMode(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"http":1,"tls":2}`))
	root.expanded = true

	app := &App{
		activeTab:  tabConfig,
		tabs:       []tab{tabCaddy, tabConfig},
		tabStates:  map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot: root,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.True(t, app.configFilterMode)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	assert.Equal(t, "h", app.configFilter)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Empty(t, app.configFilter)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, app.configFilterMode)
	assert.Equal(t, "t", app.configFilter)
}

func TestHandleConfigFilterKey_AcceptsNonASCII(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"clé":"valeur"}`))
	root.expanded = true

	app := &App{
		activeTab:        tabConfig,
		tabs:             []tab{tabCaddy, tabConfig},
		tabStates:        map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:       root,
		configFilterMode: true,
	}

	app.handleConfigFilterKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'é'}})
	assert.Equal(t, "é", app.configFilter)

	app.handleConfigFilterKey(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Empty(t, app.configFilter)
}

func TestHandleConfigKey_SearchNavigation(t *testing.T) {
	// Keys sorted: abc, abx, def -> indices 1, 2, 3 after root at 0
	root, _ := parseJSONTree(json.RawMessage(`{"abc":1,"def":2,"abx":3}`))
	root.expanded = true

	app := &App{
		activeTab:    tabConfig,
		tabs:         []tab{tabCaddy, tabConfig},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:   root,
		configFilter: "ab",
		configCursor: 0,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	assert.Equal(t, 1, app.configCursor, "first match: abc at index 1")

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	assert.Equal(t, 2, app.configCursor, "second match: abx at index 2")

	// Wrap around to first match
	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	assert.Equal(t, 1, app.configCursor, "wraps to first match")

	app.configCursor = 2
	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	assert.Equal(t, 1, app.configCursor, "reverse: abc at index 1")
}

func TestHandleConfigKey_PgUpPgDown(t *testing.T) {
	root, _ := parseJSONTree(json.RawMessage(`{"a":1,"b":2,"c":3,"d":4,"e":5}`))
	root.expanded = true

	app := &App{
		activeTab:    tabConfig,
		tabs:         []tab{tabCaddy, tabConfig},
		tabStates:    map[tab]*tabState{tabCaddy: {}, tabConfig: {}},
		configRoot:   root,
		configCursor: 0,
		height:       20,
	}

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Positive(t, app.configCursor)

	app.handleConfigListKey(tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Equal(t, 0, app.configCursor)
}

func TestParseJSONTree_LargeConfig(t *testing.T) {
	raw := `{
		"admin":{"listen":"localhost:2019","enforce_origin":false},
		"apps":{
			"http":{
				"servers":{
					"main":{
						"listen":[":443"],
						"routes":[
							{"match":[{"host":["example.com"]}],"handle":[{"handler":"static_response","body":"Hello"}]},
							{"match":[{"host":["api.example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"localhost:8080"}]}]}
						]
					}
				},
				"metrics":{}
			},
			"tls":{"automation":{"policies":[{"issuers":[{"module":"acme"}]}]}}
		}
	}`

	root, err := parseJSONTree(json.RawMessage(raw))
	require.NoError(t, err)
	assert.Equal(t, jsonObject, root.kind)
	assert.NotEmpty(t, root.children)

	expandAll(root)
	visible := flattenVisible(root)
	assert.Greater(t, len(visible), 10)

	assert.NotPanics(t, func() {
		renderConfigTree(root, 0, 120, 40, "", false)
	})
}

func TestRenderConfigTree_TruncatesLongStyledValues(t *testing.T) {
	longVal := `"` + strings.Repeat("x", 200) + `"`
	root, err := parseJSONTree(json.RawMessage(`{"key":` + longVal + `}`))
	require.NoError(t, err)
	root.expanded = true

	out := renderConfigTree(root, 0, 60, 40, "", false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "…")
}

func TestRenderConfigTree_TruncatesLongPlainValues(t *testing.T) {
	longVal := `"` + strings.Repeat("x", 200) + `"`
	root, err := parseJSONTree(json.RawMessage(`{"key":` + longVal + `}`))
	require.NoError(t, err)
	root.expanded = true

	out := renderConfigTree(root, 1, 60, 40, "", false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "…")
}

func TestRenderConfigTree_TruncatesUTF8Safely(t *testing.T) {
	longVal := `"` + strings.Repeat("é", 200) + `"`
	root, err := parseJSONTree(json.RawMessage(`{"key":` + longVal + `}`))
	require.NoError(t, err)
	root.expanded = true

	out := renderConfigTree(root, 0, 60, 40, "", false)
	plain := stripANSI(out)
	assert.Contains(t, plain, "…")
	assert.True(t, utf8.ValidString(plain))
}
