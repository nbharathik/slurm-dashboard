package views

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// Requests from the log viewer.
type (
	// CloseLogMsg closes the log viewer.
	CloseLogMsg struct{}
	// LogStreamMsg switches between stdout and stderr.
	LogStreamMsg struct{ Stderr bool }
	// PagerPathMsg opens a file in $PAGER.
	PagerPathMsg struct{ Path string }
)

// LogView shows a followed log file or static text (a batch script).
type LogView struct {
	Title  string
	Job    model.Job
	Stderr bool
	Path   string
	Static bool // no following, no stream switching
	Buf    *logs.Buffer
	State  logs.State
	Note   string // extra context, e.g. a guessed path
	Err    string

	follow  bool
	wrap    bool
	top     int // first visible line
	xoff    int // horizontal scroll when not wrapping
	search  *LineInput
	matcher *logs.Matcher
	query   string
	height  int // rows of text in the last render
}

// NewLogView returns a viewer over buf. Static text starts at the top;
// logs start following the end.
func NewLogView(title string, buf *logs.Buffer, static bool) *LogView {
	return &LogView{Title: title, Buf: buf, Static: static, follow: !static, wrap: static}
}

// Following reports whether the view sticks to the end.
func (v *LogView) Following() bool { return v.follow }

// Capturing reports that the search line is open.
func (v *LogView) Capturing() bool { return v.search != nil }

func (v *LogView) maxTop() int { return max(v.Buf.Len()-max(v.height, 1), 0) }

func (v *LogView) scroll(d int) {
	v.top = min(max(v.top+d, 0), v.maxTop())
	v.follow = !v.Static && d > 0 && v.top >= v.maxTop() && v.follow
}

// Update handles keys and the mouse wheel.
func (v *LogView) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	k := ctx.Keys
	if v.search != nil {
		m, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return nil
		}
		done, cancel := v.search.Update(m)
		switch {
		case cancel:
			v.search = nil
		case done:
			q := v.search.Value()
			mt, err := logs.NewMatcher(q)
			if err != nil {
				v.search.Err = err.Error()
				return nil
			}
			v.search, v.matcher, v.query = nil, mt, q
			if mt != nil && !v.jump(1, v.top, v.isMatch) {
				return Emit(FlashMsg{Text: "no match for " + q, Err: true})
			}
		}
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, k.Back):
			return Emit(CloseLogMsg{})
		case msg.String() == ":":
			return Emit(PaletteMsg{})
		case key.Matches(msg, k.Search):
			v.search = NewLineInput("search: ", v.query)
		case key.Matches(msg, k.NextMatch):
			v.jumpMatch(1)
		case key.Matches(msg, k.PrevMatch):
			v.jumpMatch(-1)
		case key.Matches(msg, k.NextHighlight):
			v.jump(1, v.top+1, v.isHighlighted)
		case key.Matches(msg, k.PrevHighlight):
			v.jump(-1, v.top-1, v.isHighlighted)
		case key.Matches(msg, k.Follow) && !v.Static:
			v.follow = !v.follow
			if v.follow {
				v.top = v.maxTop()
			}
		case key.Matches(msg, k.Wrap):
			v.wrap, v.xoff = !v.wrap, 0
		case key.Matches(msg, k.SwitchStream) && !v.Static:
			return Emit(LogStreamMsg{Stderr: !v.Stderr})
		case key.Matches(msg, k.Pager) && v.Path != "":
			return Emit(PagerPathMsg{Path: v.Path})
		case key.Matches(msg, k.Up):
			v.follow = false
			v.scroll(-1)
		case key.Matches(msg, k.Down):
			v.scroll(1)
		case key.Matches(msg, k.PageUp):
			v.follow = false
			v.scroll(-max(v.height-1, 1))
		case key.Matches(msg, k.PageDown):
			v.scroll(max(v.height-1, 1))
		case key.Matches(msg, k.Home):
			v.follow, v.top = false, 0
		case key.Matches(msg, k.End):
			v.top = v.maxTop()
			v.follow = !v.Static
		case msg.String() == "left" && !v.wrap:
			v.xoff = max(v.xoff-8, 0)
		case msg.String() == "right" && !v.wrap:
			v.xoff += 8
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			v.follow = false
			v.scroll(-3)
		case tea.MouseWheelDown:
			v.scroll(3)
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && ctx.InZone("log:close", msg) {
			return Emit(CloseLogMsg{})
		}
	}
	return nil
}

func (v *LogView) isMatch(i int) bool { return v.matcher.Match(v.Buf.Line(i).Text) }

func (v *LogView) isHighlighted(i int) bool { return v.Buf.Line(i).Level != logs.Plain }

// jump moves to the next line from start (inclusive) in direction dir that
// satisfies ok, keeping two lines of context above it.
func (v *LogView) jump(dir, start int, ok func(int) bool) bool {
	n := v.Buf.Len()
	for i := start; i >= 0 && i < n; i += dir {
		if ok(i) {
			v.follow = false
			v.top = min(max(i-2, 0), v.maxTop())
			if i-2 > v.maxTop() {
				v.top = v.maxTop()
			}
			return true
		}
	}
	return false
}

func (v *LogView) jumpMatch(dir int) {
	if v.matcher == nil {
		return
	}
	// The match line sits two lines below the top after a jump.
	cur := v.top + 2
	v.jump(dir, cur+dir, v.isMatch)
}

