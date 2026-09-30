package components

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// Row is one table row. Cells are pre-rendered (possibly styled) strings
// keyed by column ID.
type Row struct {
	ID     string
	Cells  map[string]string
	Indent int // child rows of a group are indented
	Muted  bool
	// Heading marks a group's own row (a partition, a state): it has cells
	// like any row, laid out in the same columns, but it is not an item, so
	// it is left out of counts and scroll positions.
	Heading bool
	// Gap is a blank line between groups: never selected, never counted,
	// and the cursor steps over it.
	Gap bool
}

// Table is a scrollable, virtualised table: only the visible rows are
// rendered, so 10,000 rows cost the same as 30.
type Table struct {
	Name     string // zone prefix; must be unique per table
	Cols     []layout.Column
	Rows     []Row
	Cursor   int
	Offset   int
	Selected map[string]bool
	SortCol  string
	SortDesc bool
	Focused  bool
	Empty    string // text shown when there are no rows

	fitW   int
	fitted []layout.Fitted
	height int // rows visible at the last render
	shown  []layout.Column
}

// SetRows replaces the rows, keeping the cursor on the same row ID; when
// that row is gone the cursor stays at the nearest position.
func (t *Table) SetRows(rows []Row) {
	cur := t.CursorID()
	t.Rows = rows
	t.fitW = -1
	t.Cols = slices.Clone(t.Cols) // the caller's column list is shared; Want is ours
	used := map[string]bool{}
	for i := range t.Cols {
		title := layout.Width(t.Cols[i].Title) + sortMarkWidth(t, t.Cols[i].ID)
		t.Cols[i].Want = title
		if t.Cols[i].Max > 0 {
			t.Cols[i].Max = max(t.Cols[i].Max, title) // room for the sort arrow
		}
	}
	for _, r := range rows {
		if r.Gap {
			continue
		}
		for i, c := range t.Cols {
			if w := layout.Width(r.Cells[c.ID]) + r.Indent*indentWidth(c.ID, t); w > t.Cols[i].Want {
				t.Cols[i].Want = w
			}
			if ansi.Strip(r.Cells[c.ID]) != "" {
				used[c.ID] = true
			}
		}
	}
	t.shown = t.shown[:0]
	for _, c := range t.Cols {
		if c.Priority > 1 && !used[c.ID] && len(rows) > 0 {
			continue // nothing to show in this column
		}
		t.shown = append(t.shown, c)
	}
	if t.Selected != nil {
		keep := map[string]bool{}
		for _, r := range rows {
			if t.Selected[r.ID] {
				keep[r.ID] = true
			}
		}
		t.Selected = keep
	}
	if cur != "" {
		for i, r := range rows {
			if r.ID == cur {
				t.Cursor = i
				break
			}
		}
	}
	t.clamp()
}

func sortMarkWidth(t *Table, id string) int {
	if t.SortCol == id {
		return 2
	}
	return 0
}

func indentWidth(colID string, t *Table) int {
	if len(t.Cols) > 0 && t.Cols[0].ID == colID {
		return 2
	}
	return 0
}

// CursorID returns the ID of the row under the cursor, or "".
func (t *Table) CursorID() string {
	if t.Cursor >= 0 && t.Cursor < len(t.Rows) {
		return t.Rows[t.Cursor].ID
	}
	return ""
}

// SetCursorID moves the cursor to the row with id, if present.
func (t *Table) SetCursorID(id string) bool {
	for i, r := range t.Rows {
		if r.ID == id {
			t.Cursor = i
			t.clamp()
			return true
		}
	}
	return false
}

// Move moves the cursor by delta rows, stepping over gaps.
func (t *Table) Move(delta int) {
	dir := 1
	if delta < 0 {
		dir, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		next := t.Cursor + dir
		for next >= 0 && next < len(t.Rows) && t.Rows[next].Gap {
			next += dir
		}
		if next < 0 || next >= len(t.Rows) {
			break
		}
		t.Cursor = next
	}
	t.clamp()
}

