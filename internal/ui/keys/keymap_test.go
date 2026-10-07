package keys

import (
	"fmt"
	"slices"
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestNoConflictsPerContext(t *testing.T) {
	m := Default()
	global := append(m.Tabs, m.NextFocus, m.PrevFocus, m.Palette, m.Refresh, m.RefreshAll, m.Pause, m.Settings, m.Help, m.Back, m.Quit, m.Debug)
	table := []key.Binding{m.Up, m.Down, m.PageUp, m.PageDown, m.Home, m.End, m.Open, m.Select, m.SelectAll, m.Sort, m.SortReverse, m.Filter, m.Menu, m.Copy, m.Expand, m.Collapse, m.DetailUp, m.DetailDown}
	jobs := []key.Binding{m.Scope, m.Cancel, m.Hold, m.Release, m.Requeue, m.Stdout, m.Stderr, m.Pager, m.Shell, m.GPU, m.Why, m.Script}
	if c := conflicts(append(append(slices.Clone(global), table...), jobs...)...); len(c) > 0 {
		t.Fatalf("jobs context conflicts: %v", c)
	}
	// On Queue the start estimate (e) and the scope (v) shadow the log and
	// script keys of Jobs, which do not apply to other people's jobs.
	queue := append(slices.Clone(table), m.Estimate, m.ScopeCycle, m.Group, m.Cancel, m.Hold, m.Release, m.Requeue, m.Why)
	if c := conflicts(append(slices.Clone(global), queue...)...); len(c) > 0 {
		t.Fatalf("queue context conflicts: %v", c)
	}
	usage := append(slices.Clone(table), m.RangePrev, m.RangeNext, m.Rerun, m.Stdout, m.Stderr, m.Script)
	if c := conflicts(append(slices.Clone(global), usage...)...); len(c) > 0 {
		t.Fatalf("usage context conflicts: %v", c)
	}
	storage := append(slices.Clone(table), m.Analyse)
	if c := conflicts(append(slices.Clone(global), storage...)...); len(c) > 0 {
		t.Fatalf("storage context conflicts: %v", c)
	}
	overview := []key.Binding{m.Dismiss, m.Open, m.Up, m.Down, m.Home, m.End}
	if c := conflicts(append(slices.Clone(global), overview...)...); len(c) > 0 {
		t.Fatalf("overview context conflicts: %v", c)
	}
	nodes := []key.Binding{m.GPUOnly, m.Group, m.Up, m.Down, m.Open, m.Select, m.Filter, m.Sort, m.SortReverse, m.Copy, m.Expand, m.Collapse}
	if c := conflicts(append(slices.Clone(global), nodes...)...); len(c) > 0 {
		t.Fatalf("nodes context conflicts: %v", c)
	}
	logs := []key.Binding{m.Follow, m.Wrap, m.Search, m.NextMatch, m.PrevMatch, m.SwitchStream, m.NextHighlight, m.PrevHighlight, m.Pager, m.Back, m.Up, m.Down}
	if c := conflicts(logs...); len(c) > 0 {
		t.Fatalf("log viewer conflicts: %v", c)
	}
}

// The help lists tabs once ("1-6"), not six times, and sorting once.
func TestGroups(t *testing.T) {
	groups := Default().Groups()
	if len(groups) != 10 {
		t.Fatal("groups")
	}
	global := groups[0].Bindings
	tabs := 0
	for _, b := range global {
		if b.Help().Key == "1-6" {
			tabs++
		}
		if len(b.Keys()) == 1 && len(b.Keys()[0]) == 1 && b.Keys()[0] >= "1" && b.Keys()[0] <= "6" {
			t.Errorf("tab key %q listed on its own", b.Keys()[0])
		}
	}
	if tabs != 1 {
		t.Errorf("want one 1-6 entry in the global group, got %d", tabs)
	}
}

// conflicts reports keys bound to more than one action within a context
// where both are active.
func conflicts(bindings ...key.Binding) []string {
	seen := map[string]string{}
	var out []string
	for _, bd := range bindings {
		for _, k := range bd.Keys() {
			if prev, ok := seen[k]; ok && prev != bd.Help().Desc {
				out = append(out, fmt.Sprintf("%q: %s / %s", k, prev, bd.Help().Desc))
			}
			seen[k] = bd.Help().Desc
		}
	}
	slices.Sort(out)
	return out
}
