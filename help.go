package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type helpSection struct {
	title string
	keys  [][2]string
}

var helpSections = []helpSection{
	{"Editor", [][2]string{
		{"←→↑↓ C-f C-b C-n C-p", "move"},
		{"C-a / C-e", "start / end of line"},
		{"M-f / M-b", "next / previous word"},
		{"C-d", "delete the next character"},
		{"C-k / C-u", "delete to the end / start of line"},
		{"M-d / C-w", "delete the next / previous word"},
		{"C-y", "insert the text last deleted"},
		{"Tab", "insert spaces"},
	}},
	{"Search", [][2]string{
		{"C-r", "search / next match"},
		{"Enter", "stay at the match"},
		{"Esc", "go back to where the search started"},
	}},
	{"Output", [][2]string{
		{"↑↓ PgUp PgDn", "scroll"},
		{"←→", "scroll long lines"},
		{"?", "show this help"},
	}},
	{"General", [][2]string{
		{"C-s", "save"},
		{"C-o", "switch between the editor and the output"},
		{"F1", "show / hide this help"},
		{"C-c", "quit"},
	}},
}

var styleHelpBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)

func helpView() string {
	keyWidth := 0
	for _, s := range helpSections {
		for _, k := range s.keys {
			keyWidth = max(keyWidth, lipgloss.Width(k[0]))
		}
	}

	var b strings.Builder
	b.WriteString(styleActive.Render("Keys"))
	for _, s := range helpSections {
		b.WriteString("\n\n" + styleDim.Render(s.title))
		for _, k := range s.keys {
			b.WriteString("\n" + k[0] + strings.Repeat(" ", keyWidth-lipgloss.Width(k[0])+2) + k[1])
		}
	}
	b.WriteString("\n\n" + styleDim.Render("Esc / F1 to close"))

	return styleHelpBox.Render(b.String())
}

func (m model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyF1, tea.KeyEsc, tea.KeyEnter, tea.KeyCtrlC:
		m.showHelp = false
		return m, nil
	}
	if msg.Type == tea.KeyRunes && string(msg.Runes) == "q" {
		m.showHelp = false
	}
	// Other keys are ignored while the help is shown.
	return m, nil
}

// overlay draws fg centered on bg, which is width x height cells.
func overlay(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}

	fgLines := strings.Split(fg, "\n")
	fgWidth := lipgloss.Width(fg)
	x := max((width-fgWidth)/2, 0)
	y := max((height-len(fgLines))/2, 0)

	for i, fl := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bl := bgLines[row]
		if w := lipgloss.Width(bl); w < width {
			bl += strings.Repeat(" ", width-w)
		}
		left := ansi.Truncate(bl, x, "")
		right := ansi.Cut(bl, x+lipgloss.Width(fl), width)
		bgLines[row] = ansi.Truncate(left+ansi.ResetStyle+fl+ansi.ResetStyle+right, width, "")
	}

	return strings.Join(bgLines, "\n")
}
