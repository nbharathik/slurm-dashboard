package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

func calmTable() *Table {
	t := &Table{Name: "t", Focused: true, Cols: []layout.Column{
		{ID: "id", Title: "ID", Priority: 1, Min: 3},
		{ID: "name", Title: "NAME", Priority: 1, Min: 6, Max: 20, Flex: true},
		{ID: "free", Title: "FREE", Priority: 1, Min: 4, Right: true},
		{ID: "note", Title: "NOTE", Priority: 3, Min: 4, Max: 12},
	}}
	return t
}

func render(t *Table, w, h int) []string {
	th := theme.New(theme.Dark, true, true, false)
	return strings.Split(ansi.Strip(t.Render(th, nil, w, h)), "\n")
}

// A heading row has cells in the same columns as the rows under it, and is
// not counted as an item.
func TestHeadingRowSharesColumns(t *testing.T) {
	tb := calmTable()
	tb.SetRows([]Row{
		{ID: "h", Heading: true, Cells: map[string]string{"id": "▾ gpu", "name": "2 nodes", "free": "256/256"}},
		{ID: "a", Indent: 1, Cells: map[string]string{"id": "n1", "name": "alpha", "free": "128/128"}},
		{ID: "b", Indent: 1, Cells: map[string]string{"id": "n2", "name": "beta", "free": "128/128"}},
	})
	lines := render(tb, 60, 8)
	col := strings.Index(lines[0], "FREE") + len("FREE")
	for _, l := range lines[2:5] {
		if got := strings.TrimRight(ansi.Cut(l, 0, col), " "); !strings.HasSuffix(got, "128") && !strings.HasSuffix(got, "256") {
			t.Errorf("FREE cell does not end under its header: %q", l)
		}
	}
	if tb.Count() != 2 {
		t.Errorf("Count = %d, want 2 items", tb.Count())
	}
}

// A gap is a blank line the cursor steps over and nothing counts.
func TestGapRowsAreSkipped(t *testing.T) {
	item := func(id string) Row { return Row{ID: id, Cells: map[string]string{"id": id, "name": id, "free": "1"}} }
	tb := calmTable()
	tb.SetRows([]Row{item("a"), {Gap: true}, item("b"), {Gap: true}, item("c")})
	tb.Move(1)
	if tb.CursorID() != "b" {
		t.Fatalf("cursor on %q after Move(1), want b", tb.CursorID())
	}
	tb.Move(1)
	tb.Move(1)
	if tb.CursorID() != "c" {
		t.Errorf("cursor stopped on %q at the end, want c", tb.CursorID())
	}
	tb.Move(-2)
	if tb.CursorID() != "a" {
		t.Errorf("cursor on %q after Move(-2), want a", tb.CursorID())
	}
	tb.Cursor = 1 // a gap
	tb.Toggle()
	if len(tb.Selected) != 0 {
		t.Errorf("a gap was selected: %v", tb.Selected)
	}
	tb.SelectAll()
	if len(tb.Selected) != 3 || tb.Count() != 3 {
		t.Errorf("SelectAll selected %d of %d items", len(tb.Selected), tb.Count())
	}
	lines := render(tb, 40, 8)
	if strings.TrimSpace(lines[3]) != "" {
		t.Errorf("gap line is not blank: %q", lines[3])
	}
}

// The header has a thin rule under it, across the whole table.
func TestHeaderRule(t *testing.T) {
	tb := calmTable()
	tb.SetRows([]Row{{ID: "a", Cells: map[string]string{"id": "1", "name": "alpha", "free": "3/8"}}})
	lines := render(tb, 40, 5)
	if lines[1] != strings.Repeat("─", 40) {
		t.Errorf("no rule under the header: %q", lines[1])
	}
	th := theme.New(theme.Dark, true, true, true)
	if got := ansi.Strip(tb.Render(th, nil, 40, 5)); !strings.Contains(got, strings.Repeat("-", 40)) {
		t.Errorf("no ASCII rule: %q", got)
	}
}

// Every line has the same width, cells sit under their headers, and a
// right-aligned column ends where its header ends.
func TestCalmColumnsLineUp(t *testing.T) {
	tb := calmTable()
	tb.SetRows([]Row{
		{ID: "a", Cells: map[string]string{"id": "1", "name": "alpha", "free": "3/8", "note": "x"}},
		{ID: "b", Cells: map[string]string{"id": "22", "name": "béta", "free": "10/64", "note": "yy"}},
		{ID: "c", Cells: map[string]string{"id": "333", "name": "日本語", "free": "0/2"}},
	})
	lines := render(tb, 50, 6)
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != 50 {
			t.Errorf("line %d is %d wide, want 50: %q", i, got, l)
		}
	}
	head := lines[0]
	freeEnd := strings.Index(head, "FREE") + len("FREE")
	for _, l := range lines[2:5] {
		cut := ansi.Cut(l, 0, freeEnd)
		if !strings.HasSuffix(strings.TrimRight(cut, " "), "8") && !strings.HasSuffix(strings.TrimRight(cut, " "), "64") && !strings.HasSuffix(strings.TrimRight(cut, " "), "2") {
			t.Errorf("FREE cell does not end under the header: %q", l)
		}
	}
	// Two spaces between columns.
	if !strings.Contains(head, "ID   NAME") && !strings.Contains(head, "ID  NAME") {
		t.Errorf("columns are not two spaces apart: %q", head)
	}
}

func TestCalmDropsEmptyColumns(t *testing.T) {
	tb := calmTable()
	tb.SetRows([]Row{{ID: "a", Cells: map[string]string{"id": "1", "name": "alpha", "free": "3/8"}}})
	if head := render(tb, 50, 4)[0]; strings.Contains(head, "NOTE") {
		t.Errorf("a column with no data is shown: %q", head)
	}
	tb.SetRows([]Row{{ID: "a", Cells: map[string]string{"id": "1", "name": "alpha", "free": "3/8", "note": "hi"}}})
	if head := render(tb, 50, 4)[0]; !strings.Contains(head, "NOTE") {
		t.Errorf("a column with data is missing: %q", head)
	}
}

func TestCalmScrollPositionAndSortArrow(t *testing.T) {
	tb := calmTable()
	tb.SortCol = "free"
	var rows []Row
	for i := range 30 {
		rows = append(rows, Row{ID: string(rune('a'+i%26)) + strings.Repeat("x", i/26), Cells: map[string]string{"id": "1", "name": "n", "free": "1", "note": "z"}})
	}
	rows[3] = Row{ID: "sec", Heading: true, Cells: map[string]string{"id": "▾ group"}}
	tb.SetRows(rows)
	head := render(tb, 60, 6)[0]
	if !strings.Contains(head, "1-3 of 29") && !strings.Contains(head, "1-4 of 29") {
		t.Errorf("no scroll position over the data rows: %q", head)
	}
	if !strings.Contains(head, "FREE ▲") {
		t.Errorf("sort arrow missing or cut: %q", head)
	}
	tb.Move(29)
	if head := render(tb, 60, 6)[0]; !strings.Contains(head, "of 29") || strings.Contains(head, "1-") {
		t.Errorf("scrolled position: %q", head)
	}
}

func TestSetRowsDoesNotChangeSharedColumns(t *testing.T) {
	shared := []layout.Column{{ID: "a", Title: "A", Priority: 1, Min: 1}}
	tb := &Table{Cols: shared}
	tb.SetRows([]Row{{ID: "1", Cells: map[string]string{"a": "a long value"}}})
	if shared[0].Want != 0 {
		t.Errorf("the caller's column list was changed: %+v", shared[0])
	}
}
