// Package layout picks the breakpoint and drops or sizes table columns to fit the terminal.
package layout

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Mode is a layout breakpoint.
type Mode int

// Breakpoints.
const (
	TooSmall Mode = iota // < 40 cols or < 12 rows
	Narrow               // 40–59
	Compact              // 60–99
	Normal               // 100–139
	Wide                 // ≥ 140
)

// Minimum usable terminal size.
const (
	MinWidth  = 40
	MinHeight = 12
)

// ModeFor returns the breakpoint for a terminal size.
func ModeFor(w, h int) Mode {
	switch {
	case w < MinWidth || h < MinHeight:
		return TooSmall
	case w < 60:
		return Narrow
	case w < 100:
		return Compact
	case w < 140:
		return Normal
	}
	return Wide
}

func (m Mode) String() string {
	return [...]string{"too-small", "narrow", "compact", "normal", "wide"}[m]
}

// Margin is the blank space kept at each side of the screen: two cells on
// ordinary terminals, one on small ones.
func Margin(w int) int {
	if w >= 80 {
		return 2
	}
	return 1
}

// Indent prefixes every line of s with n spaces.
func Indent(s string, n int) string {
	if n <= 0 {
		return s
	}
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

// Column describes one table column.
type Column struct {
	ID       string
	Title    string
	Priority int  // 1 = never dropped; higher numbers drop first
	Min      int  // minimum width
	Max      int  // maximum width; 0 = no limit
	Want     int  // preferred width (from content); 0 = Min
	Flex     bool // takes spare width
	Right    bool // right-aligned
}

// Fitted is a column chosen for display with its width.
type Fitted struct {
	Column
	Width int
}

// Fit chooses columns and widths to fit width w with gap spaces between them.
// Lowest-priority (rightmost first) columns drop first; priority-1 never do.
func Fit(cols []Column, w, gap int) []Fitted {
	active := append([]Column(nil), cols...)
	need := func(cs []Column) int {
		n := 0
		for _, c := range cs {
			if c.Flex {
				n += max(c.Min, 1)
			} else {
				n += preferred(c)
			}
		}
		return n + gap*max(len(cs)-1, 0)
	}
	for need(active) > w {
		drop, worst := -1, 1
		for i, c := range active {
			if c.Priority >= worst && c.Priority > 1 {
				drop, worst = i, c.Priority
			}
		}
		if drop < 0 {
			break // only priority-1 columns left; they get squeezed below
		}
		active = append(active[:drop], active[drop+1:]...)
	}

	out := make([]Fitted, len(active))
	total := gap * max(len(active)-1, 0)
	for i, c := range active {
		width := preferred(c)
		out[i] = Fitted{Column: c, Width: width}
		total += width
	}
	// Too wide: shrink flexible columns first, then the rest, to their minimum.
	for _, flexFirst := range []bool{true, false} {
		for i := len(out) - 1; i >= 0 && total > w; i-- {
			if out[i].Flex != flexFirst {
				continue
			}
			floor := max(out[i].Min, 1)
			cut := min(out[i].Width-floor, total-w)
			if cut > 0 {
				out[i].Width -= cut
				total -= cut
			}
		}
	}
	// Still too wide (only essential columns at minimum): squeeze the last.
	if total > w && len(out) > 0 {
		last := len(out) - 1
		out[last].Width = max(out[last].Width-(total-w), 1)
		total = w
	}
	// Spare width goes to flexible columns, each up to its Max; what is left
	// stays blank rather than stretching a column past its useful width.
	spare := w - total
	for spare > 0 {
		var open []int
		for i, f := range out {
			if f.Flex && (f.Max == 0 || f.Width < f.Max) {
				open = append(open, i)
			}
		}
		if len(open) == 0 {
			break
		}
		share := max(spare/len(open), 1)
		for _, i := range open {
			add := min(share, spare)
			if out[i].Max > 0 {
				add = min(add, out[i].Max-out[i].Width)
			}
			out[i].Width += add
			spare -= add
		}
	}
	return out
}

// preferred is a column's natural width: Want, at least Min, at most Max.
func preferred(c Column) int {
	width := max(c.Want, c.Min, 1)
	if c.Max > 0 {
		width = min(width, max(c.Max, c.Min))
	}
	return width
}

// Truncate shortens s to w cells, ending with tail when cut.
func Truncate(s string, w int, tail string) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, tail)
}

// Pad pads or truncates s to exactly w cells, left- or right-aligned.
func Pad(s string, w int, right bool, tail string) string {
	s = Truncate(s, w, tail)
	gap := w - ansi.StringWidth(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

// Width is the display width of s, ignoring ANSI sequences.
func Width(s string) int { return ansi.StringWidth(s) }

// FitLines truncates or pads every line of s to w cells and pads to h
// lines, so a block exactly fills its box.
func FitLines(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, l := range lines {
		lines[i] = Pad(l, w, false, "")
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return strings.Join(lines, "\n")
}

// Overlay draws box centred over base (both multi-line strings), keeping
// base visible around it. Width is measured in cells and ANSI-aware.
func Overlay(base, box string, w, h int) string {
	bw, bh := lipgloss.Size(box)
	x, y := max((w-bw)/2, 0), max((h-bh)/2, 0)
	return OverlayAt(base, box, x, y)
}

// OverlayAt draws box over base with its top-left corner at (x, y).
func OverlayAt(base, box string, x, y int) string {
	lines := strings.Split(base, "\n")
	for i, bl := range strings.Split(box, "\n") {
		row := y + i
		if row < 0 {
			continue
		}
		for row >= len(lines) {
			lines = append(lines, "")
		}
		line := lines[row]
		lw := ansi.StringWidth(line)
		if lw < x {
			line += strings.Repeat(" ", x-lw)
		}
		left := ansi.Cut(line, 0, x)
		right := ansi.Cut(line, x+ansi.StringWidth(bl), max(lw, x+ansi.StringWidth(bl)))
		lines[row] = left + "\x1b[0m" + bl + "\x1b[0m" + right
	}
	return strings.Join(lines, "\n")
}

// FirstLine returns s up to its first newline.
func FirstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// Wrap word-wraps s at w cells, keeping indented lines as is; no-op below 10.
func Wrap(s string, w int) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if w < 10 || Width(line) <= w || strings.HasPrefix(line, "  ") {
			out = append(out, line)
			continue
		}
		cur := ""
		for _, word := range strings.Fields(line) {
			switch {
			case cur == "":
				cur = word
			case Width(cur)+1+Width(word) > w:
				out = append(out, cur)
				cur = word
			default:
				cur += " " + word
			}
		}
		out = append(out, cur)
	}
	return out
}
