package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestHelpPopup(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "package main", 4)
	m.width, m.height = 100, 40
	m.layout()

	send := func(msg tea.Msg) tea.Cmd {
		t.Helper()
		nm, cmd := m.Update(msg)
		m = nm.(model)
		return cmd
	}

	send(tea.KeyPressMsg{Code: tea.KeyF1})
	if !m.showHelp {
		t.Fatal("F1 should show the help")
	}

	view := m.render()
	if !strings.Contains(view, "show / hide this help") {
		t.Fatal("help should be drawn")
	}
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Errorf("view height = %d, want %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != m.width {
			t.Errorf("line %d width = %d, want %d", i, w, m.width)
		}
	}

	// Keys don't reach the editor while the help is shown, and Ctrl+C only closes it.
	send(textKey("x"))
	if m.editor.Value() != "package main" {
		t.Fatalf("value = %q", m.editor.Value())
	}
	cmd := send(ctrlKey('c'))
	if m.showHelp {
		t.Fatal("Ctrl+C should close the help")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Ctrl+C in the help should not quit")
		}
	}

	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: tea.KeyF1}, textKey("q")} {
		send(tea.KeyPressMsg{Code: tea.KeyF1})
		send(k)
		if m.showHelp {
			t.Fatalf("%v should close the help", k)
		}
	}

	// "?" opens the help from the output pane but is typed in the editor.
	send(textKey("?"))
	if m.showHelp || m.editor.Value() != "?package main" {
		t.Fatalf("? in the editor: help = %v, value = %q", m.showHelp, m.editor.Value())
	}
	send(ctrlKey('o'))
	send(textKey("?"))
	if !m.showHelp {
		t.Fatal("? in the output pane should show the help")
	}
}

func TestHelpPopupSmallTerminal(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "", 4)
	m.width, m.height = 20, 8
	m.layout()
	m.showHelp = true

	lines := strings.Split(m.render(), "\n")
	if len(lines) > m.height {
		t.Errorf("view height = %d, want <= %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Errorf("line %d width = %d, want <= %d", i, w, m.width)
		}
	}
}
