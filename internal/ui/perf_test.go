package ui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// bigApp is the Jobs tab in all-users scope with n jobs.
func bigApp(tb testing.TB, n int) *App {
	tb.Helper()
	st := &state.Store{User: "you", ClusterName: "big"}
	limit := 4 * time.Hour
	jobs := make([]model.Job, n)
	for i := range jobs {
		id := 100000 + i
		state := model.StateRunning
		if i%3 == 0 {
			state = model.StatePending
		}
		left := limit / 2
		jobs[i] = model.Job{
			ID: model.JobID{Raw: fmt.Sprint(id), ArrayJobID: uint64(id)}, User: fmt.Sprintf("user%03d", i%400),
			Name: fmt.Sprintf("job-%d-with-a-longer-name", i), State: state, Partition: "gpu", CPUs: 8, GPUs: i % 4,
			TimeUsed: limit / 2, TimeLimit: &limit, TimeLeft: &left, NodeList: []string{fmt.Sprintf("n%04d", i%2000)},
			MemPerNodeMB: 32768, Reason: "Priority",
		}
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	st.Apply(state.Update{Source: "alljobs", Data: jobs, At: now})
	st.Apply(state.Update{Source: "myjobs", Data: jobs[:5], At: now})
	a := New(Options{Config: config.Default(), Store: st, Theme: "dark", Now: func() time.Time { return now }, StartTab: "jobs"})
	tb.Cleanup(a.Close)
	for _, v := range a.views {
		v.Refresh(a.ctx)
	}
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	_ = a.View()
	return a
}

func BenchmarkKeypress10k(b *testing.B) {
	a := bigApp(b, 10000)
	down := keyPress("j")
	b.ResetTimer()
	for range b.N {
		a.Update(down)
		_ = a.View()
	}
}

// TestKeypressBudget checks the rendering budget: a keypress renders within 16 ms with
// 10,000 rows. The race detector slows code ~10×, so it gets more room.
func TestKeypressBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	a := bigApp(t, 10000)
	const presses = 50
	start := time.Now()
	for range presses {
		a.Update(keyPress("j"))
		_ = a.View()
	}
	avg := time.Since(start) / presses
	budget := 16 * time.Millisecond
	if raceEnabled {
		budget *= 10
	}
	t.Logf("average keypress to frame: %v", avg)
	if avg > budget {
		t.Fatalf("keypress takes %v on average, budget %v", avg, budget)
	}
}
