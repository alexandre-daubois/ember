package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type jsonNodeKind int

const (
	jsonObject jsonNodeKind = iota
	jsonArray
	jsonString
	jsonNumber
	jsonBool
	jsonNull
)

type jsonNode struct {
	key      string
	index    int
	kind     jsonNodeKind
	value    string
	children []*jsonNode
	expanded bool
	depth    int
	parent   *jsonNode
}

func parseJSONTree(raw json.RawMessage) (*jsonNode, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	root := buildNode("", -1, v, 0, nil)
	return root, nil
}

func buildNode(key string, index int, v any, depth int, parent *jsonNode) *jsonNode {
	n := &jsonNode{
		key:    key,
		index:  index,
		depth:  depth,
		parent: parent,
	}

	switch val := v.(type) {
	case map[string]any:
		n.kind = jsonObject
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := buildNode(k, -1, val[k], depth+1, n)
			n.children = append(n.children, child)
		}
	case []any:
		n.kind = jsonArray
		for i, item := range val {
			child := buildNode("", i, item, depth+1, n)
			n.children = append(n.children, child)
		}
	case string:
		n.kind = jsonString
		n.value = val
	case float64:
		n.kind = jsonNumber
		if val == float64(int64(val)) {
			n.value = fmt.Sprintf("%d", int64(val))
		} else {
			n.value = fmt.Sprintf("%g", val)
		}
	case bool:
		n.kind = jsonBool
		n.value = fmt.Sprintf("%t", val)
	default:
		n.kind = jsonNull
		n.value = "null"
	}

	return n
}

func flattenVisible(root *jsonNode) []*jsonNode {
	if root == nil {
		return nil
	}
	var result []*jsonNode
	var walk func(n *jsonNode)
	walk = func(n *jsonNode) {
		result = append(result, n)
		if n.expanded {
			for _, c := range n.children {
				walk(c)
			}
		}
	}
	walk(root)
	return result
}

func expandAll(n *jsonNode) {
	if n == nil {
		return
	}
	if len(n.children) > 0 {
		n.expanded = true
	}
	for _, c := range n.children {
		expandAll(c)
	}
}

func collapseAll(n *jsonNode) {
	if n == nil {
		return
	}
	n.expanded = false
	for _, c := range n.children {
		collapseAll(c)
	}
}

func configSearchMatches(root *jsonNode, query string) []int {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	visible := flattenVisible(root)
	var matches []int
	for i, n := range visible {
		if strings.Contains(strings.ToLower(n.key), q) ||
			strings.Contains(strings.ToLower(n.value), q) {
			matches = append(matches, i)
		}
	}
	return matches
}

func renderConfigTree(root *jsonNode, cursor, width, height int, filter string, filterMode bool) string {
	if root == nil {
		return greyStyle.Render(" No config loaded")
	}

	visible := flattenVisible(root)
	if len(visible) == 0 {
		return greyStyle.Render(" Empty config")
	}

	start := 0
	if cursor > height-1 {
		start = cursor - height + 1
	}
	end := start + height
	if end > len(visible) {
		end = len(visible)
		start = end - height
		if start < 0 {
			start = 0
		}
	}

	matches := configSearchMatches(root, filter)
	matchSet := make(map[int]struct{}, len(matches))
	for _, m := range matches {
		matchSet[m] = struct{}{}
	}

	var lines []string
	for i := start; i < end; i++ {
		n := visible[i]
		selected := i == cursor
		_, matched := matchSet[i]
		line := renderConfigLine(n, width, selected, matched)

		if selected {
			line = selectedRowStyle.Width(width).Render(line)
		} else if matched {
			line = lipgloss.NewStyle().
				Background(lipgloss.AdaptiveColor{Light: "#FFE0B2", Dark: "#3D2800"}).
				Width(width).
				Render(line)
		}

		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")

	if filterMode {
		content += "\n" + helpStyle.Render(fmt.Sprintf(" Search: %s█", filter))
	} else if filter != "" {
		matchInfo := fmt.Sprintf(" Search: %s  (%d matches)", filter, len(matches))
		content += "\n" + helpStyle.Render(matchInfo)
	}

	return content
}

func (a *App) handleConfigListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.configFilterMode {
		return a.handleConfigFilterKey(msg)
	}

	visible := flattenVisible(a.configRoot)
	maxIdx := len(visible) - 1
	if maxIdx < 0 {
		maxIdx = 0
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "tab":
		a.nextTab()
		return a, a.switchTabCmd()
	case "shift+tab":
		a.prevTab()
		return a, a.switchTabCmd()
	case "1":
		if len(a.tabs) > 0 {
			a.switchTab(a.tabs[0])
		}
		return a, a.switchTabCmd()
	case "2":
		if len(a.tabs) > 1 {
			a.switchTab(a.tabs[1])
		}
		return a, a.switchTabCmd()
	case "3":
		if len(a.tabs) > 2 {
			a.switchTab(a.tabs[2])
		}
		return a, a.switchTabCmd()
	case "4":
		if len(a.tabs) > 3 {
			a.switchTab(a.tabs[3])
		}
		return a, a.switchTabCmd()
	case "up", "k":
		if a.configCursor > 0 {
			a.configCursor--
		}
	case "down", "j":
		if a.configCursor < maxIdx {
			a.configCursor++
		}
	case "home":
		a.configCursor = 0
	case "end":
		a.configCursor = maxIdx
	case "pgup":
		a.configCursor -= a.configPageSize()
		if a.configCursor < 0 {
			a.configCursor = 0
		}
	case "pgdown":
		a.configCursor += a.configPageSize()
		if a.configCursor > maxIdx {
			a.configCursor = maxIdx
		}
	case "enter", "right", "l":
		if a.configCursor < len(visible) {
			n := visible[a.configCursor]
			if len(n.children) > 0 && !n.expanded {
				n.expanded = true
			}
		}
	case "left", "h":
		if a.configCursor < len(visible) {
			n := visible[a.configCursor]
			if n.expanded && len(n.children) > 0 {
				n.expanded = false
			} else if n.parent != nil {
				for i, v := range visible {
					if v == n.parent {
						a.configCursor = i
						break
					}
				}
			}
		}
	case "e":
		expandAll(a.configRoot)
	case "E":
		collapseAll(a.configRoot)
		a.configCursor = 0
	case "r":
		return a, a.doFetchConfig()
	case "/":
		a.configFilterMode = true
		a.configFilter = ""
	case "n":
		a.jumpToNextMatch(1)
	case "N":
		a.jumpToNextMatch(-1)
	case "esc":
		if a.configFilter != "" {
			a.configFilter = ""
		}
	case "p":
		a.paused = !a.paused
	case "g":
		a.prevMode = a.mode
		a.mode = viewGraph
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}

func (a *App) handleConfigFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.configFilterMode = false
		a.configFilter = ""
	case "enter":
		a.configFilterMode = false
		a.jumpToNextMatch(1)
	case "backspace":
		if len(a.configFilter) > 0 {
			_, size := utf8.DecodeLastRuneInString(a.configFilter)
			a.configFilter = a.configFilter[:len(a.configFilter)-size]
		}
	default:
		if utf8.RuneCountInString(msg.String()) == 1 {
			a.configFilter += msg.String()
		}
	}
	return a, nil
}

