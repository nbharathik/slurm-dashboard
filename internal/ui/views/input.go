package views

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// LineInput is a one-line text editor (filter lines, search).
type LineInput struct {
	Prompt  string
	Err     string
	text    []rune
	cur     int
	initial string
}

// NewLineInput starts an input with initial text.
func NewLineInput(prompt, initial string) *LineInput {
	r := []rune(initial)
	return &LineInput{Prompt: prompt, text: r, cur: len(r), initial: initial}
}

// Value returns the text.
func (l *LineInput) Value() string { return string(l.text) }

// Initial is the text the input started with, restored when it is cancelled.
func (l *LineInput) Initial() string { return l.initial }

// Insert adds pasted text at the cursor; line breaks become spaces and
// control characters are dropped.
func (l *LineInput) Insert(s string) {
	r := []rune(PasteText(s))
	l.text = slices.Insert(l.text, l.cur, r...)
	l.cur += len(r)
	l.Err = ""
}

// PasteText makes pasted text safe for a one-line input.
func PasteText(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	return textsafe.Field(s)
}

// Update edits the text; it reports enter (done) and esc (cancel).
func (l *LineInput) Update(msg tea.KeyPressMsg) (done, cancel bool) {
	switch msg.String() {
	case "enter":
		return true, false
	case "esc":
		return false, true
	case "left":
		l.cur = max(l.cur-1, 0)
	case "right":
		l.cur = min(l.cur+1, len(l.text))
	case "home", "ctrl+a":
		l.cur = 0
	case "end", "ctrl+e":
		l.cur = len(l.text)
	case "backspace":
		if l.cur > 0 {
			l.text = slices.Delete(l.text, l.cur-1, l.cur)
			l.cur--
		}
	case "delete":
		if l.cur < len(l.text) {
			l.text = slices.Delete(l.text, l.cur, l.cur+1)
		}
	case "ctrl+u":
		l.text, l.cur = l.text[l.cur:], 0
	case "ctrl+w":
		i := l.cur
		for i > 0 && l.text[i-1] == ' ' {
			i--
		}
		for i > 0 && l.text[i-1] != ' ' {
			i--
		}
		l.text = slices.Delete(l.text, i, l.cur)
		l.cur = i
	case "space":
		l.text = slices.Insert(l.text, l.cur, ' ')
		l.cur++
	default:
		if msg.Text != "" {
			r := []rune(msg.Text)
			l.text = slices.Insert(l.text, l.cur, r...)
			l.cur += len(r)
		}
	}
	l.Err = ""
	return false, false
}

// View renders the input line with a block cursor.
func (l *LineInput) View(th theme.Theme, w int) string {
	before, after := string(l.text[:l.cur]), string(l.text[l.cur:])
	cursor := th.Selected.Render(" ")
	if r := []rune(after); len(r) > 0 {
		cursor, after = th.Selected.Render(string(r[0])), string(r[1:])
	}
	line := th.Accent.Render(l.Prompt) + before + cursor + after
	if l.Err != "" {
		line += "  " + th.Crit.Render(l.Err)
	}
	return layout.Pad(line, w, false, th.Sym.Ellipsis)
}
