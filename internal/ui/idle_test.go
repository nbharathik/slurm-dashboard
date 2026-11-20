package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// An idle hour (data arriving, clock ticking, no keys) writes no file: a
// slow network home must not be touched while nobody looks.
func TestIdleHourWritesNoFile(t *testing.T) {
	f := newFixture(t, 0)
	f.opt.StateDir = t.TempDir()
	a := f.app(t, 120, 36, false)
	press(a, "2", "1") // a tab change saves state once
	snapshot := func() map[string]time.Time {
		out := map[string]time.Time{}
		_ = filepath.WalkDir(f.opt.StateDir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if fi, err := d.Info(); err == nil {
					out[p] = fi.ModTime()
				}
			}
			return nil
		})
		return out
	}
	before := snapshot()
	for range 60 {
		f.clock.Advance(time.Minute)
		jobs, _ := f.src.MyJobs(t.Context())
		a.Update(updateMsg(state.Update{Source: "myjobs", Data: jobs, At: f.clock.Now()}))
		a.Update(tickMsg(f.clock.Now()))
	}
	after := snapshot()
	if len(after) != len(before) {
		t.Fatalf("files changed: %v -> %v", before, after)
	}
	for p, m := range before {
		if !after[p].Equal(m) {
			t.Errorf("%s written while idle", p)
		}
	}
}

// A clock tick that changes nothing allocates little; the ceiling catches
// a tick that starts rebuilding tables or alerts every second.
func TestTickAllocations(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 120, 36, false)
	now := f.clock.Now()
	a.Update(tickMsg(now))
	allocs := testing.AllocsPerRun(50, func() { a.Update(tickMsg(now)) })
	t.Logf("%.0f allocations per tick", allocs)
	if allocs > tickAllocCeiling {
		t.Fatalf("%.0f allocations per tick, ceiling %d", allocs, tickAllocCeiling)
	}
}

const tickAllocCeiling = 8 // 1 measured
