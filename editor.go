package main

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
)

// expandTabs replaces every tab with width spaces. The editor never holds tab characters
// because the textarea sanitizer would always turn them into four spaces.
func expandTabs(s string, width int) string {
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", width))
}

// removedText returns the text deleted when before became after. C-k, C-u, C-w and M-d
// all delete one contiguous range and leave the cursor at its start, so start is the
// cursor offset (in runes) after the deletion.
func removedText(before, after string, start int) string {
	b := []rune(before)
	n := len(b) - len([]rune(after))

	if n <= 0 || start < 0 || start+n > len(b) {
		return ""
	}

	return string(b[start : start+n])
}

// cursorOffset returns the cursor position of the textarea as a rune offset into Value().
func cursorOffset(ta textarea.Model) int {
	lines := strings.Split(ta.Value(), "\n")
	row := min(ta.Line(), len(lines)-1)

	offset := 0
	for _, l := range lines[:row] {
		offset += len([]rune(l)) + 1
	}

	li := ta.LineInfo()
	return offset + li.StartColumn + li.ColumnOffset
}
