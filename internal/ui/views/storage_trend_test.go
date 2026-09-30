package views

import "testing"

func TestFillIn(t *testing.T) {
	for _, c := range []struct {
		days        float64
		short, long string
	}{
		{0.4, "<1d", "within a day"},
		{1, "~1d", "in about 1 day"},
		{19.2, "~19d", "in about 19 days"},
		{99, "~99d", "in about 99 days"},
		{190, "~6mo", "in about 6 months"},
		{364, "~12mo", "in about 12 months"},
	} {
		if got := fillIn(c.days, true); got != c.short {
			t.Errorf("fillIn(%v, short) = %q, want %q", c.days, got, c.short)
		}
		if got := fillIn(c.days, false); got != c.long {
			t.Errorf("fillIn(%v) = %q, want %q", c.days, got, c.long)
		}
	}
}
