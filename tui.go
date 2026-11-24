package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	debounceDelay = 300 * time.Millisecond
	evalTimeout   = 10 * time.Second
)

type pane int

const (
	paneEditor pane = iota
	paneOutput
)

type debounceMsg struct{ seq int }

type evalMsg struct {
	seq      int
	output   string
	err      error
	canceled bool
}

var (
	colorAccent = lipgloss.AdaptiveColor{Light: "#4a52c7", Dark: "#a3a9ff"}
	colorDim    = lipgloss.AdaptiveColor{Light: "#5d6773", Dark: "#7d8794"}
	colorLine   = lipgloss.AdaptiveColor{Light: "#d9dee4", Dark: "#323944"}
	colorErr    = lipgloss.AdaptiveColor{Light: "#c62828", Dark: "#ff8a80"}

	styleDim       = lipgloss.NewStyle().Foreground(colorDim)
	styleActive    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleErr       = lipgloss.NewStyle().Foreground(colorErr)
	styleStale     = lipgloss.NewStyle().Faint(true)
	styleErrBox    = lipgloss.NewStyle().Foreground(colorErr).Border(lipgloss.ThickBorder(), false, false, false, true).BorderForeground(colorErr).PaddingLeft(1)
	stylePane      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorLine)
	stylePaneFocus = stylePane.BorderForeground(colorAccent)
)

type model struct {
	eval       *evaluator
	inputLabel string
	fileCount  int
	tabWidth   int

	editor textarea.Model
	output viewport.Model
	focus  pane

	policyPath string
	savedValue string

	saving   bool
	filename textinput.Model
	notice   string

	killBuf string

	seq        int
	appliedSeq int
	cancelEval context.CancelFunc
	quitArmed  bool
	lastOutput string
	lastErr    error

	width  int
	height int
}

func newModel(ev *evaluator, inputLabel string, policyPath, policy string, tabWidth int) model {
	ed := textarea.New()
	ed.ShowLineNumbers = false
	ed.Prompt = ""
	ed.CharLimit = 0
	ed.MaxHeight = 0
	ed.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ed.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ed.SetValue(policy)
	ed.Focus()

	fn := textinput.New()
	fn.Prompt = "Save as: "
	fn.Placeholder = "policy.rego"

	return model{
		eval:       ev,
		inputLabel: inputLabel,
		fileCount:  len(ev.configs),
		tabWidth:   tabWidth,
		editor:     ed,
		output:     newOutputView(),
		focus:      paneEditor,
		policyPath: policyPath,
		savedValue: policy,
		filename:   fn,
	}
}

func (m model) Init() tea.Cmd {
	// Go through debounceMsg so the initial evaluation is started (and cancelable) from Update.
	seq := m.seq
	return tea.Batch(textarea.Blink, func() tea.Msg { return debounceMsg{seq: seq} })
}

// evalCmd cancels the evaluation still running, if any, and starts a new one with a timeout.
func (m *model) evalCmd(seq int) tea.Cmd {
	m.stopEval()

	src := m.editor.Value()
	name := "policy.rego"
	if m.policyPath != "" {
		name = filepath.Base(m.policyPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), evalTimeout)
	m.cancelEval = cancel
	ev := m.eval

	return func() tea.Msg {
		defer cancel()
		out, err := ev.evaluate(ctx, name, src)

		switch ctx.Err() {
		case context.Canceled:
			return evalMsg{seq: seq, canceled: true}
		case context.DeadlineExceeded:
			err = fmt.Errorf("evaluation timed out after %s", evalTimeout)
		}

		return evalMsg{seq: seq, output: out, err: err}
	}
}

