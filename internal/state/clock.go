package state

import (
	"sort"
	"sync"
	"time"
)

// Clock is the source of time for the scheduler.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the wall clock.
type RealClock struct{}

// Now returns time.Now().
func (RealClock) Now() time.Time { return time.Now() }

// After returns time.After(d).
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// FakeClock is a manually advanced clock for tests.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
	changed chan struct{}
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

// NewFakeClock returns a FakeClock starting at t.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t, changed: make(chan struct{}, 1)}
}

// Now returns the fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that fires once the clock is advanced past d.
func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	select {
	case c.changed <- struct{}{}:
	default:
	}
	return ch
}

// Advance moves the clock forward and fires due timers in order.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	sort.Slice(c.waiters, func(i, j int) bool { return c.waiters[i].at.Before(c.waiters[j].at) })
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
			continue
		}
		kept = append(kept, w)
	}
	c.waiters = kept
}

// Waiters returns how many timers are pending.
func (c *FakeClock) Waiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// BlockUntil waits (in real time, up to 5 s) until n timers are pending.
func (c *FakeClock) BlockUntil(n int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.Waiters() >= n {
			return true
		}
		select {
		case <-c.changed:
		case <-time.After(time.Millisecond):
		}
	}
	return false
}
