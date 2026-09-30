package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// helpBox lists every key binding, grouped by context, generated from the
// same bindings the footer uses.
func (a *App) helpBox(w, h int) string {
	th := a.th
	groups := a.keys.Groups()
	var cols []string
	colW := 30
	for _, g := range groups {
		var b strings.Builder
		b.WriteString(th.Bold.Render(g.Title) + "\n")
		for _, bd := range g.Bindings {
			if !bd.Enabled() {
				continue
			}
			k := strings.Join(bd.Keys(), "/")
			if len(k) > 12 || slices.Contains(bd.Keys(), "/") {
				k = strings.Join(bd.Keys(), " ")
			}
			if len(k) > 12 {
				k = bd.Help().Key
			}
			b.WriteString(th.Key.Render(layout.Pad(k, 12, false, th.Sym.Ellipsis)) + " " + th.Muted.Render(bd.Help().Desc) + "\n")
		}
		cols = append(cols, b.String())
	}
	perRow := max(1, min(4, (w-6)/colW))
	var rows []string
	for i := 0; i < len(cols); i += perRow {
		end := min(i+perRow, len(cols))
		var cells []string
		for _, c := range cols[i:end] {
			cells = append(cells, layout.FitLines(c, colW, strings.Count(c, "\n")+1))
		}
		rows = append(rows, joinBlocks(cells))
	}
	body := strings.Join(rows, "\n")
	body += "\n\n" + th.Muted.Render("Mouse: click tabs, rows, headers and hints; the wheel scrolls.")
	body += "\n" + th.Muted.Render("Hold Shift (Option in iTerm2) to select text while the mouse is on.")
	body += "\n" + th.Muted.Render("Commands: press / and type; tab completes. esc closes this help.")
	body = a.scrollWindow(body, h-6)
	box := components.Modal(th, "Keys", body, nil, min(w-2, perRow*colW+6))
	return layout.FitLines(box, min(w, layout.Width(layout.FirstLine(box))), min(h, strings.Count(box, "\n")+1))
}

// scrollWindow shows rows lines of body from the overlay's scroll offset,
// with a hint when more lines are hidden.
func (a *App) scrollWindow(body string, rows int) string {
	lines := strings.Split(body, "\n")
	if rows < 3 || len(lines) <= rows {
		a.overlayScroll = 0
		return body
	}
	rows--
	a.overlayScroll = min(max(a.overlayScroll, 0), len(lines)-rows)
	win := lines[a.overlayScroll : a.overlayScroll+rows]
	more := fmt.Sprintf("%s lines %d-%d of %d (j/k, pgup/pgdn scroll)", a.th.Sym.Arrow, a.overlayScroll+1, a.overlayScroll+rows, len(lines))
	return strings.Join(win, "\n") + "\n" + a.th.Faint.Render(more)
}

// joinBlocks puts multi-line blocks side by side, top-aligned.
func joinBlocks(blocks []string) string {
	split := make([][]string, len(blocks))
	height, widths := 0, make([]int, len(blocks))
	for i, b := range blocks {
		split[i] = strings.Split(b, "\n")
		height = max(height, len(split[i]))
		for _, l := range split[i] {
			widths[i] = max(widths[i], layout.Width(l))
		}
	}
	var out []string
	for r := range height {
		var line strings.Builder
		for i := range blocks {
			cell := ""
			if r < len(split[i]) {
				cell = split[i][r]
			}
			line.WriteString(layout.Pad(cell, widths[i], false, ""))
		}
		out = append(out, strings.TrimRight(line.String(), " "))
	}
	return strings.Join(out, "\n")
}

// debugBox shows the last commands, collector state and parse warnings.
func (a *App) debugBox(w, h int) string {
	th := a.th
	inner := max(w-8, 30)
	var b strings.Builder
	b.WriteString(th.Bold.Render("Collectors") + "\n")
	for _, s := range a.schedStates() {
		state := th.OK.Render("ok")
		switch {
		case s.Running:
			state = th.Info.Render("running")
		case s.Err != "":
			state = th.Crit.Render(layout.Truncate(layout.FirstLine(s.Err), 40, th.Sym.Ellipsis))
		case s.Done:
			state = th.Muted.Render("done")
		case s.Runs == 0:
			state = th.Muted.Render("waiting")
		}
		extra := ""
		if s.Backoff > 0 {
			extra += " backoff " + units.FormatShort(s.Backoff)
		}
		if s.Slowdown > 0 {
			extra += " slow " + units.FormatShort(s.Slowdown)
		}
		if !s.Visible {
			extra += " off-screen"
		}
		fmt.Fprintf(&b, "%-13s every %-6s runs %-4d took %-6s %s%s\n", s.Name,
			units.FormatShort(s.Interval), s.Runs, s.Took.Round(time.Millisecond), state, th.Muted.Render(extra))
	}
	if a.opt.Warnings != nil {
		if w := a.opt.Warnings(); len(w) > 0 {
			var names []string
			for k := range w {
				names = append(names, k)
			}
			slices.Sort(names)
			var parts []string
			for _, n := range names {
				parts = append(parts, fmt.Sprintf("%s %d", n, w[n]))
			}
			b.WriteString(th.Warn.Render("Unparsed lines: "+strings.Join(parts, ", ")) + "\n")
		}
	}
	var hist []execx.CallRecord
	if a.opt.History != nil {
		hist = a.opt.History()
	}
	b.WriteString(a.rateLine(hist) + "\n")
	b.WriteString("\n" + th.Bold.Render("Recent commands") + "\n")
	rows := max(h-8-strings.Count(b.String(), "\n"), 3)
	start := max(len(hist)-rows, 0)
	for i := len(hist) - 1; i >= start; i-- {
		r := hist[i]
		status := th.OK.Render(fmt.Sprintf("%3d", r.ExitCode))
		if r.Err != "" {
			status = th.Crit.Render(fmt.Sprintf("%3d", r.ExitCode))
		}
		line := fmt.Sprintf("%s %s %6s  %s", r.Start.Format("15:04:05"), status,
			r.Duration.Round(time.Millisecond), execx.Key(r.Argv))
		b.WriteString(layout.Truncate(line, inner, th.Sym.Ellipsis) + "\n")
	}
	if len(hist) == 0 {
		b.WriteString(th.Muted.Render("no commands yet") + "\n")
	}
	body := a.scrollWindow(strings.TrimRight(b.String(), "\n"), h-6)
	box := components.Modal(th, "Debug", body, nil, min(w-2, inner+6))
	return layout.FitLines(box, min(w, layout.Width(layout.FirstLine(box))), min(h, strings.Count(box, "\n")+1))
}
