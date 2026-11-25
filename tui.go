package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// theme holds the styles for a light or a dark terminal background.
type theme struct {
	dim       lipgloss.Style
	active    lipgloss.Style
	err       lipgloss.Style
	stale     lipgloss.Style
	errBox    lipgloss.Style
	pane      lipgloss.Style
	paneFocus lipgloss.Style
	helpBox   lipgloss.Style
	match     lipgloss.Style
}

func newTheme(isDark bool) theme {
	lightDark := lipgloss.LightDark(isDark)
	accent := lightDark(lipgloss.Color("#4a52c7"), lipgloss.Color("#a3a9ff"))
	dim := lightDark(lipgloss.Color("#5d6773"), lipgloss.Color("#7d8794"))
	line := lightDark(lipgloss.Color("#d9dee4"), lipgloss.Color("#323944"))
	errColor := lightDark(lipgloss.Color("#c62828"), lipgloss.Color("#ff8a80"))

	pane := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(line)

	return theme{
		dim:       lipgloss.NewStyle().Foreground(dim),
		active:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		err:       lipgloss.NewStyle().Foreground(errColor),
		stale:     lipgloss.NewStyle().Faint(true),
		errBox:    lipgloss.NewStyle().Foreground(errColor).Border(lipgloss.ThickBorder(), false, false, false, true).BorderForeground(errColor).PaddingLeft(1),
		pane:      pane,
		paneFocus: pane.BorderForeground(accent),
		helpBox:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1),
		match:     lipgloss.NewStyle().Reverse(true),
	}
}

func editorStyles(isDark bool) textarea.Styles {
	s := textarea.DefaultStyles(isDark)
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Blurred.CursorLine = lipgloss.NewStyle()
	return s
}

type model struct {
	eval       *evaluator
	inputLabel string
	fileCount  int
	tabWidth   int

	// theme follows the terminal background, which is queried at startup.
	theme theme

	editor textarea.Model
	output viewport.Model
	focus  pane

	policyPath string
	savedValue string

	saving   bool
	filename textinput.Model
	notice   string

	killBuf string

	searching    bool
	search       textinput.Model
	searchOrigin textPos
	searchMatch  textPos
	searchFailed bool
	// searchScroll is the x and y offset of the output when a search in it started.
	searchScroll [2]int

	showHelp bool

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
	ed.SetStyles(editorStyles(true))
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
		theme:      newTheme(true),
		editor:     ed,
		output:     newOutputView(),
		focus:      paneEditor,
		policyPath: policyPath,
		savedValue: policy,
		filename:   fn,
		search:     newSearchInput(),
	}
}

func (m model) Init() tea.Cmd {
	// Go through debounceMsg so the initial evaluation is started (and cancelable) from Update.
	seq := m.seq
	return tea.Batch(textarea.Blink, tea.RequestBackgroundColor, func() tea.Msg { return debounceMsg{seq: seq} })
}

// evalCmd cancels the evaluation still running, if any, and starts a new one with a timeout.
func (m *model) evalCmd(seq int) tea.Cmd {
	m.stopEval()

	src := m.editor.Value()
	// The module name is the policy path itself, as in conftest: its builtins
	// (parse_config_file, parse_combined_config_files) resolve relative paths
	// against the directory of the module file.
	name := "policy.rego"
	if m.policyPath != "" {
		name = m.policyPath
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
		if m.searchingOutput() {
			// The output changed under the search; find the match again.
			m.runSearch(m.searchMatch)
		}
		return m, nil

	case tea.BackgroundColorMsg:
		m.setTheme(msg.IsDark())
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg)
	}

	var cmd tea.Cmd
	if m.saving {
		m.filename, cmd = m.filename.Update(msg)
	} else if m.searching {
		var edCmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		// Keep the editor cursor blinking at the match.
		m.editor, edCmd = m.editor.Update(msg)
		cmd = tea.Batch(cmd, edCmd)
	} else if m.focus == paneEditor {
		m.editor, cmd = m.editor.Update(msg)
	}
	return m, cmd
}

