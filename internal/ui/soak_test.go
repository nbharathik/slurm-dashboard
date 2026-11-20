package ui

import (
	"context"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// soakSizes is what must not grow while sdash runs for days.
type soakSizes struct {
	heap       uint64
	goroutines int
	maps       int
}

func measure(a *App) soakSizes {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	n := len(a.estimates) + len(a.warned) + len(a.state.Dismissed) + len(a.dirty)
	for _, v := range a.views {
		if j, ok := v.(*views.Jobs); ok {
			n += j.MapSizes()
		}
	}
	return soakSizes{heap: ms.HeapAlloc, goroutines: runtime.NumGoroutine(), maps: n}
}

// 24 simulated hours of the demo with jobs coming and going: heap,
// goroutines and map sizes are as flat at hour 24 as at hour 1.
func TestSoak24Hours(t *testing.T) {
	if testing.Short() {
		t.Skip("simulates a day")
	}
	f := newFixture(t, 0)
	if r, ok := f.src.Runner.(*execx.FakeRunner); ok {
		r.NoLog = true // as in the demo; the test would otherwise measure its own log
	}
	a := f.app(t, 140, 40, false)
	ctx := context.Background()
	deliver := func(source string, data any, err error) {
		a.Update(updateMsg(state.Update{Source: source, Data: data, Err: err, At: f.clock.Now()}))
	}
	step := func() {
		jobs, err := f.src.MyJobs(ctx)
		deliver("myjobs", jobs, err)
		all, err := f.src.AllJobs(ctx)
		deliver("alljobs", all, err)
		cl, err := f.src.Cluster(ctx, nil)
		deliver("cluster", cl, err)
		q, err := f.src.QueueRank(ctx, state.PartitionsOfPending(jobs))
		deliver("queuerank", q, err)
		n, err := f.src.Nodes(ctx)
		deliver("nodes", n, err)
		h, err := f.src.History(ctx, 7)
		deliver("history", h, err)
		deliver("storage", f.quotas(), nil)
		a.Update(tickMsg(f.clock.Now()))
		_, cmd := a.Update(finalTickMsg{}) // tests switch scheduled ticks off
		drive(a, cmd, 0)
	}
	tabs := []string{"1", "2", "3", "4", "5", "6"}
	var first soakSizes
	for hour := range 24 {
		for minute := 0; minute < 60; minute += 5 {
			f.clock.Advance(5 * time.Minute)
			step()
		}
		press(a, tabs[hour%len(tabs)])
		press(a, "down", "enter", "esc")
		if hour == 0 {
			first = measure(a)
		}
		if testing.Verbose() {
			m := measure(a)
			t.Logf("hour %d: %s, %d history jobs", hour+1, kib(m.heap), len(f.st.History.Data.Jobs))
		}
	}
	last := measure(a)
	if p := os.Getenv("SDASH_SOAK_PROFILE"); p != "" {
		if out, err := os.Create(p); err == nil {
			_ = pprof.WriteHeapProfile(out)
			_ = out.Close()
		}
	}
	t.Logf("hour 1: %d KiB heap, %d goroutines, %d map entries; hour 24: %d KiB, %d, %d",
		first.heap>>10, first.goroutines, first.maps, last.heap>>10, last.goroutines, last.maps)
	if last.goroutines > first.goroutines+2 {
		t.Errorf("goroutines grew from %d to %d", first.goroutines, last.goroutines)
	}
	if last.maps > first.maps+16 {
		t.Errorf("map entries grew from %d to %d", first.maps, last.maps)
	}
	if last.heap > first.heap+512<<10 { // about 110 KiB measured
		t.Errorf("heap grew from %s to %s", kib(first.heap), kib(last.heap))
	}
	ended := 0
	for _, h := range f.st.History.Data.Jobs {
		if h.State != model.StateRunning {
			ended++
		}
	}
	t.Logf("%d jobs queued, %d in history at hour 24", len(f.st.MyJobs.Data), ended)
}

func kib(n uint64) string { return strconv.FormatUint(n>>10, 10) + " KiB" }