func (a *App) jumpToNextMatch(direction int) {
	matches := configSearchMatches(a.configRoot, a.configFilter)
	if len(matches) == 0 {
		return
	}

	if direction > 0 {
		for _, m := range matches {
			if m > a.configCursor {
				a.configCursor = m
				return
			}
		}
		a.configCursor = matches[0]
	} else {
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i] < a.configCursor {
				a.configCursor = matches[i]
				return
			}
		}
		a.configCursor = matches[len(matches)-1]
	}
}

func (a *App) configPageSize() int {
	ps := a.height - 10
	if ps < 1 {
		ps = 1
	}
	return ps
}

func renderConfigLine(n *jsonNode, width int, selected, matched bool) string {
	prefix := " "
	if selected {
		prefix = ">"
	} else if matched {
		prefix = "*"
	}

	indent := strings.Repeat("  ", n.depth)

	var icon string
	if len(n.children) > 0 {
		if n.expanded {
			icon = "▼ "
		} else {
			icon = "▶ "
		}
	} else {
		icon = "  "
	}

	plain := selected || matched

	var keyPart string
	if n.key != "" {
		if plain {
			keyPart = n.key + ": "
		} else {
			keyPart = titleStyle.Render(n.key) + separatorStyle.Render(": ")
		}
	} else if n.index >= 0 {
		idx := fmt.Sprintf("[%d]", n.index)
		if plain {
			keyPart = idx + ": "
		} else {
			keyPart = greyStyle.Render(idx) + separatorStyle.Render(": ")
		}
	}

	valuePart := configValueString(n, width, indent, keyPart, plain)

	return prefix + indent + icon + keyPart + valuePart
}

func configValueString(n *jsonNode, width int, indent, keyPart string, plain bool) string {
	switch n.kind {
	case jsonObject:
		if n.expanded {
			if plain {
				return "{"
			}
			return greyStyle.Render("{")
		}
		s := fmt.Sprintf("{%d keys}", len(n.children))
		if plain {
			return s
		}
		return greyStyle.Render(s)
	case jsonArray:
		if n.expanded {
			if plain {
				return "["
			}
			return greyStyle.Render("[")
		}
		s := fmt.Sprintf("[%d items]", len(n.children))
		if plain {
			return s
		}
		return greyStyle.Render(s)
	case jsonString:
		displayed := n.value
		runes := []rune(displayed)
		maxLen := width - len(indent) - 2 - lipgloss.Width(keyPart) - 4
		if maxLen > 0 && len(runes) > maxLen {
			displayed = string(runes[:maxLen]) + "…"
		}
		s := fmt.Sprintf("%q", displayed)
		if plain {
			return s
		}
		return lipgloss.NewStyle().Foreground(amber).Render(s)
	case jsonNumber:
		return n.value
	case jsonBool:
		if plain {
			return n.value
		}
		return warnStyle.Render(n.value)
	case jsonNull:
		if plain {
			return "null"
		}
		return greyStyle.Render("null")
	}
	return ""
}
