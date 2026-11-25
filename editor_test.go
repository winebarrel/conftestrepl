package main

import (
	"encoding/json"
	"image/color"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestExpandTabs(t *testing.T) {
	if got := expandTabs("a\tb\n\tc", 2); got != "a  b\n  c" {
		t.Errorf("got %q", got)
	}
}

func TestRemovedText(t *testing.T) {
	tests := []struct {
		before, after string
		start         int
		want          string
	}{
		{"hello world", "hello ", 6, "world"},
		{"hello world", "world", 0, "hello "},
		{"foo bar baz", "foo baz", 4, "bar "},
		{"line1\nline2", "line1line2", 5, "\n"},
		{"日本語テキスト", "日本語", 3, "テキスト"},
		{"same", "same", 0, ""},
		{"ab", "abc", 0, ""},
	}
	for _, tt := range tests {
		if got := removedText(tt.before, tt.after, tt.start); got != tt.want {
			t.Errorf("removedText(%q, %q, %d) = %q, want %q", tt.before, tt.after, tt.start, got, tt.want)
		}
	}
}

func TestEditorStartsAtBeginning(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "line1\nline2\nline3", 4)
	if got := m.editorPos(); got != (textPos{}) {
		t.Errorf("cursor = %+v, want beginning", got)
	}
}

// TestEditorKeys drives the real model with key messages.
func TestEditorKeys(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "foo bar baz\nqux", 4)
	m.width, m.height = 120, 40
	m.layout()

	send := func(msg tea.Msg) {
		t.Helper()
		nm, _ := m.Update(msg)
		m = nm.(model)
	}
	alt := func(r rune) tea.KeyPressMsg {
		return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt}
	}

	send(ctrlKey('a'))
	send(alt('f')) // after "foo"
	send(alt('d')) // delete " bar"
	if m.killBuf != " bar" || m.editor.Value() != "foo baz\nqux" {
		t.Fatalf("M-d: kill = %q, value = %q", m.killBuf, m.editor.Value())
	}

	send(ctrlKey('k')) // delete " baz"
	if m.killBuf != " baz" || m.editor.Value() != "foo\nqux" {
		t.Fatalf("C-k: kill = %q, value = %q", m.killBuf, m.editor.Value())
	}

	send(ctrlKey('a'))
	send(ctrlKey('y'))
	if m.editor.Value() != " bazfoo\nqux" {
		t.Fatalf("C-y: value = %q", m.editor.Value())
	}

	send(ctrlKey('e'))
	send(ctrlKey('w'))
	if m.killBuf != " bazfoo" || m.editor.Value() != "\nqux" {
		t.Fatalf("C-w: kill = %q, value = %q", m.killBuf, m.editor.Value())
	}

	send(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.editor.Value() != "    \nqux" {
		t.Fatalf("tab: value = %q", m.editor.Value())
	}

	send(tea.PasteMsg{Content: "a	b"})
	if m.editor.Value() != "    a    b\nqux" {
		t.Fatalf("paste: value = %q", m.editor.Value())
	}

	send(ctrlKey('u'))
	if m.killBuf != "    a    b" || m.editor.Value() != "\nqux" {
		t.Fatalf("C-u: kill = %q, value = %q", m.killBuf, m.editor.Value())
	}

	// C-k at the end of a line joins the next line; the killed text is the newline.
	send(ctrlKey('k'))
	if m.killBuf != "\n" || m.editor.Value() != "qux" {
		t.Fatalf("C-k at EOL: kill = %q, value = %q", m.killBuf, m.editor.Value())
	}

	if m.seq == 0 {
		t.Error("edits should bump the evaluation sequence")
	}
}

func TestQuitWithUnsavedChanges(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "package main", 4)

	ctrlC := ctrlKey('c')
	isQuit := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}

	// No unsaved changes: quit right away.
	if _, cmd := m.Update(ctrlC); !isQuit(cmd) {
		t.Fatal("Ctrl+C without changes should quit")
	}

	nm, _ := m.Update(textKey("x"))
	m = nm.(model)

	nm, cmd := m.Update(ctrlC)
	m = nm.(model)
	if isQuit(cmd) || !m.quitArmed {
		t.Fatal("first Ctrl+C with unsaved changes should ask for confirmation")
	}

	// Any other key disarms the confirmation.
	nm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = nm.(model)
	if m.quitArmed {
		t.Fatal("another key should disarm quit")
	}

	nm, _ = m.Update(ctrlC)
	m = nm.(model)
	if _, cmd := m.Update(ctrlC); !isQuit(cmd) {
		t.Fatal("second Ctrl+C should quit")
	}
}

