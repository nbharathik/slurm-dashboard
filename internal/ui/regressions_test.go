package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// Opening one task of a collapsed array (from the Overview) puts the
// cursor on that task, so an action never reaches the whole array.
func TestOpenArrayTaskTargetsOnlyThatTask(t *testing.T) {
	f := newFixture(t, 5*time.Minute)
	a := f.app(t, 120, 36, false)
	press(a, "esc")
	a.Update(views.OpenJobMsg{ID: "815_3"})
	press(a, "c")
	if a.confirm == nil {
		t.Fatalf("no confirmation:\n%s", screen(a))
	}
	var ids []string
	for _, j := range a.confirm.Jobs {
		ids = append(ids, j.ID.Raw)
	}
	if len(ids) != 1 || ids[0] != "815_3" {
		t.Fatalf("cancel targets %v, want only 815_3", ids)
	}
	a.confirm = nil
	press(a, "l")
	if a.logView == nil || !strings.Contains(screen(a), "815_3") {
		t.Errorf("log viewer is not for 815_3:\n%s", screen(a))
	}
}

// :range changes the Usage range, as [ and ] do.
func TestRangeCommandChangesUsage(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	press(a, "esc")
	cmd, err := a.runPalette("range 30d")
	if err != nil {
		t.Fatal(err)
	}
	drive(a, cmd, 0)
	u := a.viewByName(model.TabUsage).(*views.Usage)
	if u.Days() != 30 {
		t.Fatalf("range = %d days, want 30", u.Days())
	}
	if s := screen(a); !strings.Contains(s, "last 30 days") {
		t.Errorf("Usage does not show 30 days:\n%s", s)
	}
}

// Pasted text goes into the input that has the keys and is never read as
// key presses.
func TestPasteGoesIntoInputs(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	press(a, "esc", "2", "/")
	a.Update(tea.PasteMsg{Content: "state:PD\n"})
	press(a, "enter")
	if s := screen(a); !strings.Contains(s, "filter: state:PD") && !strings.Contains(s, "state:PD") {
		t.Errorf("paste did not reach the filter:\n%s", s)
	}
	a.Update(tea.PasteMsg{Content: "c"}) // not capturing: nothing happens
	if a.confirm != nil {
		t.Fatal("pasted text ran a key binding")
	}
	press(a, ":")
	a.Update(tea.PasteMsg{Content: "why 815"})
	if a.palette == nil || a.palette.String() != "why 815" {
		t.Fatalf("palette = %v", a.palette)
	}
}

// esc while editing a filter goes back to the filter applied before.
func TestEscRestoresAppliedFilter(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	press(a, "esc", "4", "/", "g", "p", "u", "enter")
	want := screen(a)
	press(a, "/", "x", "esc")
	if got := screen(a); got != want {
		t.Errorf("the applied filter was not kept:\n--- want\n%s\n--- got\n%s", want, got)
	}
}

// Holding one job runs at once; holding several asks first, and so does
// every action with confirm = "all".
func TestBulkHoldAsksFirst(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	var pending model.Job
	for _, j := range a.st.MyJobs.Data {
		if j.State == model.StatePending && !strings.Contains(j.Reason, "Held") {
			pending = j
		}
	}
	if pending.ID.Raw == "" {
		t.Fatal("fixture has no pending job")
	}
	other := pending
	other.ID.Raw = "999"
	_ = a.startAction("hold", []model.Job{pending}, nil)
	if a.confirm != nil {
		t.Fatal("holding one job asked first")
	}
	_ = a.startAction("hold", []model.Job{pending, other}, nil)
	if a.confirm == nil {
		t.Fatal("holding two jobs did not ask")
	}
	a.confirm = nil
	a.opt.Config.Confirm = "all"
	_ = a.startAction("hold", []model.Job{pending}, nil)
	if a.confirm == nil {
		t.Fatal(`confirm = "all" did not ask`)
	}
}

// :export saves the table as shown and never overwrites a file; :node jumps
// from a job to the node it runs on.
func TestExportAndNodeJump(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	press(a, "esc", "2")
	path := filepath.Join(t.TempDir(), "jobs.csv")
	cmd, err := a.runPalette("export csv " + path)
	if err != nil {
		t.Fatal(err)
	}
	drive(a, cmd, 0)
	b, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(b), "ID,") || !strings.Contains(string(b), "812") {
		t.Fatalf("export = %q %v", b, err)
	}
	cmd, _ = a.runPalette("export csv " + path)
	drive(a, cmd, 0)
	if !a.flash.err {
		t.Error("export overwrote an existing file")
	}

	a.Update(views.OpenJobMsg{ID: "812"})
	cmd, err = a.runPalette("node")
	if err != nil {
		t.Fatal(err)
	}
	drive(a, cmd, 0)
	if a.views[a.tab].Name() != model.TabNodes || !strings.Contains(screen(a), "gpu01") {
		t.Errorf(":node did not open gpu01:\n%s", screen(a))
	}
}

// / filters the Usage table, as the docs say, instead of opening the
// command line.
func TestSlashFiltersUsage(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	press(a, "esc", "5", "/")
	if a.palette != nil {
		t.Fatal("/ opened the command line on Usage")
	}
	if !a.views[a.tab].Capturing() || !strings.Contains(screen(a), "filter:") {
		t.Errorf("/ did not open the filter:\n%s", screen(a))
	}
}