// Page moves the cursor by a screen of rows.
func (t *Table) Page(dir int) { t.Move(dir * max(t.height-1, 1)) }

// Home moves to the first row.
func (t *Table) Home() { t.Cursor = 0; t.clamp() }

// End moves to the last row.
func (t *Table) End() { t.Cursor = len(t.Rows) - 1; t.clamp() }

func (t *Table) clamp() {
	if t.Cursor >= len(t.Rows) {
		t.Cursor = len(t.Rows) - 1
	}
	if t.Cursor < 0 {
		t.Cursor = 0
	}
	t.stepOffGap()
	if t.height > 0 {
		if t.Cursor < t.Offset {
			t.Offset = t.Cursor
		}
		if t.Cursor >= t.Offset+t.height {
			t.Offset = t.Cursor - t.height + 1
		}
	}
	if t.Offset > max(len(t.Rows)-t.height, 0) {
		t.Offset = max(len(t.Rows)-t.height, 0)
	}
	if t.Offset < 0 {
		t.Offset = 0
	}
}

// stepOffGap moves a cursor that landed on a gap to the next row, or the
// previous one when there is none.
func (t *Table) stepOffGap() {
	if t.Cursor >= len(t.Rows) || !t.Rows[t.Cursor].Gap {
		return
	}
	for i := t.Cursor; i < len(t.Rows); i++ {
		if !t.Rows[i].Gap {
			t.Cursor = i
			return
		}
	}
	for i := t.Cursor; i >= 0; i-- {
		if !t.Rows[i].Gap {
			t.Cursor = i
			return
		}
	}
}

// Toggle selects or deselects the cursor row.
func (t *Table) Toggle() {
	id := t.CursorID()
	if id == "" || !t.Rows[t.Cursor].item() {
		return
	}
	if t.Selected == nil {
		t.Selected = map[string]bool{}
	}
	if t.Selected[id] {
		delete(t.Selected, id)
	} else {
		t.Selected[id] = true
	}
}

// SelectAll selects every item row, or clears the selection when all of
// them are already selected.
func (t *Table) SelectAll() {
	if t.Selected != nil && len(t.Selected) == t.Count() && len(t.Rows) > 0 {
		t.Selected = map[string]bool{}
		return
	}
	t.Selected = map[string]bool{}
	for _, r := range t.Rows {
		if r.item() {
			t.Selected[r.ID] = true
		}
	}
}

// SelectedIDs returns the selection in row order, or the cursor row when
// nothing is selected.
func (t *Table) SelectedIDs() []string {
	var out []string
	for _, r := range t.Rows {
		if t.Selected[r.ID] {
			out = append(out, r.ID)
		}
	}
	if len(out) == 0 && t.CursorID() != "" {
		out = []string{t.CursorID()}
	}
	return out
}

// VisibleColumns returns the columns chosen at the last render.
func (t *Table) VisibleColumns() []layout.Fitted { return t.fitted }

// RowZone is the zone ID of a row.
func (t *Table) RowZone(id string) string { return t.Name + ":row:" + id }

// ColZone is the zone ID of a column header.
func (t *Table) ColZone(id string) string { return t.Name + ":col:" + id }

