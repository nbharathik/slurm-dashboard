package layout

import (
	"strings"
	"testing"
)

func TestModeFor(t *testing.T) {
	cases := map[[2]int]Mode{
		{39, 30}: TooSmall, {80, 11}: TooSmall, {40, 12}: Narrow, {59, 20}: Narrow,
		{60, 20}: Compact, {99, 30}: Compact, {100, 30}: Normal, {139, 40}: Normal, {140, 40}: Wide, {200, 60}: Wide,
	}
	for size, want := range cases {
		if got := ModeFor(size[0], size[1]); got != want {
			t.Errorf("ModeFor(%v) = %v, want %v", size, got, want)
		}
	}
	if Wide.String() != "wide" || TooSmall.String() != "too-small" {
		t.Fatal("String")
	}
}

func jobColumns() []Column {
	return []Column{
		{ID: "id", Priority: 1, Min: 6, Want: 10},
		{ID: "name", Priority: 1, Min: 8, Want: 30, Flex: true},
		{ID: "state", Priority: 1, Min: 6, Want: 8},
		{ID: "time", Priority: 2, Min: 11, Want: 16},
		{ID: "gpu", Priority: 2, Min: 3, Want: 3, Right: true},
		{ID: "reason", Priority: 2, Min: 8, Want: 30, Flex: true},
		{ID: "partition", Priority: 3, Min: 5, Want: 9},
		{ID: "cpus", Priority: 3, Min: 4, Want: 4},
		{ID: "account", Priority: 5, Min: 7, Want: 10},
	}
}

func ids(fs []Fitted) string {
	var s []string
	for _, f := range fs {
		s = append(s, f.ID)
	}
	return strings.Join(s, ",")
}

func total(fs []Fitted, gap int) int {
	n := gap * (len(fs) - 1)
	for _, f := range fs {
		n += f.Width
	}
	return n
}

func TestFitDropsLowPriorityFirst(t *testing.T) {
	for _, tc := range []struct {
		w    int
		want string
	}{
		{200, "id,name,state,time,gpu,reason,partition,cpus,account"},
		{80, "id,name,state,time,gpu,reason,partition,cpus"},
		{60, "id,name,state,time,gpu,reason"},
		{45, "id,name,state,time"},
		{24, "id,name,state"},
	} {
		got := Fit(jobColumns(), tc.w, 1)
		if ids(got) != tc.want {
			t.Errorf("Fit(%d) = %s, want %s", tc.w, ids(got), tc.want)
		}
		if n := total(got, 1); n > tc.w {
			t.Errorf("Fit(%d) total width %d overflows", tc.w, n)
		}
	}
}

func TestFitFlexAbsorbsSpace(t *testing.T) {
	got := Fit(jobColumns(), 200, 1)
	if total(got, 1) != 200 {
		t.Fatalf("width not filled: %d", total(got, 1))
	}
	for _, f := range got {
		if f.ID == "name" && f.Width <= 30 {
			t.Fatalf("name should absorb spare width: %d", f.Width)
		}
		if f.ID == "gpu" && f.Width != 3 {
			t.Fatalf("fixed columns keep their width: %+v", f)
		}
	}
	tight := Fit(jobColumns()[:3], 20, 1)
	if total(tight, 1) > 20 {
		t.Fatalf("squeezed essentials overflow: %+v", tight)
	}
	capped := Fit([]Column{{ID: "a", Priority: 1, Min: 2, Want: 50, Max: 10}}, 30, 1)
	if capped[0].Width != 10 {
		t.Fatalf("max width: %+v", capped)
	}
	if len(Fit(nil, 10, 1)) != 0 {
		t.Fatal("empty")
	}
}

func TestTruncatePad(t *testing.T) {
	if got := Truncate("train-lora-long-name", 8, "…"); got != "train-l…" || Width(got) != 8 {
		t.Fatalf("Truncate = %q", got)
	}
	if Truncate("abc", 0, "…") != "" || Truncate("abc", 5, "…") != "abc" {
		t.Fatal("Truncate edges")
	}
	if got := Pad("ab", 5, true, "…"); got != "   ab" {
		t.Fatalf("right pad = %q", got)
	}
	if got := Pad("\x1b[31mred\x1b[0m", 5, false, ""); Width(got) != 5 || !strings.HasPrefix(got, "\x1b[31m") {
		t.Fatalf("ANSI-aware pad = %q", got)
	}
	if got := Pad("数据集", 4, false, "…"); Width(got) != 4 {
		t.Fatalf("wide runes: %q (%d)", got, Width(got))
	}
	if got := FitLines("a\nbb\nccc", 2, 4); got != "a \nbb\ncc\n  " {
		t.Fatalf("FitLines = %q", got)
	}
}

func TestOverlay(t *testing.T) {
	base := strings.Join([]string{"..........", "..........", "..........", ".........."}, "\n")
	got := Overlay(base, "XX\nYY", 10, 4)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 || Width(lines[1]) != 10 || !strings.Contains(lines[1], "XX") || !strings.Contains(lines[2], "YY") {
		t.Fatalf("Overlay:\n%s", got)
	}
	if stripped := ansiStrip(lines[1]); stripped != "....XX...." {
		t.Fatalf("overlay position: %q", stripped)
	}
	short := OverlayAt("ab", "ZZ", 5, 2)
	if ansiStrip(strings.Split(short, "\n")[2]) != "     ZZ" {
		t.Fatalf("overlay past the end: %q", short)
	}
}

func ansiStrip(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestFitFlexStopsAtMax(t *testing.T) {
	cols := []Column{
		{ID: "id", Priority: 1, Min: 4, Want: 4},
		{ID: "name", Priority: 1, Min: 6, Want: 10, Max: 20, Flex: true},
		{ID: "where", Priority: 1, Min: 6, Want: 8, Max: 12, Flex: true},
	}
	got := Fit(cols, 200, 2)
	if got[1].Width != 20 || got[2].Width != 12 {
		t.Errorf("flex columns should stop at Max: %+v", got)
	}
	if n := total(got, 2); n > 200 {
		t.Errorf("total %d overflows", n)
	}
	// With room short of Max, the space is shared.
	got = Fit(cols, 40, 2)
	if total(got, 2) != 40 {
		t.Errorf("width not used: %d", total(got, 2))
	}
}
