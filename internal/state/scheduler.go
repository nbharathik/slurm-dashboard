package state

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"slices"
	"sync"
	"time"
)

// Scheduling limits.
const (
	Floor           = 5 * time.Second
	MaxBackoff      = 5 * time.Minute
	RefreshDebounce = time.Second
	IdleFactor      = 3
	staggerMax      = 500 * time.Millisecond
)

// Collector is one data source.
type Collector interface {
	Name() string
	Interval() time.Duration
	Collect(ctx context.Context) (any, error)
}

// Update is the result of one collection.
type Update struct {
	Source string
	Data   any
	Err    error
	At     time.Time
	Took   time.Duration
}

// Options tune how the scheduler runs one collector.
type Options struct {
	// Tabs lists the tabs that show this data; empty means always visible.
	Tabs []string
	// HiddenMin is the minimum interval while none of Tabs is visible.
	HiddenMin time.Duration
	// OnlyVisible skips runs while none of Tabs is visible (a manual
	// refresh still runs).
	OnlyVisible bool
	// Active gates runs: while it returns false the collector is skipped
	// (queuerank without pending jobs, jobdetail without an open detail).
	Active func() bool
	// Once runs the collector until it succeeds once, then stops.
	Once bool
	// Timeout bounds one run.
	Timeout time.Duration
}

type entry struct {
	c     Collector
	opt   Options
	wake  chan struct{} // forced run
	nudge chan struct{} // re-evaluate the wait

	// guarded by Scheduler.mu
	running     bool
	queued      bool
	skipped     bool // a run was skipped (paused, hidden, inactive) before any ran
	lastRefresh time.Time
	lastStart   time.Time
	lastEnd     time.Time
	lastErr     error
	took        time.Duration
	backoff     time.Duration
	slow        time.Duration
	runs        int
	done        bool
}

// Scheduler runs collectors. Configure it with Add before Start.
type Scheduler struct {
	clock Clock
	out   chan Update

	mu      sync.Mutex
	rng     *rand.Rand
	entries []*entry
	visible string
	idle    bool
	paused  bool
	manual  bool
}

// NewScheduler returns a scheduler. seed makes jitter reproducible.
func NewScheduler(clock Clock, seed uint64) *Scheduler {
	if clock == nil {
		clock = RealClock{}
	}
	return &Scheduler{
		clock: clock,
		out:   make(chan Update, 64),
		rng:   rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), //nolint:gosec // jitter and stagger, not secrets
	}
}

// Add registers a collector.
func (s *Scheduler) Add(c Collector, opt Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, &entry{c: c, opt: opt, wake: make(chan struct{}, 1), nudge: make(chan struct{}, 1)})
}

// Updates delivers collection results.
func (s *Scheduler) Updates() <-chan Update { return s.out }

// Start launches one goroutine per collector; they stop when ctx ends.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	entries := slices.Clone(s.entries)
	s.mu.Unlock()
	for _, e := range entries {
		go s.loop(ctx, e)
	}
}

// SetManual switches manual refresh: each collector runs once, then only on Refresh.
func (s *Scheduler) SetManual(manual bool) {
	s.mu.Lock()
	changed := s.manual != manual
	s.manual = manual
	s.mu.Unlock()
	if changed {
		s.nudgeAll()
	}
}

// Manual reports whether manual refresh is on.
func (s *Scheduler) Manual() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.manual
}

// Nudge makes every collector look at its interval again, after the
// intervals changed.
func (s *Scheduler) Nudge() { s.nudgeAll() }

// SetVisible records the visible tab, so off-screen sources slow down and
// sources coming on screen catch up.
func (s *Scheduler) SetVisible(tab string) {
	s.mu.Lock()
	changed := s.visible != tab
	s.visible = tab
	s.mu.Unlock()
	if changed {
		s.nudgeAll()
	}
}