// Hints are the footer keys.
func (v *LogView) Hints(ctx *Context) []key.Binding {
	k := ctx.Keys
	if v.search != nil {
		return []key.Binding{bind("enter", "find"), bind("esc", "cancel")}
	}
	hints := []key.Binding{bind("esc", "close")}
	if !v.Static {
		hints = append(hints, k.Follow, k.SwitchStream)
	}
	hints = append(hints, k.Search, k.NextHighlight, k.Wrap)
	if v.Path != "" {
		hints = append(hints, k.Pager)
	}
	return append(hints, bind(":", "command"))
}

// Render draws the viewer in w×h.
func (v *LogView) Render(ctx *Context, w, h int) string {
	th := ctx.Theme
	var foot string
	if v.search != nil {
		foot = v.search.View(th, w)
		h--
	}
	inner := w - 2
	rows := max(h-3, 1) // borders and the status line
	v.height = rows
	if v.follow {
		v.top = v.maxTop()
	}
	v.top = min(max(v.top, 0), v.maxTop())

	var out []string
	gutter := func(l logs.Line) string {
		switch l.Level {
		case logs.Error:
			return th.Crit.Render(th.Pick("▌", "!"))
		case logs.Warning:
			return th.Warn.Render(th.Pick("▌", "?"))
		}
		return " "
	}
	textW := max(inner-2, 1)
	if v.wrap && v.follow {
		// Fill from the bottom so the newest lines are visible.
		var rev []string
		for i := v.Buf.Len() - 1; i >= 0 && len(rev) < rows; i-- {
			l := v.Buf.Line(i)
			parts := wrapLine(v.styled(ctx, l.Text), textW)
			for j := len(parts) - 1; j >= 0 && len(rev) < rows; j-- {
				g := " "
				if j == 0 {
					g = gutter(l)
				}
				rev = append(rev, g+" "+parts[j])
			}
		}
		for i := len(rev) - 1; i >= 0; i-- {
			out = append(out, rev[i])
		}
	} else {
		for i := v.top; i < v.Buf.Len() && len(out) < rows; i++ {
			l := v.Buf.Line(i)
			if v.wrap {
				for j, p := range wrapLine(v.styled(ctx, l.Text), textW) {
					if len(out) == rows {
						break
					}
					g := " "
					if j == 0 {
						g = gutter(l)
					}
					out = append(out, g+" "+p)
				}
				continue
			}
			text := l.Text
			if v.xoff > 0 {
				text = ansi.Cut(text, v.xoff, v.xoff+textW+1)
			}
			out = append(out, gutter(l)+" "+layout.Truncate(v.styled(ctx, text), textW, th.Sym.Ellipsis))
		}
	}
	if v.Buf.Len() == 0 {
		out = append(out, th.Muted.Render(v.emptyText(ctx)))
	}
	for len(out) < rows {
		out = append(out, "")
	}
	out = append(out, " "+v.status(ctx, inner-1))
	title := v.Title + " " + ctx.Mark("log:close", th.Faint.Render("[esc]"))
	body := components.Panel(th, title, strings.Join(out, "\n"), w, h, true)
	if foot != "" {
		body += "\n" + foot
	}
	return body
}

func (v *LogView) emptyText(ctx *Context) string {
	switch v.State {
	case logs.Missing:
		return "Not created yet: the job may not have started writing. Retrying every 5 s."
	case logs.Unreadable:
		return "Not readable from this node (for example node-local /tmp). Use /shell to look inside the job."
	case logs.Failed:
		return "Cannot read the file: " + v.Err
	}
	if v.Static {
		return "(empty)"
	}
	return "(empty so far" + ctx.Theme.Sym.Ellipsis + ")"
}

// styled highlights search matches in a line.
func (v *LogView) styled(ctx *Context, text string) string {
	if v.matcher == nil {
		return text
	}
	spans := v.matcher.Spans(text)
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, s := range spans {
		b.WriteString(text[last:s[0]])
		b.WriteString(ctx.Theme.Match.Render(text[s[0]:s[1]]))
		last = s[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

func (v *LogView) status(ctx *Context, w int) string {
	th := ctx.Theme
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	var parts []string
	if v.Path != "" {
		parts = append(parts, tildify(v.Path))
	}
	n := v.Buf.Len()
	if n > 0 {
		end := min(v.top+v.height, n)
		parts = append(parts, fmt.Sprintf("lines %d-%d of %d", v.top+1, end, n))
	}
	if d := v.Buf.Dropped(); d > 0 {
		parts = append(parts, th.Muted.Render(fmt.Sprintf("%d older lines dropped", d)))
	}
	if !v.Static {
		if v.follow {
			parts = append(parts, th.OK.Render("following"))
		} else {
			parts = append(parts, th.Warn.Render("paused (F follows)"))
		}
	}
	if v.query != "" {
		parts = append(parts, "search: "+v.query)
	}
	if v.Note != "" {
		parts = append(parts, th.Muted.Render(v.Note))
	}
	if v.State == logs.Missing && n > 0 {
		parts = append(parts, th.Warn.Render("file gone"))
	}
	return layout.Truncate(strings.Join(parts, sep), w, th.Sym.Ellipsis)
}

// wrapLine splits a (possibly styled) line into rows of width w.
func wrapLine(s string, w int) []string {
	if ansi.StringWidth(s) <= w {
		return []string{s}
	}
	return strings.Split(ansi.Hardwrap(s, w, true), "\n")
}
