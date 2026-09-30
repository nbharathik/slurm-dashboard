package components

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// Gauge renders a bar of width cells filled to frac (0..1), coloured by
// level: under 70% normal, 70–90% warning, 90% and above critical.
func Gauge(th theme.Theme, frac float64, width int) string {
	if width <= 0 {
		return ""
	}
	if math.IsNaN(frac) || frac < 0 {
		return th.Faint.Render(strings.Repeat(th.Sym.Empty, width))
	}
	frac = math.Min(frac, 1)
	n := int(math.Round(frac * float64(width)))
	if frac > 0 && n == 0 {
		n = 1
	}
	return th.Level(frac).Render(strings.Repeat(th.Sym.Filled, n)) + th.Faint.Render(strings.Repeat(th.Sym.Empty, width-n))
}

// QuotaGauge renders a storage bar filled to pct percent and coloured by
// the [alerts] thresholds, so bar colours and alerts agree.
func QuotaGauge(th theme.Theme, pct, warn, crit, width int) string {
	if pct < 0 {
		return Gauge(th, -1, width)
	}
	style := th.OK
	switch {
	case pct >= crit:
		style = th.Crit
	case pct >= warn:
		style = th.Warn
	}
	n := min((pct*width+50)/100, width)
	if pct > 0 && n == 0 {
		n = 1
	}
	return style.Render(strings.Repeat(th.Sym.Filled, n)) + th.Faint.Render(strings.Repeat(th.Sym.Empty, width-n))
}

// Percent renders frac as "71%", or "-" when unknown.
func Percent(th theme.Theme, frac float64) string {
	if frac < 0 || math.IsNaN(frac) {
		return th.Faint.Render("-")
	}
	return fmt.Sprintf("%d%%", int(math.Round(frac*100)))
}

// Dash is the placeholder for unknown values.
func Dash(th theme.Theme) string { return th.Faint.Render("-") }

// GPUCells draws one cell per GPU: the user's, other users', free and
// unavailable. Above maxCells it falls back to counts.
func GPUCells(th theme.Theme, mine, others, free, total, maxCells int) string {
	if total <= 0 {
		return Dash(th)
	}
	if total > maxCells {
		return fmt.Sprintf("%s %d/%d", th.OK.Render(fmt.Sprintf("%d free", free)), total-free, total)
	}
	unavailable := max(total-mine-others-free, 0)
	var b strings.Builder
	b.WriteString(th.Accent.Render(strings.Repeat(th.Sym.GPUMine, min(mine, total))))
	b.WriteString(th.Muted.Render(strings.Repeat(th.Sym.GPUOther, min(others, total-mine))))
	b.WriteString(th.OK.Render(strings.Repeat(th.Sym.GPUFree, free)))
	b.WriteString(th.Faint.Render(strings.Repeat(th.Sym.GPUDown, unavailable)))
	return b.String()
}

// Panel draws content in a bordered box of exactly w×h cells with a title
// in the top border.
func Panel(th theme.Theme, title string, content string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	style := th.Panel
	if focused {
		style = th.PanelFoc
	}
	inner := layout.FitLines(content, w-2, h-2)
	box := style.Width(w).Height(h).Render(inner)
	if title == "" {
		return box
	}
	// Put the title into the top border: ┌ Title ───┐
	lines := strings.Split(box, "\n")
	t := " " + layout.Truncate(title, w-6, th.Sym.Ellipsis) + " "
	head := th.PanelHead.Render(t)
	top := lines[0]
	lines[0] = layout.OverlayAt(top, head, 2, 0)
	return strings.Join(lines, "\n")
}

// PaddedPanel is Panel with one blank column inside each border, for
// text content; the content area is w-4 wide.
func PaddedPanel(th theme.Theme, title string, content string, w, h int, focused bool) string {
	lines := strings.Split(layout.FitLines(content, max(w-4, 0), max(h-2, 0)), "\n")
	for i, l := range lines {
		lines[i] = " " + l + " "
	}
	return Panel(th, title, strings.Join(lines, "\n"), w, h, focused)
}

// Modal renders a centred dialog box with a title, a body and buttons.
func Modal(th theme.Theme, title, body string, buttons []string, width int) string {
	width = max(width, 20)
	var b strings.Builder
	b.WriteString(th.Bold.Render(title))
	b.WriteString("\n\n")
	b.WriteString(body)
	if len(buttons) > 0 {
		b.WriteString("\n\n")
		b.WriteString(lipgloss.PlaceHorizontal(width-6, lipgloss.Right, strings.Join(buttons, "  ")))
	}
	return th.Modal.Width(width).Render(b.String())
}

// Button renders a clickable button label, highlighted when focused.
func Button(th theme.Theme, label string, focused, danger bool) string {
	text := "[ " + label + " ]"
	switch {
	case focused && danger:
		return th.Crit.Reverse(true).Render(text)
	case focused:
		return th.Selected.Render(text)
	case danger:
		return th.Crit.Render(text)
	}
	return th.Muted.Render(text)
}

// Sparkline draws vals one cell each, scaled min to max; NaN is blank, a flat line is mid height.
func Sparkline(th theme.Theme, vals []float64) string {
	steps := []rune(th.Sym.Spark)
	if len(steps) == 0 || len(vals) == 0 {
		return ""
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		if !math.IsNaN(v) {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	var b strings.Builder
	for _, v := range vals {
		switch {
		case math.IsNaN(v):
			b.WriteByte(' ')
		case hi == lo:
			b.WriteRune(steps[len(steps)/2])
		default:
			b.WriteRune(steps[int(math.Round((v-lo)/(hi-lo)*float64(len(steps)-1)))])
		}
	}
	return b.String()
}