func TestCtrlCInSavePrompt(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "", 4)

	send := func(msg tea.Msg) tea.Cmd {
		t.Helper()
		nm, cmd := m.Update(msg)
		m = nm.(model)
		return cmd
	}

	send(textKey("x"))
	send(ctrlKey('s'))
	if !m.saving {
		t.Fatal("Ctrl+S without -p should open the save prompt")
	}

	cmd := send(ctrlKey('c'))
	if m.saving {
		t.Fatal("Ctrl+C should cancel the save prompt")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Ctrl+C in the save prompt should not quit")
		}
	}
	if m.quitArmed {
		t.Fatal("Ctrl+C in the save prompt should not arm quit")
	}
}

// TestPolicyRelativePaths checks that conftest builtins resolve relative paths against
// the directory of the -p policy file, not the current directory.
func TestPolicyRelativePaths(t *testing.T) {
	ev := &evaluator{configs: map[string]any{"x.json": map[string]any{}}}
	src := `package main

b := parse_config_file("../inputs/config.json")
c := parse_combined_config_files(["../inputs/config.json"])
`
	m := newModel(ev, "dir", "testdata/policies/main.rego", src, 4)

	msg := m.evalCmd(1)().(evalMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}

	var got map[string]map[string]any
	if err := json.Unmarshal([]byte(msg.output), &got); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(got["x.json"]["b"], map[string]any{"kind": "Config"}) {
		t.Errorf("parse_config_file = %v (output %s)", got["x.json"]["b"], msg.output)
	}
	if _, ok := got["x.json"]["c"]; !ok {
		t.Errorf("parse_combined_config_files is undefined (output %s)", msg.output)
	}
}

func TestHeaderFitsWidth(t *testing.T) {
	long := "/home/user/work/very/long/path/policies/main.rego *"
	for _, width := range []int{0, 1, 2, 5, 10, 30, 52, 80} {
		for _, right := range []string{"main.rego", long, ""} {
			got := header(width, "Policy (Rego)", right)
			if w := lipgloss.Width(got); w > width {
				t.Errorf("header(%d, ..., %q) is %d wide: %q", width, right, w, got)
			}
		}
	}

	if got := header(30, "Policy (Rego)", "main.rego"); got != "Policy (Rego)        main.rego" {
		t.Errorf("short header = %q", got)
	}

	got := header(30, "Policy (Rego)", long)
	if lipgloss.Width(got) != 30 || !strings.HasPrefix(got, "Policy (Rego)") || !strings.HasSuffix(got, "main.rego *") || !strings.Contains(got, "…") {
		t.Errorf("long header = %q", got)
	}
}

// TestViewWithLongPolicyPath checks that a long -p path doesn't push the layout
// past the terminal size.
func TestViewWithLongPolicyPath(t *testing.T) {
	ev := &evaluator{configs: map[string]any{"x.json": map[string]any{}}}
	path := "/home/user/work/very/long/path/to/the/policies/directory/main.rego"
	m := newModel(ev, "dir", path, "package main\n\nx := 1\nlast_line := 2", 4)

	nm, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	m = nm.(model)
	nm, _ = m.Update(textKey(" ")) // mark as modified
	m = nm.(model)

	view := m.render()
	lines := strings.Split(view, "\n")
	if len(lines) != 12 {
		t.Errorf("view has %d lines, want 12:\n%s", len(lines), view)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > 60 {
			t.Errorf("line %d is %d wide: %q", i, w, l)
		}
	}
	if !strings.Contains(view, "Policy (Rego)") || !strings.Contains(view, "main.rego *") {
		t.Errorf("header missing:\n%s", view)
	}
	if strings.Contains(view, "/home/user") {
		t.Errorf("editor header should show only the base name:\n%s", view)
	}
	if !strings.Contains(view, "last_line := 2") {
		t.Errorf("last editor line is cut off:\n%s", view)
	}
}

func ctrlKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

func textKey(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func TestBackgroundColorSetsTheme(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "", 4)
	m.width, m.height = 80, 20
	m.layout()

	if m.theme.active.GetForeground() != newTheme(true).active.GetForeground() {
		t.Fatal("the theme should default to dark")
	}

	nm, _ := m.Update(tea.BackgroundColorMsg{Color: color.White})
	m = nm.(model)
	if m.theme.active.GetForeground() != newTheme(false).active.GetForeground() {
		t.Error("a light background should select the light theme")
	}

	nm, _ = m.Update(tea.BackgroundColorMsg{Color: color.Black})
	m = nm.(model)
	if m.theme.active.GetForeground() != newTheme(true).active.GetForeground() {
		t.Error("a dark background should select the dark theme")
	}
}
