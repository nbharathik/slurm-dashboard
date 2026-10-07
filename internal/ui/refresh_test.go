package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func TestRefreshControls(t *testing.T) {
	f := newFixture(t, 0)
	sched := state.NewScheduler(f.clock, 1)
	sched.Add(state.Func{N: "myjobs", I: func() time.Duration { return 10 * time.Second }, Fn: func(context.Context) (any, error) { return nil, nil }}, state.Options{})
	f.opt.Scheduler = sched
	a := f.app(t, 120, 36, false)
	if s := screen(a); !strings.Contains(s, "0s ago") || strings.Contains(s, "↻") || strings.Contains(s, "manual") {
		t.Fatalf("header should show only the freshness:\n%s", firstLines(s, 2))
	}
	press(a, "p")
	if s := screen(a); !sched.Paused() || !strings.Contains(s, "‖ paused") {
		t.Fatalf("pause:\n%s", firstLines(s, 2))
	}
	press(a, "p")
	if sched.Paused() {
		t.Fatal("p should resume")
	}
	// The - and + keys are gone: the speed is the refresh setting.
	press(a, "-", "+")
	if s := screen(a); !strings.Contains(s, "0s ago") || sched.Paused() {
		t.Fatalf("- and + must do nothing:\n%s", firstLines(s, 2))
	}
}

// Changing the refresh setting takes effect at once: the intervals, and
// manual mode with its badge.
func TestApplyRefresh(t *testing.T) {
	f := newFixture(t, 0)
	sched := state.NewScheduler(f.clock, 1)
	sched.Add(state.Func{N: "myjobs", I: func() time.Duration { return 10 * time.Second }, Fn: func(context.Context) (any, error) { return nil, nil }}, state.Options{})
	f.opt.Scheduler = sched
	var got config.Intervals
	f.opt.ApplyRefresh = func(iv config.Intervals) { got = iv; sched.SetManual(iv.Manual) }
	a := f.app(t, 120, 36, false)

	cfg := a.opt.Config
	cfg.Refresh = config.RefreshManual
	a.applyConfig(cfg)
	if !got.Manual || !sched.Manual() {
		t.Fatalf("manual not applied: %+v", got)
	}
	if s := screen(a); !strings.Contains(s, "manual") {
		t.Fatalf("badge lacks manual:\n%s", firstLines(s, 2))
	}
	cfg.Refresh = config.RefreshFast
	a.applyConfig(cfg)
	if got.Manual || sched.Manual() || got.MyJobs != 5*time.Second {
		t.Fatalf("fast not applied: %+v", got)
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[:min(n, len(lines))], "\n")
}

func TestCallRate(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var hist []execx.CallRecord
	for i := range 60 { // one Slurm call every 10 s for 10 minutes
		hist = append(hist, execx.CallRecord{Argv: []string{"squeue"}, Start: now.Add(-time.Duration(i) * 10 * time.Second)})
	}
	hist = append(hist, execx.CallRecord{Argv: []string{"du"}, Start: now}, execx.CallRecord{Argv: []string{"squeue"}, Start: now.Add(-time.Hour)})
	per, last := callRate(hist, now)
	if per < 5.9 || per > 6.2 || last != 7 {
		t.Fatalf("callRate = %.2f/min, %d in the last minute", per, last)
	}
	if per, last := callRate(nil, now); per != 0 || last != 0 {
		t.Fatal("empty history")
	}
}

func TestWelcomeOnce(t *testing.T) {
	f := newFixture(t, 0)
	f.opt.StateDir = t.TempDir()
	a := f.app(t, 100, 30, false)
	if s := screen(a); !strings.Contains(s, "Welcome to sdash") {
		t.Fatalf("no welcome card on the first run:\n%s", s)
	}
	golden(t, "welcome", screen(a))
	press(a, "x")
	if s := screen(a); strings.Contains(s, "Welcome to sdash") {
		t.Fatal("any key should close the welcome card")
	}
	if s := screen(f.app(t, 100, 30, false)); strings.Contains(s, "Welcome to sdash") {
		t.Fatal("the welcome card showed twice")
	}
}
