package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// A bare array ID picks the element that fits the command: once task 3
// runs, /why 815 explains the pending rest and /script accepts either.
func TestArrayIDResolution(t *testing.T) {
	f := newFixture(t, 5*time.Minute)
	a := f.app(t, 120, 36, false)
	jobs, err := a.resolveJobs("815")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, j := range jobs {
		ids = append(ids, j.ID.Raw)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("815 resolves to %v, want the running task and the pending rest", ids)
	}
	j, err := a.oneJob("815", isPending)
	if err != nil || !strings.HasPrefix(j.ID.Raw, "815_[") {
		t.Errorf("pending element: %v %v", j.ID.Raw, err)
	}
	j, err = a.oneJob("815", isRunning)
	if err != nil || j.ID.Raw != "815_3" {
		t.Errorf("running element: %v %v", j.ID.Raw, err)
	}
	if _, err := a.oneJob("815", func(model.Job) bool { return false }); err == nil || !strings.Contains(err.Error(), "is an array; give one of") {
		t.Errorf("no preferred element: %v", err)
	}
	if _, err := a.resolveJobs("81"); err == nil {
		t.Error("81 must not match 815 or 812")
	}
	if _, err := a.resolveJobs("812"); err != nil {
		t.Errorf("plain job: %v", err)
	}
}

// A collapsed array counts tasks per state, not queue records.
func TestArrayGroupCountsTasks(t *testing.T) {
	f := newFixture(t, 5*time.Minute)
	a := f.app(t, 120, 36, false)
	press(a, "2")
	s := screen(a)
	for _, want := range []string{"▸ 815", "●1 ◌6", "7 tasks"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}