// SetPaused stops scheduled runs; a manual Refresh still runs. Unpausing
// catches up at once on anything that became due.
func (s *Scheduler) SetPaused(paused bool) {
	s.mu.Lock()
	changed := s.paused != paused
	s.paused = paused
	s.mu.Unlock()
	if changed {
		s.nudgeAll()
	}
}

// Paused reports whether scheduled runs are stopped.
func (s *Scheduler) Paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// SetIdle switches idle mode (intervals ×3).
func (s *Scheduler) SetIdle(idle bool) {
	s.mu.Lock()
	changed := s.idle != idle
	s.idle = idle
	s.mu.Unlock()
	if changed {
		s.nudgeAll()
	}
}

// Idle reports whether idle mode is on.
func (s *Scheduler) Idle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.idle
}

// Refresh runs the named collectors now (all if none); busy or just-refreshed ones are skipped.
func (s *Scheduler) Refresh(names ...string) {
	now := s.clock.Now()
	s.mu.Lock()
	var wake []*entry
	for _, e := range s.entries {
		if len(names) > 0 && !slices.Contains(names, e.c.Name()) {
			continue
		}
		if e.done || e.running || e.queued || (!e.lastRefresh.IsZero() && now.Sub(e.lastRefresh) < RefreshDebounce) {
			continue
		}
		e.lastRefresh, e.queued = now, true
		wake = append(wake, e)
	}
	s.mu.Unlock()
	for _, e := range wake {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
}

func (s *Scheduler) nudgeAll() {
	s.mu.Lock()
	entries := slices.Clone(s.entries)
	s.mu.Unlock()
	for _, e := range entries {
		select {
		case e.nudge <- struct{}{}:
		default:
		}
	}
}

func (s *Scheduler) loop(ctx context.Context, e *entry) {
	s.mu.Lock()
	wait := time.Duration(s.rng.Int64N(int64(staggerMax)))
	s.mu.Unlock()
	for {
		force := false
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(wait):
		case <-e.wake:
			force = true
		case <-e.nudge:
			s.mu.Lock()
			if e.runs > 0 {
				next := e.lastStart.Add(s.intervalLocked(e))
				if d := next.Sub(s.clock.Now()); d > 0 {
					wait = d
					s.mu.Unlock()
					continue
				}
			} else if wait > 0 && !e.skipped {
				s.mu.Unlock()
				continue
			}
			s.mu.Unlock()
		}

		s.mu.Lock()
		hidden := s.paused || (s.manual && e.runs > 0) || (e.opt.OnlyVisible && !s.visibleLocked(e))
		s.mu.Unlock()
		if !force && (hidden || (e.opt.Active != nil && !e.opt.Active())) {
			s.mu.Lock()
			e.skipped = true // a nudge re-checks at once, e.g. when unpaused or back from manual
			wait = s.jitterLocked(s.intervalLocked(e))
			s.mu.Unlock()
			continue
		}
		s.run(ctx, e)

		s.mu.Lock()
		if e.opt.Once && e.lastErr == nil {
			e.done = true
			s.mu.Unlock()
			return
		}
		wait = s.jitterLocked(s.intervalLocked(e))
		s.mu.Unlock()
	}
}

func (s *Scheduler) run(ctx context.Context, e *entry) {
	s.mu.Lock()
	e.running, e.queued = true, false
	start := s.clock.Now()
	e.lastStart = start
	s.mu.Unlock()

	rctx, cancel := ctx, context.CancelFunc(func() {})
	if e.opt.Timeout > 0 {
		rctx, cancel = context.WithTimeout(ctx, e.opt.Timeout)
	}
	data, err := safeCollect(rctx, e.c)
	cancel()

	s.mu.Lock()
	end := s.clock.Now()
	took := end.Sub(start)
	base := s.baseIntervalLocked(e)
	e.running = false
	e.lastEnd, e.took, e.lastErr = end, took, err
	e.runs++
	if err != nil {
		e.backoff = min(max(e.backoff*2, base*2), max(MaxBackoff, base))
	} else {
		e.backoff = 0
	}
	switch {
	case took > base/4:
		e.slow = min(max(e.slow*2, base*2), max(MaxBackoff, base))
	case took < base/10:
		e.slow = 0
	}
	s.mu.Unlock()

	select {
	case s.out <- Update{Source: e.c.Name(), Data: data, Err: err, At: end, Took: took}:
	case <-ctx.Done():
	}
}

