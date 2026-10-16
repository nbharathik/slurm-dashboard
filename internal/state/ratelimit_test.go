package state

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
)

func drain(s *Scheduler) {
	go func() {
		for range s.Updates() {
		}
	}()
}

func TestRateLimitSlowsEverySource(t *testing.T) {
	s, _ := newTestScheduler(t)
	limited := Func{N: "nodes", I: func() time.Duration { return 10 * time.Second }, Fn: func(context.Context) (any, error) {
		return nil, Classify(&execx.ExitError{Code: 1}, []byte("scontrol: error: Rate limit exceeded, please retry later"))
	}}
	other := &testCollector{name: "myjobs", interval: 10 * time.Second}
	s.Add(limited, Options{})
	s.Add(other, Options{})
	drain(s)
	ctx := context.Background()

	s.run(ctx, s.entries[0])
	for _, e := range s.entries {
		if got := s.intervalLocked(e); got != RateLimitWait {
			t.Fatalf("%s waits %s after a rate limit, want %s", e.c.Name(), got, RateLimitWait)
		}
	}
	s.run(ctx, s.entries[1])
	if got := s.intervalLocked(s.entries[1]); got != 10*time.Second {
		t.Fatalf("a success must end the wait of that source, got %s", got)
	}
	if got := s.intervalLocked(s.entries[0]); got != RateLimitWait {
		t.Fatalf("the limited source keeps waiting until it succeeds, got %s", got)
	}
}

func TestOtherFailuresDoNotSlowOtherSources(t *testing.T) {
	s, _ := newTestScheduler(t)
	failing := &testCollector{name: "nodes", interval: 10 * time.Second}
	failing.fail.Store(true)
	other := &testCollector{name: "myjobs", interval: 10 * time.Second}
	s.Add(failing, Options{})
	s.Add(other, Options{})
	drain(s)

	s.run(context.Background(), s.entries[0])
	if got := s.intervalLocked(s.entries[1]); got != 10*time.Second {
		t.Fatalf("an ordinary failure slowed another source to %s", got)
	}
}

func TestSourcesGroupsAndRateLimit(t *testing.T) {
	f := execx.NewFake()
	s := &Sources{Runner: f, Cmd: slurm.Commands{User: "alice"}}
	ctx := context.Background()

	f.Set(slurm.Groups(), execx.FakeResponse{Stdout: []byte("alice physics\n")})
	got, err := s.Groups(ctx)
	if err != nil || !slices.Equal(got, []string{"alice", "physics"}) {
		t.Fatalf("groups = %q, %v", got, err)
	}
	if calls := f.Calls(); len(calls) != 1 || !slices.Equal(calls[0], []string{"id", "-Gn"}) {
		t.Fatalf("calls = %q", calls)
	}
	f.Set(slurm.Groups(), execx.FakeResponse{Stderr: []byte("id: error"), ExitCode: 1})
	if _, err := s.Groups(ctx); err == nil {
		t.Fatal("a failed id must be an error, so the groups stay unknown")
	}

	f.Set(s.Cmd.Nodes(), execx.FakeResponse{Stderr: []byte("scontrol: error: slurm_load_node error: Rate limit exceeded, please retry later\n"), ExitCode: 1})
	if _, err := s.Nodes(ctx); KindOf(err) != ErrRateLimited {
		t.Fatalf("nodes error %v has kind %v", err, KindOf(err))
	}
}