func (m *model) setTheme(isDark bool) {
	m.theme = newTheme(isDark)
	m.editor.SetStyles(editorStyles(isDark))
	m.filename.SetStyles(textinput.DefaultStyles(isDark))
	m.search.SetStyles(textinput.DefaultStyles(isDark))
	m.layout()
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		return m.handleHelpKey(msg)
	}
	if m.searching {
		return m.handleSearchKey(msg)
	}

	if msg.String() == "ctrl+c" && !m.saving {
		if m.editor.Value() != m.savedValue && !m.quitArmed {
			m.quitArmed = true
			m.notice = m.theme.err.Render("unsaved changes: ^C again to quit")
			return m, nil
		}
		m.stopEval()
		return m, tea.Quit
	}
	m.quitArmed = false

	if m.saving {
		switch msg.String() {
		case "enter":
			name := strings.TrimSpace(m.filename.Value())
			if name == "" {
				return m, nil
			}
			m.saving = false
			m.filename.Blur()
			m.save(name)
			return m, m.focusCmd()
		case "esc", "ctrl+c":
			// Ctrl+C in the prompt only cancels the prompt, like Esc. The quit
			// confirmation would not be visible here.
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

	switch msg.String() {
	case "ctrl+s":
		if m.policyPath != "" {
			m.save(m.policyPath)
			return m, nil
		}
		m.saving = true
		m.filename.SetValue("")
		m.editor.Blur()
		return m, m.filename.Focus()

	case "ctrl+r":
		return m.startSearch()

	case "f1":
		m.showHelp = true
		return m, nil

	case "ctrl+o":
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
		if msg.Text == "?" {
			m.showHelp = true
			return m, nil
		}
		var cmd tea.Cmd
		m.output, cmd = m.output.Update(msg)
		return m, cmd
	}

	return m.handleEditorKey(msg)
}

func (m model) handleEditorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	before := m.editor.Value()
	var cmd tea.Cmd

	switch {
	case msg.String() == "ctrl+y":
		if m.killBuf != "" {
			m.editor.InsertString(m.killBuf)
		}

	case msg.String() == "tab":
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
		m.editor, cmd = m.editor.Update(msg)
	}

	return m.edited(before, cmd)
}

func (m model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case m.showHelp:
		return m, nil
	case m.saving:
		m.filename, cmd = m.filename.Update(msg)
		return m, cmd
	case m.searching:
		return m.updateSearchInput(msg)
	case m.focus != paneEditor:
		return m, nil
	}

	before := m.editor.Value()
	// Pasted text can contain tabs; expand them before the textarea sanitizer does.
	msg.Content = expandTabs(msg.Content, m.tabWidth)
	m.editor, cmd = m.editor.Update(msg)
	return m.edited(before, cmd)
}

// edited schedules an evaluation if the policy changed from before.
func (m model) edited(before string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
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
		m.notice = m.theme.err.Render("save failed: " + err.Error())
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
	return m.theme.errBox.Width(width).Render(m.lastErr.Error())
}

// layout resizes the editor and the output view and refreshes the output content.
func (m *model) layout() {
	if m.width == 0 {
		return
	}

	_, innerWidth, innerHeight := m.paneSize()

	// Keep the status line prompts on one line.
	m.filename.SetWidth(promptWidth(m.filename, m.width, 0))
	m.search.SetWidth(promptWidth(m.search, m.width, len(searchNotFound)))

	m.editor.SetWidth(innerWidth)
	m.editor.SetHeight(innerHeight)

	outHeight := innerHeight
	if box := m.errorBox(innerWidth); box != "" {
		outHeight -= lipgloss.Height(box)
	}

	m.output.SetWidth(innerWidth)
	m.output.SetHeight(max(outHeight, 0))

	content := m.outputText()
	switch {
	case m.lastErr != nil:
		content = m.theme.stale.Render(content)
	case m.lastOutput == "":
		content = m.theme.dim.Render(content)
	}
	m.output.SetContent(m.highlightOutput(content))
}

// outputText returns the text shown in the output, without styles.
func (m model) outputText() string {
	if m.lastOutput == "" && m.lastErr == nil {
		return emptyPolicy
	}
	return m.lastOutput
}