func (m *model) stopEval() {
	if m.cancelEval != nil {
		m.cancelEval()
		m.cancelEval = nil
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case debounceMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		return m, m.evalCmd(msg.seq)

	case evalMsg:
		if msg.canceled || msg.seq < m.appliedSeq {
			return m, nil
		}
		m.appliedSeq = msg.seq
		if msg.err != nil {
			m.lastErr = msg.err
		} else {
			m.lastErr = nil
			m.lastOutput = msg.output
		}
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	if m.saving {
		m.filename, cmd = m.filename.Update(msg)
	} else if m.focus == paneEditor {
		m.editor, cmd = m.editor.Update(msg)
	}
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		if m.editor.Value() != m.savedValue && !m.quitArmed {
			m.quitArmed = true
			m.notice = styleErr.Render("unsaved changes: ^C again to quit")
			return m, nil
		}
		m.stopEval()
		return m, tea.Quit
	}
	m.quitArmed = false

	if m.saving {
		switch msg.Type {
		case tea.KeyEnter:
			name := strings.TrimSpace(m.filename.Value())
			if name == "" {
				return m, nil
			}
			m.saving = false
			m.filename.Blur()
			m.save(name)
			return m, m.focusCmd()
		case tea.KeyEsc:
			m.saving = false
			m.filename.Blur()
			m.notice = "save canceled"
			return m, m.focusCmd()
		}
		var cmd tea.Cmd
		m.filename, cmd = m.filename.Update(msg)
		return m, cmd
	}

	m.notice = ""

	switch msg.Type {
	case tea.KeyCtrlS:
		if m.policyPath != "" {
			m.save(m.policyPath)
			return m, nil
		}
		m.saving = true
		m.filename.SetValue("")
		m.editor.Blur()
		return m, m.filename.Focus()

	case tea.KeyCtrlO:
		if m.focus == paneEditor {
			m.focus = paneOutput
			m.editor.Blur()
			m.layout()
			return m, nil
		}
		m.focus = paneEditor
		m.layout()
		return m, m.editor.Focus()
	}

	if m.focus == paneOutput {
		var cmd tea.Cmd
		m.output, cmd = m.output.Update(msg)
		return m, cmd
	}

	return m.handleEditorKey(msg)
}

func (m model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := m.editor.Value()
	var cmd tea.Cmd

	switch {
	case msg.Type == tea.KeyCtrlY:
		if m.killBuf != "" {
			m.editor.InsertString(m.killBuf)
		}

	case msg.Type == tea.KeyTab:
		m.editor.InsertString(strings.Repeat(" ", m.tabWidth))

	case key.Matches(msg, m.editor.KeyMap.DeleteAfterCursor),
		key.Matches(msg, m.editor.KeyMap.DeleteBeforeCursor),
		key.Matches(msg, m.editor.KeyMap.DeleteWordBackward),
		key.Matches(msg, m.editor.KeyMap.DeleteWordForward):
		m.editor, cmd = m.editor.Update(msg)
		if killed := removedText(before, m.editor.Value(), cursorOffset(m.editor)); killed != "" {
			m.killBuf = killed
		}

	default:
		if msg.Type == tea.KeyRunes {
			// Pasted text can contain tabs; expand them before the textarea sanitizer does.
			msg.Runes = []rune(expandTabs(string(msg.Runes), m.tabWidth))
		}
		m.editor, cmd = m.editor.Update(msg)
	}

	if m.editor.Value() == before {
		return m, cmd
	}

	m.seq++
	seq := m.seq
	return m, tea.Batch(cmd, tea.Tick(debounceDelay, func(time.Time) tea.Msg {
		return debounceMsg{seq: seq}
	}))
}

func (m model) focusCmd() tea.Cmd {
	if m.focus == paneEditor {
		return m.editor.Focus()
	}
	return nil
}

func (m *model) save(path string) {
	value := m.editor.Value()
	content := value
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		m.notice = styleErr.Render("save failed: " + err.Error())
		return
	}
	m.policyPath = path
	m.savedValue = value
	m.notice = "saved " + filepath.Base(path)
}

func (m model) paneSize() (paneWidth, innerWidth, innerHeight int) {
	paneWidth = m.width / 2
	innerWidth = max(paneWidth-2, 1)
	// 1 row for the status line, 2 rows for the pane border, 1 row for the pane header.
	innerHeight = max(m.height-1-2-1, 1)
	return
}