// Render draws the header, a thin rule under it and the visible rows into
// w×h cells. Text starts two cells in, after the cursor column.
func (t *Table) Render(th theme.Theme, zm *zone.Manager, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	const sep = "  "
	cols := t.Cols
	if len(t.shown) > 0 {
		cols = t.shown
	}
	if t.fitW != w || t.fitted == nil {
		t.fitted = layout.Fit(cols, w-2, len(sep))
		t.fitW = w
	}
	t.height = max(h-2, 1) // the header and its rule
	t.clamp()

	var b strings.Builder
	head := make([]string, 0, len(t.fitted))
	for _, f := range t.fitted {
		title := f.Title
		if t.SortCol == f.ID {
			if t.SortDesc {
				title += " ▼"
			} else {
				title += " ▲"
			}
			if th.Sym.ASCII {
				title = strings.NewReplacer("▼", "v", "▲", "^").Replace(title)
			}
		}
		cell := layout.Pad(title, f.Width, f.Right, th.Sym.Ellipsis)
		head = append(head, mark(zm, t.ColZone(f.ID), th.ColHead.Render(cell)))
	}
	line := "  " + strings.Join(head, sep)
	if info := t.position(); info != "" {
		if room := w - layout.Width(line) - layout.Width(info) - 1; room >= 1 {
			line += strings.Repeat(" ", room) + th.Faint.Render(info)
		}
	}
	b.WriteString(line + "\n" + th.HRule(w))

	if len(t.Rows) == 0 {
		msg := t.Empty
		if msg == "" {
			msg = "nothing to show"
		}
		b.WriteString("\n" + th.Muted.Render("  "+layout.Truncate(msg, w-2, th.Sym.Ellipsis)))
		return layout.FitLines(b.String(), w, h)
	}

	end := min(t.Offset+t.height, len(t.Rows))
	for i := t.Offset; i < end; i++ {
		r := t.Rows[i]
		if r.Gap {
			b.WriteString("\n")
			continue
		}
		prefix := "  "
		switch {
		case i == t.Cursor && t.Focused:
			prefix = th.Accent.Render(th.Sym.Cursor) + " "
		case t.Selected[r.ID]:
			prefix = th.Accent.Render("+") + " "
		}
		cells := make([]string, 0, len(t.fitted))
		for ci, f := range t.fitted {
			v := r.Cells[f.ID]
			if ci == 0 && r.Indent > 0 {
				v = strings.Repeat(" ", 2*r.Indent) + v
			}
			if r.Muted {
				v = th.Muted.Render(ansi.Strip(v))
			}
			cells = append(cells, layout.Pad(v, f.Width, f.Right, th.Sym.Ellipsis))
		}
		line := strings.Join(cells, sep)
		if t.Selected[r.ID] && (i != t.Cursor || !t.Focused) {
			line = th.Accent.Render(ansi.Strip(line))
		}
		if i == t.Cursor && t.Focused {
			line = th.Selected.Render(ansi.Strip(line))
		}
		b.WriteString("\n" + mark(zm, t.RowZone(r.ID), prefix+line))
	}
	return layout.FitLines(b.String(), w, h)
}

// item reports whether a row is a real item: not a heading or a gap.
func (r Row) item() bool { return !r.Heading && !r.Gap }

// Count is the number of item rows.
func (t *Table) Count() int {
	n := 0
	for _, r := range t.Rows {
		if r.item() {
			n++
		}
	}
	return n
}

// position is "12-30 of 44" over the item rows when some are scrolled out
// of sight, else "".
func (t *Table) position() string {
	end := min(t.Offset+t.height, len(t.Rows))
	if len(t.Rows) == 0 || (t.Offset == 0 && end == len(t.Rows)) {
		return ""
	}
	first, last, total := 0, 0, 0
	for i, r := range t.Rows {
		if !r.item() {
			continue
		}
		total++
		if i < t.Offset {
			first++
		}
		if i < end {
			last++
		}
	}
	return strconv.Itoa(min(first+1, total)) + "-" + strconv.Itoa(last) + " of " + strconv.Itoa(total)
}

// ScrollInfo returns "a–b of n" for the visible range.
func (t *Table) ScrollInfo() string {
	if len(t.Rows) == 0 {
		return "0 of 0"
	}
	end := min(t.Offset+max(t.height, 1), len(t.Rows))
	return strconv.Itoa(t.Offset+1) + "-" + strconv.Itoa(end) + " of " + strconv.Itoa(len(t.Rows))
}

func mark(zm *zone.Manager, id, s string) string {
	if zm == nil {
		return s
	}
	return zm.Mark(id, s)
}