const emptyPolicy = "(empty policy)"

// header lays out left and right on one line that never exceeds width.
// When they don't fit, right keeps its tail (e.g. "…/main.rego *") but gives up to
// half the width to left, and left is then truncated to the space that remains.
func header(width int, left, right string) string {
	width = max(width, 0)
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)

	if lw+1+rw > width {
		if maxRight := max(width-min(lw, width/2)-1, 0); rw > maxRight {
			right = truncateLeft(right, maxRight)
			rw = lipgloss.Width(right)
		}
		if avail := max(width-rw-1, 0); lw > avail {
			left = ansi.Truncate(left, avail, "…")
			lw = lipgloss.Width(left)
		}
	}

	gap := max(width-lw-rw, 1)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, width, "")
}

// truncateLeft keeps the last cells of s so that the result, including a leading "…",
// is at most width cells wide.
func truncateLeft(s string, width int) string {
	w := lipgloss.Width(s)
	if w <= width {
		return s
	}
	if width <= 1 {
		return ansi.Truncate("…", width, "")
	}
	return "…" + ansi.Cut(s, w-(width-1), w)
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) render() string {
	if m.width == 0 {
		return ""
	}

	paneWidth, innerWidth, innerHeight := m.paneSize()

	// Editor pane
	edTitle := "Policy (Rego)"
	if m.focus == paneEditor {
		edTitle = m.theme.active.Render(edTitle)
	} else {
		edTitle = m.theme.dim.Render(edTitle)
	}
	name := "[no file]"
	if m.policyPath != "" {
		name = filepath.Base(m.policyPath)
	}
	if m.editor.Value() != m.savedValue {
		name += " *"
	}
	edPane := lipgloss.JoinVertical(lipgloss.Left,
		header(innerWidth, edTitle, m.theme.dim.Render(name)),
		m.editor.View(),
	)

	// Output pane
	outTitle := "Output"
	switch {
	case m.lastErr != nil:
		outTitle = m.theme.err.Render("● error — showing last result")
	case m.focus == paneOutput:
		outTitle = m.theme.active.Render(outTitle)
	default:
		outTitle = m.theme.dim.Render(outTitle)
	}
	outInfo := fmt.Sprintf("%d files", m.fileCount)
	if m.eval.combined {
		outInfo = "combined"
	}
	outParts := []string{header(innerWidth, outTitle, m.theme.dim.Render(outInfo))}
	if box := m.errorBox(innerWidth); box != "" {
		outParts = append(outParts, box)
	}
	outParts = append(outParts, m.output.View())
	outPane := lipgloss.JoinVertical(lipgloss.Left, outParts...)

	edStyle, outStyle := m.theme.pane, m.theme.pane
	if m.focus == paneEditor {
		edStyle = m.theme.paneFocus
	} else {
		outStyle = m.theme.paneFocus
	}
	// Width and Height include the border.
	paneHeight := innerHeight + 1 + 2

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		edStyle.Width(paneWidth).Height(paneHeight).MaxHeight(paneHeight).Render(edPane),
		outStyle.Width(m.width-paneWidth).Height(paneHeight).MaxHeight(paneHeight).Render(outPane),
	)

	view := lipgloss.JoinVertical(lipgloss.Left, panes, m.statusLine())
	if m.showHelp {
		view = overlay(view, m.helpView(), m.width, m.height)
	}
	return view
}

func (m model) statusLine() string {
	if m.saving {
		return m.filename.View()
	}
	if m.searching {
		return m.searchView()
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
		parts = append(parts, m.theme.err.Render("1 error"))
	}
	if m.notice != "" {
		parts = append(parts, m.notice)
	}

	left := strings.Join(parts, "   ")
	right := m.theme.dim.Render("^S save  ^R search  ^O switch  F1 help  ^C quit")

	return m.theme.dim.Render(header(m.width, left, right))
}

func newOutputView() viewport.Model {
	vp := viewport.New()
	// Long JSON lines are cut at the pane edge; allow scrolling them with left/right.
	vp.SetHorizontalStep(8)
	return vp
}
