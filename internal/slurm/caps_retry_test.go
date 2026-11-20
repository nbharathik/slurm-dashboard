package slurm

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

// A probe that failed because slurmdbd was down is retried after
// RetryTTL, not after a day; a feature that is simply missing stays cached.
func TestFailedProbeIsRetriedSoon(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	sacct := []string{"sacct", "-n", "-X", "-S", "now-1minutes", "-E", "now", "-o", "JobID"}

	f := probeFake("slurm 24.11.5\n")
	f.Set(sacct, execx.FakeResponse{Stderr: []byte("sacct: error: Problem talking to the database: Connection refused"), ExitCode: 1})
	caps, err := CachedProbe(context.Background(), f, "alice", dir, now)
	if err != nil || caps.HasSacct {
		t.Fatalf("caps = %+v %v", caps, err)
	}

	f.Set(sacct, execx.FakeResponse{})
	if caps, _ := CachedProbe(context.Background(), f, "alice", dir, now.Add(RetryTTL/2)); caps.HasSacct {
		t.Fatal("re-probed before RetryTTL")
	}
	if caps, _ := CachedProbe(context.Background(), f, "alice", dir, now.Add(RetryTTL+time.Minute)); !caps.HasSacct {
		t.Fatal("accounting still off after slurmdbd came back")
	}

	// sshare "not supported" (probeFake) is not transient: cached for a day.
	calls := len(f.Calls())
	if _, err := CachedProbe(context.Background(), f, "alice", dir, now.Add(2*RetryTTL+time.Minute)); err != nil || len(f.Calls()) != calls+1 {
		t.Fatalf("a permanent failure was re-probed: %d calls (was %d)", len(f.Calls()), calls)
	}
}
