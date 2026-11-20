package cli

import (
	"context"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// budgetRun counts the Slurm calls of the dashboard's collectors over a
// simulated hour (fake clock, recorded 23.11 output) and those made after
// the first minute.
func budgetRun(t *testing.T, tab string, idle int, paused, manual bool, card string) (total, afterFirstMinute int) {
	t.Helper()
	f := execx.NewFake()
	if err := f.LoadDir("../../testdata/fixtures/23.11"); err != nil {
		t.Fatal(err)
	}
	f.Handler = func(context.Context, []string) (execx.Result, error) { return execx.Result{}, nil }
	caps := model.Capabilities{Version: "23.11.0", Major: 23, Minor: 11, HasMe: true, HasSstat: true, HasSshare: true, HasSprio: true, HasSacct: true}
	src := &state.Sources{Runner: f, Cmd: slurm.Commands{User: "user1", Caps: caps}}
	clk := state.NewFakeClock(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	s := state.NewScheduler(clk, 7)
	cfg := config.Default()
	vs := &viewState{historyDays: 7, warnWaste: cfg.WarnWaste, notify: true}
	addCollectors(s, src, parse.ClusterInfo{}, state.NewIntervals(intervalMap(cfg.Intervals())), vs,
		func(context.Context) ([]model.Quota, error) { return nil, nil })
	if card != "" {
		vs.onViewState(views.DetailMsg{ID: card})
	}
	s.SetVisible(tab)
	s.SetIdleLevel(idle)
	s.SetPaused(paused)
	s.SetManual(manual)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			select {
			case <-s.Updates():
			case <-ctx.Done():
				return
			}
		}
	}()
	s.Start(ctx)
	// Settled: no collector runs and none has set a timer for a while.
	settle := func() {
		last, quiet := -1, 0
		for range 20000 {
			busy := false
			for _, st := range s.States() {
				busy = busy || st.Running
			}
			if n := clk.Afters(); !busy && n == last {
				if quiet++; quiet >= 3 {
					return
				}
			} else {
				last, quiet = n, 0
			}
			time.Sleep(100 * time.Microsecond)
		}
		t.Fatal("collectors did not settle")
	}
	start := clk.Now()
	end := start.Add(time.Hour)
	minuteMarked := false
	for {
		settle()
		next := clk.NextAt()
		if next.IsZero() || next.After(end) {
			break
		}
		if !minuteMarked && next.Sub(start) > time.Minute {
			afterFirstMinute, minuteMarked = -len(f.Calls()), true
		}
		clk.Advance(next.Sub(clk.Now()))
	}
	if !minuteMarked {
		afterFirstMinute = -len(f.Calls())
	}
	total = len(f.Calls())
	by := map[string]int{}
	for _, c := range f.Calls() {
		k := c[0]
		if len(c) > 1 {
			k += " " + c[1]
		}
		by[k]++
	}
	t.Logf("by command: %v", by)
	return total, afterFirstMinute + total
}

// sdash doctor's estimate stays within 15% of the simulated Overview hour.
func TestDoctorEstimate(t *testing.T) {
	if testing.Short() {
		t.Skip("simulates an hour")
	}
	total, _ := budgetRun(t, model.TabOverview, 0, false, false, "")
	est := overviewCallsPerMinute(config.Default().Intervals())
	if got := float64(total) / 60; est < got*0.85 || est > got*1.15 {
		t.Errorf("doctor says %.1f calls a minute, the simulation makes %.1f", est, got)
	}
}

// The dashboard's Slurm calls per hour at refresh = "normal" stay within
// budget; pause makes none and manual none after the first round.
func TestCallBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("simulates an hour per case")
	}
	for _, tc := range []struct {
		name   string
		tab    string
		idle   int
		paused bool
		manual bool
		card   string
		max    int // calls per hour: measured on Nov 11, plus about 10%
	}{
		{"overview", model.TabOverview, 0, false, false, "", 780},
		{"jobs", model.TabJobs, 0, false, false, "", 590},
		{"job card", model.TabJobs, 0, false, false, "14", 790}, // sstat runs on the real clock: +60 an hour in use
		{"queue", model.TabQueue, 0, false, false, "", 650},
		{"nodes", model.TabNodes, 0, false, false, "", 730},
		{"usage", model.TabUsage, 0, false, false, "", 550},
		{"idle", model.TabOverview, 1, false, false, "", 265},
		{"deep idle", model.TabOverview, 2, false, false, "", 180}, // your jobs stay x3 while notices are on
		{"paused", model.TabOverview, 0, true, false, "", 0},
		{"manual", model.TabOverview, 0, false, true, "", 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			total, later := budgetRun(t, tc.tab, tc.idle, tc.paused, tc.manual, tc.card)
			t.Logf("%s: %d calls in an hour (%.1f a minute), %d after the first minute", tc.name, total, float64(total)/60, later)
			if total > tc.max {
				t.Errorf("%d calls in an hour, budget %d", total, tc.max)
			}
			if (tc.paused || tc.manual) && later != 0 {
				t.Errorf("%d calls after the first minute while paused or manual", later)
			}
		})
	}
}
