package views

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// detailLayout keeps the list visible beside a card whenever both fit.
func detailLayout(w, h int, list, card func(int, int) string) string {
	if w >= 136 {
		right := min(max(w*2/5, 56), 76)
		left := w - right - 1
		return joinH(list(left, h), left+1, card(right, h))
	}
	if w >= 80 && h >= 18 {
		rows := max(h/3, 6)
		return list(w, rows) + "\n" + card(w, h-rows)
	}
	return card(w, h)
}

type detailPane struct {
	id, zone          string
	offset, maxOffset int
}

func (p *detailPane) update(ctx *Context, msg tea.Msg) bool {
	step := 0
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(m, ctx.Keys.DetailUp):
			step = -1
		case key.Matches(m, ctx.Keys.DetailDown):
			step = 1
		default:
			return false
		}
	case tea.MouseWheelMsg:
		if p.zone == "" || !ctx.InZone(p.zone, m) {
			return false
		}
		switch m.Button {
		case tea.MouseWheelUp:
			step = -3
		case tea.MouseWheelDown:
			step = 3
		default:
			return false
		}
	default:
		return false
	}
	p.offset = min(max(p.offset+step, 0), p.maxOffset)
	return true
}

func (p *detailPane) render(ctx *Context, id, title, closeZone, content string, w, h int) string {
	if p.id != id {
		p.id, p.offset = id, 0
	}
	p.zone = closeZone + ":pane"
	inner, rows := max(w-4, 1), max(h-2, 0)
	lines := strings.Split(ansi.Wrap(content, inner, "/"), "\n")
	if len(lines) > rows && rows > 1 {
		rows--
	}
	p.maxOffset = max(len(lines)-rows, 0)
	p.offset = min(p.offset, p.maxOffset)
	body := strings.Join(lines[p.offset:min(p.offset+rows, len(lines))], "\n")
	if p.maxOffset > 0 && h > 3 {
		hint := fmt.Sprintf("%d-%d/%d  alt+up/down scroll", p.offset+1, min(p.offset+rows, len(lines)), len(lines))
		body += "\n" + ctx.Theme.Faint.Render(layout.Truncate(hint, inner, ""))
	}
	head := layout.Truncate(title, max(w-12, 1), ctx.Theme.Sym.Ellipsis) + " " + ctx.Mark(closeZone, ctx.Theme.Faint.Render("[esc]"))
	return ctx.Mark(p.zone, components.PaddedPanel(ctx.Theme, head, body, w, h, false))
}
