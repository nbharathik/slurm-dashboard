package ui

import (
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Enter runs the highlighted suggestion when the typed text is only its start.
func TestPaletteEnterRunsSuggestion(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	press(a, ":")
	for _, r := range "tab que" {
		press(a, string(r))
	}
	press(a, "enter")
	if a.palette != nil || a.views[a.tab].Name() != model.TabQueue {
		t.Fatalf("on %s, palette open %v", a.views[a.tab].Name(), a.palette != nil)
	}
}

// n on a running job opens its node; the node card then lists the job.
func TestJobOpensItsNode(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 140, 40, false)
	press(a, "2", "n") // 812 runs on gpu01
	if a.views[a.tab].Name() != model.TabNodes {
		t.Fatalf("n opened %s", a.views[a.tab].Name())
	}
	if s := screen(a); !strings.Contains(s, "gpu01") || !strings.Contains(s, "812") {
		t.Fatalf("node card:\n%s", s)
	}
}

// Help starts with the keys of the tab it was opened on.
func TestHelpStartsWithCurrentTab(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 40, false)
	press(a, "4", "?")
	s := screen(a)
	nodes, global := strings.Index(s, "Nodes (this tab)"), strings.Index(s, "Global")
	if nodes < 0 || global < 0 || nodes > global {
		t.Fatalf("help:\n%s", s)
	}
}
