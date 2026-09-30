package state

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

type testCollector struct {
	name     string
	interval time.Duration
	clock    *FakeClock
	takes    time.Duration // advance the fake clock by this much per run
	fail     atomic.Bool
	panics   atomic.Bool
	runs     atomic.Int32
}

func (c *testCollector) Name() string            { return c.name }
func (c *testCollector) Interval() time.Duration { return c.interval }
func (c *testCollector) Collect(context.Context) (any, error) {
	c.runs.Add(1)
	if c.takes > 0 && c.clock != nil {
		c.clock.Advance(c.takes)
	}
	if c.panics.Load() {
		panic("boom")
	}
	if c.fail.Load() {
		return nil, errors.New("slurm_load_jobs error: Unable to contact slurm controller")
	}
	return c.runs.Load(), nil
}

func newTestScheduler(t *testing.T) (*Scheduler, *FakeClock) {
	t.Helper()
	clk := NewFakeClock(t0)
	return NewScheduler(clk, 42), clk
}

func onlyEntry(s *Scheduler) *entry { return s.entries[0] }

func TestIntervalFloorIdleVisibility(t *testing.T) {
	s, _ := newTestScheduler(t)
	fast := &testCollector{name: "fast", interval: time.Second}
	s.Add(fast, Options{})
	e := onlyEntry(s)
	if got := s.intervalLocked(e); got != Floor {
		t.Fatalf("1s interval must be raised to the floor, got %s", got)
	}

	s2, _ := newTestScheduler(t)
	hist := &testCollector{name: "history", interval: 5 * time.Minute}
	s2.Add(hist, Options{Tabs: []string{"history"}, HiddenMin: 30 * time.Minute})
	e2 := onlyEntry(s2)
	s2.visible = "overview"
	if got := s2.intervalLocked(e2); got != 30*time.Minute {
		t.Fatalf("hidden history = %s, want 30m", got)
	}
	s2.visible = "history"
	if got := s2.intervalLocked(e2); got != 5*time.Minute {
		t.Fatalf("visible history = %s, want 5m", got)
	}
	s2.idle = true
	if got := s2.intervalLocked(e2); got != 15*time.Minute {
		t.Fatalf("idle = %s, want 15m", got)
	}
	s2.idle = false

	s3, _ := newTestScheduler(t)
	c := &testCollector{name: "cluster", interval: 30 * time.Second}
	s3.Add(c, Options{Tabs: []string{"overview", "gpus"}, HiddenMin: 60 * time.Second})
	s3.visible = "jobs"
	if got := s3.intervalLocked(onlyEntry(s3)); got != 60*time.Second {
		t.Fatalf("cluster off screen = %s, want 60s", got)
	}
	s3.visible = "gpus"
	if got := s3.intervalLocked(onlyEntry(s3)); got != 30*time.Second {
		t.Fatalf("visible cluster = %s, want 30s", got)
	}
}

func TestJitterBounds(t *testing.T) {
	s, _ := newTestScheduler(t)
	lo, hi := time.Hour, time.Duration(0)
	for range 5000 {
		d := s.jitterLocked(10 * time.Second)
		lo, hi = min(lo, d), max(hi, d)
		if d < 9*time.Second || d > 11*time.Second {
			t.Fatalf("jitter %s outside ±10%%", d)
		}
	}
	if hi-lo < 1500*time.Millisecond {
		t.Fatalf("jitter spread too small: %s..%s", lo, hi)
	}
	for range 1000 {
		if d := s.jitterLocked(Floor); d < Floor {
			t.Fatalf("jitter went below the floor: %s", d)
		}
	}
}

func TestBackoffDoublesAndResets(t *testing.T) {
	s, _ := newTestScheduler(t)
	c := &testCollector{name: "myjobs", interval: 10 * time.Second}
	s.Add(c, Options{})
	e := onlyEntry(s)
	ctx := context.Background()
	go func() {
		for range s.Updates() {
		}
	}()

	c.fail.Store(true)
	want := []time.Duration{20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 5 * time.Minute, 5 * time.Minute}
	for i, w := range want {
		s.run(ctx, e)
		if got := s.intervalLocked(e); got != w {
			t.Fatalf("after failure %d: interval %s, want %s", i+1, got, w)
		}
	}
	c.fail.Store(false)
	s.run(ctx, e)
	if got := s.intervalLocked(e); got != 10*time.Second {
		t.Fatalf("success must reset backoff, got %s", got)
	}
}

func TestBackoffKeepsLongIntervals(t *testing.T) {
	s, _ := newTestScheduler(t)
	c := &testCollector{name: "history", interval: 5 * time.Minute}
	s.Add(c, Options{Tabs: []string{"history"}, HiddenMin: 30 * time.Minute})
	c.fail.Store(true)
	go func() {
		for range s.Updates() {
		}
	}()
	s.run(context.Background(), onlyEntry(s))
	if got := s.intervalLocked(onlyEntry(s)); got != 30*time.Minute {
		t.Fatalf("backoff must not shorten a 30m hidden interval, got %s", got)
	}
}