func safeCollect(ctx context.Context, c Collector) (data any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("collector %s panicked: %v\n%s", c.Name(), r, debug.Stack())
		}
	}()
	return c.Collect(ctx)
}

// baseIntervalLocked is the configured interval with the floor,
// visibility and idle rules applied, before backoff and slowdown.
func (s *Scheduler) baseIntervalLocked(e *entry) time.Duration {
	d := max(e.c.Interval(), Floor)
	if !s.visibleLocked(e) && e.opt.HiddenMin > d {
		d = e.opt.HiddenMin
	}
	if s.idle {
		d *= IdleFactor
	}
	return max(d, Floor)
}

// intervalLocked is the wait between runs, before jitter.
func (s *Scheduler) intervalLocked(e *entry) time.Duration {
	return max(s.baseIntervalLocked(e), e.slow, e.backoff, Floor)
}

func (s *Scheduler) visibleLocked(e *entry) bool {
	return len(e.opt.Tabs) == 0 || slices.Contains(e.opt.Tabs, s.visible)
}

// jitterLocked applies ±10% jitter, never going below the floor.
func (s *Scheduler) jitterLocked(d time.Duration) time.Duration {
	f := 0.9 + 0.2*s.rng.Float64()
	return max(time.Duration(float64(d)*f), Floor)
}

// State describes one collector for the debug overlay.
type State struct {
	Name     string
	Interval time.Duration
	Backoff  time.Duration
	Slowdown time.Duration
	Running  bool
	Runs     int
	LastEnd  time.Time
	Took     time.Duration
	Err      string
	Visible  bool
	Done     bool
}

// States returns the current state of every collector.
func (s *Scheduler) States() []State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]State, 0, len(s.entries))
	for _, e := range s.entries {
		st := State{
			Name: e.c.Name(), Interval: s.intervalLocked(e), Backoff: e.backoff, Slowdown: e.slow,
			Running: e.running, Runs: e.runs, LastEnd: e.lastEnd, Took: e.took,
			Visible: s.visibleLocked(e), Done: e.done,
		}
		if e.lastErr != nil {
			st.Err = e.lastErr.Error()
		}
		out = append(out, st)
	}
	return out
}

// Func adapts a function to Collector.
type Func struct {
	N  string
	I  func() time.Duration
	Fn func(ctx context.Context) (any, error)
}

// Name implements Collector.
func (f Func) Name() string { return f.N }

// Interval implements Collector.
func (f Func) Interval() time.Duration {
	if f.I == nil {
		return Floor
	}
	return f.I()
}

// Collect implements Collector.
func (f Func) Collect(ctx context.Context) (any, error) { return f.Fn(ctx) }

// Intervals is a table of collector intervals that can change while the
// scheduler runs (the refresh setting). It is safe for concurrent use.
type Intervals struct {
	mu sync.RWMutex
	m  map[string]time.Duration
}

// NewIntervals returns a table holding m.
func NewIntervals(m map[string]time.Duration) *Intervals { return &Intervals{m: m} }

// Get returns a source's interval; an unknown source gets the floor.
func (iv *Intervals) Get(name string) time.Duration {
	iv.mu.RLock()
	defer iv.mu.RUnlock()
	if d, ok := iv.m[name]; ok {
		return d
	}
	return Floor
}

// Set replaces the table.
func (iv *Intervals) Set(m map[string]time.Duration) {
	iv.mu.Lock()
	iv.m = m
	iv.mu.Unlock()
}