func (m model) errorBox(width int) string {
	if m.lastErr == nil {
		return ""
	}
	return styleErrBox.Width(width).Render(m.lastErr.Error())
}

// layout resizes the editor and the output view and refreshes the output content.
func (m *model) layout() {
	if m.width == 0 {
		return
	}

	_, innerWidth, innerHeight := m.paneSize()

	m.editor.SetWidth(innerWidth)
	m.editor.SetHeight(innerHeight)

	outHeight := innerHeight
	if box := m.errorBox(innerWidth); box != "" {
		outHeight -= lipgloss.Height(box)
	}

	m.output.Width = innerWidth
	m.output.Height = max(outHeight, 0)

	content := m.lastOutput
	if content == "" && m.lastErr == nil {
		content = styleDim.Render("(empty policy)")
	}
	if m.lastErr != nil {
		content = styleStale.Render(content)
	}
	m.output.SetContent(content)
}

// header lays out left and right on one line of the given width, truncating left if needed.
func header(width int, left, right string) string {
	if avail := width - lipgloss.Width(right) - 1; lipgloss.Width(left) > avail {
		left = ansi.Truncate(left, max(avail, 0), "…")
	}
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	paneWidth, innerWidth, innerHeight := m.paneSize()

	// Editor pane
	edTitle := "Policy (Rego)"
	if m.focus == paneEditor {
		edTitle = styleActive.Render(edTitle)
	} else {
		edTitle = styleDim.Render(edTitle)
	}
	name := "[no file]"
	if m.policyPath != "" {
		name = filepath.Base(m.policyPath)
	}
	if m.editor.Value() != m.savedValue {
		name += " *"
	}
	edPane := lipgloss.JoinVertical(lipgloss.Left,
		header(innerWidth, edTitle, styleDim.Render(name)),
		m.editor.View(),
	)

	// Output pane
	outTitle := "Output"
	switch {
	case m.lastErr != nil:
		outTitle = styleErr.Render("● error — showing last result")
	case m.focus == paneOutput:
		outTitle = styleActive.Render(outTitle)
	default:
		outTitle = styleDim.Render(outTitle)
	}
	outInfo := fmt.Sprintf("%d files", m.fileCount)
	if m.eval.combined {
		outInfo = "combined"
	}
	outParts := []string{header(innerWidth, outTitle, styleDim.Render(outInfo))}
	if box := m.errorBox(innerWidth); box != "" {
		outParts = append(outParts, box)
	}
	outParts = append(outParts, m.output.View())
	outPane := lipgloss.JoinVertical(lipgloss.Left, outParts...)

	edStyle, outStyle := stylePane, stylePane
	if m.focus == paneEditor {
		edStyle = stylePaneFocus
	} else {
		outStyle = stylePaneFocus
	}
	paneHeight := innerHeight + 1

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		edStyle.Width(innerWidth).Height(paneHeight).MaxHeight(paneHeight+2).Render(edPane),
		outStyle.Width(m.width-paneWidth-2).Height(paneHeight).MaxHeight(paneHeight+2).Render(outPane),
	)

	return lipgloss.JoinVertical(lipgloss.Left, panes, m.statusLine())
}

func (m model) statusLine() string {
	if m.saving {
		return m.filename.View()
	}

	mode := "per-file"
	if m.eval.combined {
		mode = "combined"
	}

	parts := []string{
		fmt.Sprintf("%s · %d files", m.inputLabel, m.fileCount),
		"mode: " + mode,
	}
	if m.lastErr != nil {
		parts = append(parts, styleErr.Render("1 error"))
	}
	if m.notice != "" {
		parts = append(parts, m.notice)
	}

	left := strings.Join(parts, "   ")
	right := styleDim.Render("^S save  ^O switch  ^C quit")

	return styleDim.Render(header(m.width, left, right))
}

func newOutputView() viewport.Model {
	vp := viewport.New(0, 0)
	// Long JSON lines are cut at the pane edge; allow scrolling them with left/right.
	vp.SetHorizontalStep(8)
	return vp
}