func TestAdaptiveSlowdown(t *testing.T) {
	s, clk := newTestScheduler(t)
	c := &testCollector{name: "cluster", interval: 20 * time.Second, clock: clk, takes: 8 * time.Second}
	s.Add(c, Options{})
	e := onlyEntry(s)
	go func() {
		for range s.Updates() {
		}
	}()
	s.run(context.Background(), e)
	if got := s.intervalLocked(e); got != 40*time.Second {
		t.Fatalf("slow command (8s of 20s): interval %s, want 40s", got)
	}
	s.run(context.Background(), e)
	if got := s.intervalLocked(e); got != 80*time.Second {
		t.Fatalf("still slow: interval %s, want 80s", got)
	}
	c.takes = 0
	s.run(context.Background(), e)
	if got := s.intervalLocked(e); got != 20*time.Second {
		t.Fatalf("fast again: interval %s, want 20s", got)
	}
}

func TestPanicBecomesError(t *testing.T) {
	s, _ := newTestScheduler(t)
	c := &testCollector{name: "nodes", interval: 30 * time.Second}
	c.panics.Store(true)
	s.Add(c, Options{})
	done := make(chan Update, 1)
	go func() { done <- <-s.Updates() }()
	s.run(context.Background(), onlyEntry(s))
	u := <-done
	if u.Err == nil || !strings.Contains(u.Err.Error(), "panicked") || u.Source != "nodes" {
		t.Fatalf("update = %+v", u)
	}
	if st := s.States()[0]; st.Err == "" || st.Runs != 1 {
		t.Fatalf("state = %+v", st)
	}
}

