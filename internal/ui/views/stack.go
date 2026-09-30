package views

import "strings"

// block is one panel in a vertical stack: it gets at least min rows (or is
// dropped), then up to want, and the grow block takes what is left.
type block struct {
	min, want int
	render    func(w, h int) string
}

// stack lays blocks out top to bottom in w×h. Blocks that do not fit at
// their minimum are dropped from the end.
func stack(blocks []block, w, h, grow int) string {
	for len(blocks) > 0 {
		total := 0
		for _, b := range blocks {
			total += b.min
		}
		if total <= h {
			break
		}
		blocks = blocks[:len(blocks)-1]
	}
	if len(blocks) == 0 {
		return ""
	}
	heights := make([]int, len(blocks))
	left := h
	for i, b := range blocks {
		heights[i] = b.min
		left -= b.min
	}
	for i, b := range blocks {
		add := min(max(b.want-heights[i], 0), left)
		heights[i] += add
		left -= add
	}
	if grow >= 0 && grow < len(blocks) {
		heights[grow] += left
	}
	parts := make([]string, 0, len(blocks))
	for i, b := range blocks {
		parts = append(parts, b.render(w, heights[i]))
	}
	return strings.Join(parts, "\n")
}

// sideBySide splits w between two renderers at frac.
func sideBySide(w, h int, frac float64, a, b func(w, h int) string) string {
	wa := int(float64(w) * frac)
	return joinH(a(wa, h), wa, b(w-wa, h))
}
