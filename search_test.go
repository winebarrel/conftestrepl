package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestFindText(t *testing.T) {
	text := "foo Bar\nbar baz\nfoo"

	tests := []struct {
		query string
		from  textPos
		want  textPos
		ok    bool
	}{
		{"bar", textPos{0, 0}, textPos{0, 4}, true}, // smartcase: lower-case ignores case
		{"Bar", textPos{0, 5}, textPos{0, 4}, true}, // wraps around to before from
		{"bar", textPos{0, 5}, textPos{1, 0}, true}, // next match after from
		{"foo", textPos{0, 1}, textPos{2, 0}, true}, //
		{"foo", textPos{2, 1}, textPos{0, 0}, true}, // wraps to the top
		{"BAZ", textPos{0, 0}, textPos{}, false},    // upper-case is case-sensitive
		{"o\nb", textPos{0, 0}, textPos{}, false},   // no multi-line matches
		{"", textPos{0, 0}, textPos{}, false},       //
		{"foo", textPos{0, 0}, textPos{0, 0}, true}, // match at from
		{"oo", textPos{0, 2}, textPos{2, 1}, true},  //
		{"o B", textPos{1, 0}, textPos{0, 2}, true}, //
		{"zzz", textPos{1, 3}, textPos{}, false},    //
	}
	for _, tt := range tests {
		got, ok := findText(text, tt.query, tt.from)
		if ok != tt.ok || got != tt.want {
			t.Errorf("findText(%q, %+v) = %+v, %v; want %+v, %v", tt.query, tt.from, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSearchKeys(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "allow if {\n  x := 1\n}\ndeny if {\n  x := 2\n}", 4)
	m.width, m.height = 120, 40
	m.layout()

	send := func(msg tea.Msg) tea.Cmd {
		t.Helper()
		nm, cmd := m.Update(msg)
		m = nm.(model)
		return cmd
	}
	typ := func(s string) {
		t.Helper()
		for _, r := range s {
			send(textKey(string(r)))
		}
	}

	// Start at the top.
	for range 10 {
		send(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	send(ctrlKey('a'))
	value := m.editor.Value()

	send(ctrlKey('r'))
	if !m.searching {
		t.Fatal("Ctrl+R should start a search")
	}

	typ("x :")
	if p := m.editorPos(); p != (textPos{1, 2}) {
		t.Fatalf("incremental search: pos = %+v", p)
	}

	send(ctrlKey('r'))
	if p := m.editorPos(); p != (textPos{4, 2}) {
		t.Fatalf("next match: pos = %+v", p)
	}

	send(ctrlKey('r'))
	if p := m.editorPos(); p != (textPos{1, 2}) {
		t.Fatalf("wrapped match: pos = %+v", p)
	}

	typ("q")
	if !m.searchFailed {
		t.Fatal("search should fail")
	}
	if p := m.editorPos(); p != (textPos{1, 2}) {
		t.Fatalf("failed search should not move: pos = %+v", p)
	}

	// Esc returns to where the search started. Ctrl+C does the same and does not quit.
	cmd := send(ctrlKey('c'))
	if m.searching {
		t.Fatal("Ctrl+C should end the search")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Ctrl+C in search should not quit")
		}
	}
	if p := m.editorPos(); p != (textPos{0, 0}) {
		t.Fatalf("canceled search: pos = %+v", p)
	}

	// Enter keeps the cursor at the match.
	send(ctrlKey('r'))
	typ("deny")
	send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searching {
		t.Fatal("Enter should end the search")
	}
	if p := m.editorPos(); p != (textPos{3, 0}) {
		t.Fatalf("confirmed search: pos = %+v", p)
	}

	if m.editor.Value() != value || m.seq != 0 {
		t.Fatal("search should not edit the policy")
	}

	// Typing goes to the editor again.
	typ("z")
	if m.editor.Value() != "allow if {\n  x := 1\n}\nzdeny if {\n  x := 2\n}" {
		t.Fatalf("value = %q", m.editor.Value())
	}
}

func TestSearchWrappedLines(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	long := "aaaa bbbb cccc dddd eeee ffff gggg hhhh iiii jjjj"
	m := newModel(ev, "dir", "", long+"\n"+long+" target\nend", 4)
	m.width, m.height = 30, 6
	m.layout()

	for _, p := range []textPos{{1, 51}, {0, 3}, {2, 1}, {1, 0}} {
		m.moveEditorCursor(p)
		if got := m.editorPos(); got != p {
			t.Errorf("moveEditorCursor(%+v) = %+v", p, got)
		}
	}
}

func TestSearchRefineStaysAtMatch(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "x := 1\nx := 2\nx := 1", 4)
	m.width, m.height = 120, 40
	m.layout()

	send := func(msg tea.Msg) {
		t.Helper()
		nm, _ := m.Update(msg)
		m = nm.(model)
	}
	typ := func(s string) {
		t.Helper()
		for _, r := range s {
			send(textKey(string(r)))
		}
	}

	for range 10 {
		send(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	send(ctrlKey('a'))

	send(ctrlKey('r'))
	typ("x")
	send(ctrlKey('r'))
	if p := m.editorPos(); p != (textPos{1, 0}) {
		t.Fatalf("next match: pos = %+v", p)
	}

	// The longer query still matches at the current match, so the cursor stays.
	typ(" := ")
	if p := m.editorPos(); p != (textPos{1, 0}) {
		t.Fatalf("refined query: pos = %+v", p)
	}

	// It no longer matches here, so the cursor moves on.
	typ("1")
	if p := m.editorPos(); p != (textPos{2, 0}) {
		t.Fatalf("refined query moved on: pos = %+v", p)
	}

	// Deleting a character searches again from where the search started.
	send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if p := m.editorPos(); p != (textPos{0, 0}) {
		t.Fatalf("shortened query: pos = %+v", p)
	}
}

func TestStatusPromptsFitWidth(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "", 4)
	m.width, m.height = 40, 10
	m.layout()

	send := func(msg tea.Msg) {
		t.Helper()
		nm, _ := m.Update(msg)
		m = nm.(model)
	}
	long := tea.PasteMsg{Content: strings.Repeat("z", 100)}

	send(ctrlKey('r'))
	send(long)
	if !m.searchFailed {
		t.Fatal("search should fail")
	}
	if w := lipgloss.Width(m.statusLine()); w > m.width {
		t.Errorf("search prompt width = %d, want <= %d", w, m.width)
	}
	send(tea.KeyPressMsg{Code: tea.KeyEscape})

	send(ctrlKey('s'))
	send(long)
	if w := lipgloss.Width(m.statusLine()); w > m.width {
		t.Errorf("save prompt width = %d, want <= %d", w, m.width)
	}
}

func TestSearchOutput(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "", 4)
	m.width, m.height = 60, 10
	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	lines[20] = "line 20 target " + strings.Repeat("x", 80) + " far"
	m.lastOutput = strings.Join(lines, "\n")
	m.layout()

	send := func(msg tea.Msg) {
		t.Helper()
		nm, _ := m.Update(msg)
		m = nm.(model)
	}
	typ := func(s string) {
		t.Helper()
		for _, r := range s {
			send(textKey(string(r)))
		}
	}

	send(ctrlKey('o'))
	send(ctrlKey('r'))
	if !m.searching {
		t.Fatal("Ctrl+R in the output should start a search")
	}
	if !strings.Contains(m.statusLine(), "Search:") {
		t.Fatalf("status line = %q", m.statusLine())
	}

	typ("target")
	if m.searchMatch != (textPos{20, 8}) {
		t.Fatalf("match = %+v", m.searchMatch)
	}
	if y := m.output.YOffset(); y > 20 || 20 >= y+m.output.Height() {
		t.Fatalf("match line not visible: yoffset = %d", y)
	}
	if !strings.Contains(m.output.GetContent(), "\x1b[7m") {
		t.Fatal("match should be highlighted")
	}

	// A match beyond the pane width scrolls horizontally.
	typ(" x")
	if m.output.XOffset() != 0 {
		t.Fatalf("near match scrolled: xoffset = %d", m.output.XOffset())
	}
	send(ctrlKey('c'))
	send(ctrlKey('r'))
	typ("x far")
	if m.searchFailed || m.output.XOffset() == 0 {
		t.Fatalf("far match: failed = %v, xoffset = %d", m.searchFailed, m.output.XOffset())
	}

	// Esc returns to where the search started and removes the highlight.
	send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.searching || m.output.YOffset() != 0 || m.output.XOffset() != 0 {
		t.Fatalf("canceled search: searching = %v, offset = %d,%d", m.searching, m.output.XOffset(), m.output.YOffset())
	}
	if strings.Contains(m.output.GetContent(), "\x1b[7m") {
		t.Fatal("highlight should be removed")
	}

	// Ctrl+R goes to the next match; Enter stays there.
	send(ctrlKey('r'))
	typ("line 2")
	if m.searchMatch != (textPos{20, 0}) {
		t.Fatalf("first match = %+v", m.searchMatch)
	}
	send(ctrlKey('r'))
	if m.searchMatch != (textPos{21, 0}) {
		t.Fatalf("next match = %+v", m.searchMatch)
	}
	send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.searching || m.output.YOffset() == 0 {
		t.Fatalf("confirmed search: searching = %v, yoffset = %d", m.searching, m.output.YOffset())
	}
	if m.focus != paneOutput || m.editor.Value() != "" {
		t.Fatal("output search should not touch the editor")
	}
}
