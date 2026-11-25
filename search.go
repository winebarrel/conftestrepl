package main

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// textPos is a cursor position in the editor: a line index and a rune column in that line.
type textPos struct {
	row, col int
}

func newSearchInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "Search: "
	return ti
}

// findText returns the first match of query at or after from, wrapping around to the top
// of the text. The search is case-insensitive unless query contains an upper-case letter
// (smartcase). Matches never span lines.
func findText(text, query string, from textPos) (textPos, bool) {
	q := []rune(query)
	if len(q) == 0 {
		return textPos{}, false
	}

	fold := !strings.ContainsFunc(query, unicode.IsUpper)
	if fold {
		q = []rune(strings.ToLower(query))
	}

	lines := strings.Split(text, "\n")
	n := len(lines)
	from.row = min(max(from.row, 0), n-1)

	// Visit the line of from twice: first its part from from.col, and at the end,
	// after wrapping around, its part before from.col.
	for i := 0; i <= n; i++ {
		row := (from.row + i) % n
		line := []rune(lines[row])
		if fold {
			for j, r := range line {
				line[j] = unicode.ToLower(r)
			}
		}

		start, end := 0, len(line)
		switch i {
		case 0:
			start = from.col
		case n:
			end = from.col + len(q) - 1
		}

		for col := max(start, 0); col+len(q) <= min(end, len(line)); col++ {
			if runesHavePrefix(line[col:], q) {
				return textPos{row: row, col: col}, true
			}
		}
	}

	return textPos{}, false
}

func runesHavePrefix(s, prefix []rune) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i, r := range prefix {
		if s[i] != r {
			return false
		}
	}
	return true
}

// editorPos returns the cursor position of the editor.
func (m model) editorPos() textPos {
	return textPos{row: m.editor.Line(), col: m.editor.LineInfo().StartColumn + m.editor.LineInfo().ColumnOffset}
}

// moveEditorCursor moves the editor cursor to p and scrolls the editor so that it is visible.
func (m *model) moveEditorCursor(p textPos) {
	// The textarea only moves the cursor by (soft-wrapped) rows, so step until the row
	// is reached. Stop if a step makes no progress.
	for m.editor.Line() > p.row {
		before := m.editorPos()
		m.editor.CursorUp()
		if m.editorPos() == before {
			break
		}
	}
	for m.editor.Line() < p.row {
		before := m.editorPos()
		m.editor.CursorDown()
		if m.editorPos() == before {
			break
		}
	}
	m.editor.SetCursor(p.col)
	// Update repositions the textarea viewport to the cursor.
	m.editor, _ = m.editor.Update(nil)
}

func (m model) startSearch() (tea.Model, tea.Cmd) {
	m.searching = true
	m.searchFailed = false
	m.searchOrigin = m.editorPos()
	m.searchMatch = m.searchOrigin
	m.search.SetValue("")
	return m, m.search.Focus()
}

func (m *model) endSearch() {
	m.searching = false
	m.search.Blur()
}

// runSearch moves the cursor to the first match at or after from.
func (m *model) runSearch(from textPos) {
	query := m.search.Value()
	if query == "" {
		m.searchFailed = false
		m.searchMatch = m.searchOrigin
		m.moveEditorCursor(m.searchOrigin)
		return
	}

	p, ok := findText(m.editor.Value(), query, from)
	m.searchFailed = !ok
	if ok {
		m.searchMatch = p
		m.moveEditorCursor(p)
	}
}

func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.endSearch()
		return m, nil

	case tea.KeyEsc, tea.KeyCtrlC:
		m.endSearch()
		m.moveEditorCursor(m.searchOrigin)
		return m, nil

	case tea.KeyCtrlR:
		// Next match, after the current one.
		m.runSearch(textPos{row: m.searchMatch.row, col: m.searchMatch.col + 1})
		return m, nil
	}

	before := m.search.Value()
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	if after := m.search.Value(); after != before {
		// Like Emacs isearch: a longer query stays at the current match if it still
		// matches there; any other change searches again from where the search started.
		from := m.searchOrigin
		if strings.HasPrefix(after, before) {
			from = m.searchMatch
		}
		m.runSearch(from)
	}
	return m, cmd
}

const searchNotFound = "  not found"

func (m model) searchView() string {
	v := m.search.View()
	if m.searchFailed {
		v += styleErr.Render(searchNotFound)
	}
	return v
}

// promptWidth returns the Width for a textinput in the status line so that the prompt,
// the text, the cursor and reserve cells fit in width.
func promptWidth(ti textinput.Model, width, reserve int) int {
	return max(width-lipgloss.Width(ti.Prompt)-1-reserve, 1)
}