func TestLoopRunsOnScheduleAndStaggers(t *testing.T) {
	s, clk := newTestScheduler(t)
	c := &testCollector{name: "myjobs", interval: 10 * time.Second}
	s.Add(c, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	if !clk.BlockUntil(1) {
		t.Fatal("collector never waited")
	}
	clk.Advance(staggerMax) // first run within the stagger window
	u := <-s.Updates()
	if u.Source != "myjobs" || u.Err != nil {
		t.Fatalf("first update = %+v", u)
	}
	for i := 2; i <= 4; i++ {
		if !clk.BlockUntil(1) {
			t.Fatal("no timer armed")
		}
		clk.Advance(11 * time.Second) // interval + max jitter
		<-s.Updates()
		if n := c.runs.Load(); n != int32(i) {
			t.Fatalf("runs = %d, want %d", n, i)
		}
	}
}

func TestRefreshDebounceAndCoalesce(t *testing.T) {
	s, clk := newTestScheduler(t)
	block := make(chan struct{})
	var runs atomic.Int32
	c := Func{N: "myjobs", I: func() time.Duration { return time.Hour }, Fn: func(context.Context) (any, error) {
		runs.Add(1)
		<-block
		return nil, nil
	}}
	s.Add(c, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	clk.BlockUntil(1)

	s.Refresh("myjobs") // starts a run that blocks
	waitFor(t, func() bool { return runs.Load() == 1 })
	s.Refresh("myjobs") // running: no-op
	clk.Advance(2 * time.Second)
	s.Refresh("myjobs") // still running: no-op
	close(block)
	<-s.Updates()

	s.mu.Lock()
	queued := onlyEntry(s).queued
	s.mu.Unlock()
	if queued {
		t.Fatal("refresh while running must not queue another run")
	}

	s.Refresh("myjobs")
	<-s.Updates()
	s.Refresh("myjobs") // within one second of the previous refresh
	select {
	case u := <-s.Updates():
		t.Fatalf("debounced refresh ran: %+v", u)
	case <-time.After(100 * time.Millisecond):
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("runs = %d, want 2", n)
	}
	s.Refresh("other") // unknown names are ignored
}

func TestOnceAndActive(t *testing.T) {
	s, clk := newTestScheduler(t)
	once := &testCollector{name: "version", interval: time.Hour}
	once.fail.Store(true)
	var active atomic.Bool
	gated := &testCollector{name: "queuerank", interval: time.Minute}
	s.Add(once, Options{Once: true})
	s.Add(gated, Options{Active: active.Load})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	var mu sync.Mutex
	seen := map[string]int{}
	go func() {
		for u := range s.Updates() {
			mu.Lock()
			seen[u.Source]++
			mu.Unlock()
		}
	}()
	count := func(name string) int {
		mu.Lock()
		defer mu.Unlock()
		return seen[name]
	}

	clk.BlockUntil(2)
	clk.Advance(staggerMax)
	waitFor(t, func() bool { return count("version") == 1 })
	once.fail.Store(false)
	clk.BlockUntil(2)
	clk.Advance(70 * time.Minute) // the retry after a failure waits one interval (1h) plus jitter
	waitFor(t, func() bool { return count("version") == 2 })
	for range 3 {
		clk.Advance(2 * time.Hour)
		time.Sleep(10 * time.Millisecond)
	}
	if n := count("version"); n != 2 {
		t.Fatalf("once collector ran %d times after succeeding", n)
	}
	if n := count("queuerank"); n != 0 {
		t.Fatalf("inactive collector ran %d times", n)
	}
	for _, st := range s.States() {
		if st.Name == "version" && !st.Done {
			t.Fatalf("version not marked done: %+v", st)
		}
	}
}

func TestSetVisibleCatchesUp(t *testing.T) {
	s, clk := newTestScheduler(t)
	c := &testCollector{name: "history", interval: 5 * time.Minute}
	s.Add(c, Options{Tabs: []string{"history"}, HiddenMin: 30 * time.Minute})
	s.SetVisible("overview")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	clk.BlockUntil(1)
	clk.Advance(staggerMax)
	<-s.Updates()

	clk.BlockUntil(1)
	clk.Advance(6 * time.Minute) // hidden: 30m interval, nothing due yet
	select {
	case u := <-s.Updates():
		t.Fatalf("hidden history ran early: %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
	s.SetVisible("history") // visible: 5m interval is already over
	select {
	case <-s.Updates():
	case <-time.After(2 * time.Second):
		t.Fatal("history did not catch up when its tab became visible")
	}
	if s.Idle() {
		t.Fatal("not idle")
	}
	s.SetIdle(true)
}

func TestFakeClock(t *testing.T) {
	clk := NewFakeClock(t0)
	a := clk.After(2 * time.Second)
	b := clk.After(time.Second)
	now := clk.After(0)
	<-now
	clk.Advance(time.Second)
	select {
	case <-b:
	default:
		t.Fatal("1s timer should fire")
	}
	select {
	case <-a:
		t.Fatal("2s timer fired early")
	default:
	}
	clk.Advance(time.Second)
	<-a
	if !clk.Now().Equal(t0.Add(2 * time.Second)) {
		t.Fatal("Now")
	}
	var rc RealClock
	if rc.Now().IsZero() {
		t.Fatal("real clock")
	}
	<-rc.After(time.Millisecond)
	if (Func{N: "x"}).Interval() != Floor {
		t.Fatal("Func default interval")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestOnlyVisible(t *testing.T) {
	s, clk := newTestScheduler(t)
	c := &testCollector{name: "alljobs", interval: 30 * time.Second}
	s.Add(c, Options{Tabs: []string{"queue"}, OnlyVisible: true})
	s.SetVisible("overview")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	for range 4 {
		clk.BlockUntil(1)
		clk.Advance(time.Minute)
	}
	select {
	case u := <-s.Updates():
		t.Fatalf("ran while its tab was hidden: %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
	s.Refresh("alljobs") // a manual refresh still runs
	select {
	case <-s.Updates():
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not run a hidden collector")
	}
}

func TestPause(t *testing.T) {
	s, clk := newTestScheduler(t)
	c := &testCollector{name: "myjobs", interval: 10 * time.Second}
	s.Add(c, Options{})
	s.SetPaused(true)
	if !s.Paused() {
		t.Fatal("Paused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	for range 3 {
		clk.BlockUntil(1)
		clk.Advance(time.Minute)
	}
	select {
	case u := <-s.Updates():
		t.Fatalf("ran while paused: %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
	s.SetPaused(false)
	select {
	case <-s.Updates():
	case <-time.After(2 * time.Second):
		t.Fatal("did not catch up after unpausing")
	}
}

// With manual refresh, each collector runs once, then only on Refresh,
// however much time passes; turning it off resumes the schedule.
func TestManual(t *testing.T) {
	s, clk := newTestScheduler(t)
	c := &testCollector{name: "myjobs", interval: 10 * time.Second}
	s.Add(c, Options{})
	s.SetManual(true)
	if !s.Manual() {
		t.Fatal("Manual")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	clk.BlockUntil(1)
	clk.Advance(time.Second) // the start-up stagger
	select {
	case <-s.Updates():
	case <-time.After(2 * time.Second):
		t.Fatal("manual mode must still load once")
	}
	for range 60 { // ten idle minutes
		clk.BlockUntil(1)
		clk.Advance(10 * time.Second)
	}
	select {
	case u := <-s.Updates():
		t.Fatalf("ran on its own in manual mode: %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
	if n := c.runs.Load(); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}
	s.Refresh("myjobs")
	select {
	case <-s.Updates():
	case <-time.After(2 * time.Second):
		t.Fatal("r must refresh in manual mode")
	}
	s.SetManual(false)
	clk.BlockUntil(1)
	clk.Advance(11 * time.Second)
	select {
	case <-s.Updates():
	case <-time.After(2 * time.Second):
		t.Fatal("the schedule did not resume")
	}
}

func TestIntervalsTable(t *testing.T) {
	iv := NewIntervals(map[string]time.Duration{"myjobs": 10 * time.Second})
	if iv.Get("myjobs") != 10*time.Second || iv.Get("other") != Floor {
		t.Fatal("Get")
	}
	iv.Set(map[string]time.Duration{"myjobs": 30 * time.Second})
	if iv.Get("myjobs") != 30*time.Second {
		t.Fatal("Set")
	}
}
