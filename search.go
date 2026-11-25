package main

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	m.editor.SetCursorColumn(p.col)
	// Update repositions the textarea viewport to the cursor.
	m.editor, _ = m.editor.Update(nil)
}

// searchingOutput reports whether the search runs in the output instead of the editor.
func (m model) searchingOutput() bool {
	return m.searching && m.focus == paneOutput
}

func (m model) startSearch() (tea.Model, tea.Cmd) {
	m.searching = true
	m.searchFailed = false
	if m.focus == paneOutput {
		// Search the output from the top of the view.
		m.searchOrigin = textPos{row: m.output.YOffset()}
		m.searchScroll = [2]int{m.output.XOffset(), m.output.YOffset()}
	} else {
		m.searchOrigin = m.editorPos()
	}
	m.searchMatch = m.searchOrigin
	m.search.SetValue("")
	return m, m.search.Focus()
}

func (m *model) endSearch() {
	output := m.searchingOutput()
	m.searching = false
	m.search.Blur()
	if output {
		// Remove the highlight.
		m.layout()
	}
}

// cancelSearch ends the search and goes back to where it started.
func (m *model) cancelSearch() {
	if m.searchingOutput() {
		m.endSearch()
		m.output.SetXOffset(m.searchScroll[0])
		m.output.SetYOffset(m.searchScroll[1])
		return
	}
	m.endSearch()
	m.moveEditorCursor(m.searchOrigin)
}

// searchText returns the text that the search runs on.
func (m model) searchText() string {
	if m.searchingOutput() {
		return m.outputText()
	}
	return m.editor.Value()
}

// moveToMatch shows p, the position of a match or the search origin.
func (m *model) moveToMatch(p textPos) {
	if !m.searchingOutput() {
		m.moveEditorCursor(p)
		return
	}

	// Redraw the output with the highlight at the new match.
	m.layout()
	if m.search.Value() == "" || m.searchFailed {
		m.output.SetXOffset(m.searchScroll[0])
		m.output.SetYOffset(m.searchScroll[1])
		return
	}
	start, end := m.outputMatchCells()
	m.output.EnsureVisible(p.row, start, end)
}

// outputMatchCells returns the cell range of the current match in its output line.
func (m model) outputMatchCells() (int, int) {
	lines := strings.Split(m.outputText(), "\n")
	if m.searchMatch.row >= len(lines) {
		return 0, 0
	}
	line := []rune(lines[m.searchMatch.row])
	col := min(m.searchMatch.col, len(line))
	end := min(col+len([]rune(m.search.Value())), len(line))
	start := lipgloss.Width(string(line[:col]))
	return start, start + lipgloss.Width(string(line[col:end]))
}

// highlightOutput highlights the current match in content, the rendered output.
func (m model) highlightOutput(content string) string {
	if !m.searchingOutput() || m.search.Value() == "" || m.searchFailed {
		return content
	}
	lines := strings.Split(content, "\n")
	if m.searchMatch.row >= len(lines) {
		return content
	}
	start, end := m.outputMatchCells()
	if end > start {
		lines[m.searchMatch.row] = lipgloss.StyleRanges(lines[m.searchMatch.row], lipgloss.NewRange(start, end, m.theme.match))
	}
	return strings.Join(lines, "\n")
}

// runSearch moves the cursor to the first match at or after from.
func (m *model) runSearch(from textPos) {
	query := m.search.Value()
	if query == "" {
		m.searchFailed = false
		m.searchMatch = m.searchOrigin
		m.moveToMatch(m.searchOrigin)
		return
	}

	p, ok := findText(m.searchText(), query, from)
	m.searchFailed = !ok
	if ok {
		m.searchMatch = p
		m.moveToMatch(p)
	} else if m.searchingOutput() {
		// Remove the highlight of the previous match.
		m.layout()
	}
}

func (m model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.endSearch()
		return m, nil

	case "esc", "ctrl+c":
		m.cancelSearch()
		return m, nil

	case "ctrl+r":
		// Next match, after the current one.
		m.runSearch(textPos{row: m.searchMatch.row, col: m.searchMatch.col + 1})
		return m, nil
	}

	return m.updateSearchInput(msg)
}

// updateSearchInput passes msg to the search prompt and searches again if the query changed.
func (m model) updateSearchInput(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		v += m.theme.err.Render(searchNotFound)
	}
	return v
}

// promptWidth returns the Width for a textinput in the status line so that the prompt,
// the text, the cursor and reserve cells fit in width.
func promptWidth(ti textinput.Model, width, reserve int) int {
	return max(width-lipgloss.Width(ti.Prompt)-1-reserve, 1)
}
