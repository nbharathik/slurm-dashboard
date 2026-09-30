//go:build integration

package integration

import (
	"context"
	"slices"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// TestStatusAgreesWithSqueue checks that sdash's view of
// "my jobs" matches squeue at the same moment.
func TestStatusAgreesWithSqueue(t *testing.T) {
	c := NewCluster(t)
	run := c.Submit("running", "-t", "10", "--wrap", "sleep 300")
	held := c.Submit("held", "--hold", "-t", "10", "--wrap", "sleep 1")
	later := c.Submit("later", "--begin=now+1hour", "-t", "10", "--wrap", "sleep 1")
	c.WaitState(run, "RUNNING")

	ctx := context.Background()
	user, err := slurm.User(ctx, func(string) string { return "root" }, c.Runner)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := slurm.Probe(ctx, c.Runner, user)
	if err != nil {
		t.Fatal(err)
	}
	src := &state.Sources{Runner: c.Runner, Cmd: slurm.Commands{Caps: caps, User: user}}
	jobs, err := src.MyJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := c.SqueueIDs("")

	var got []string
	byID := map[string]model.Job{}
	for _, j := range jobs {
		got = append(got, j.ID.Raw)
		byID[j.ID.Raw] = j
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("sdash sees %v, squeue sees %v", got, want)
	}
	counts := state.CountJobs(jobs)
	if counts.Running != len(c.SqueueIDs("RUNNING")) || counts.Pending != len(c.SqueueIDs("PENDING")) {
		t.Fatalf("counts %+v vs squeue running %v pending %v", counts, c.SqueueIDs("RUNNING"), c.SqueueIDs("PENDING"))
	}
	if byID[run].State != model.StateRunning || byID[held].Reason != "JobHeldUser" || byID[later].Reason != "BeginTime" {
		t.Fatalf("states: run=%+v held=%+v later=%+v", byID[run], byID[held], byID[later])
	}
	if byID[run].Name != "sdash-test-running" || byID[run].User != "root" {
		t.Fatalf("running job = %+v", byID[run])
	}
}
