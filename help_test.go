package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestHelpPopup(t *testing.T) {
	ev := &evaluator{configs: map[string]any{}}
	m := newModel(ev, "dir", "", "package main", 4)
	m.width, m.height = 100, 40
	m.layout()

	send := func(msg tea.KeyMsg) tea.Cmd {
		t.Helper()
		nm, cmd := m.Update(msg)
		m = nm.(model)
		return cmd
	}

	send(tea.KeyMsg{Type: tea.KeyF1})
	if !m.showHelp {
		t.Fatal("F1 should show the help")
	}

	view := m.View()
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
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.editor.Value() != "package main" {
		t.Fatalf("value = %q", m.editor.Value())
	}
	cmd := send(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.showHelp {
		t.Fatal("Ctrl+C should close the help")
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("Ctrl+C in the help should not quit")
		}
	}

	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyF1}, {Type: tea.KeyRunes, Runes: []rune("q")}} {
		send(tea.KeyMsg{Type: tea.KeyF1})
		send(k)
		if m.showHelp {
			t.Fatalf("%v should close the help", k)
		}
	}

	// "?" opens the help from the output pane but is typed in the editor.
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.showHelp || m.editor.Value() != "package main?" {
		t.Fatalf("? in the editor: help = %v, value = %q", m.showHelp, m.editor.Value())
	}
	send(tea.KeyMsg{Type: tea.KeyCtrlO})
	send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
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

	lines := strings.Split(m.View(), "\n")
	if len(lines) > m.height {
		t.Errorf("view height = %d, want <= %d", len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w > m.width {
			t.Errorf("line %d width = %d, want <= %d", i, w, m.width)
		}
	}
}
