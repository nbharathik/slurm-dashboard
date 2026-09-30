//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// sources returns collectors for the test user on the throwaway cluster.
func sources(t *testing.T, c *Cluster) *state.Sources {
	t.Helper()
	ctx := context.Background()
	user, err := slurm.User(ctx, func(string) string { return "root" }, c.Runner)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := slurm.Probe(ctx, c.Runner, user)
	if err != nil {
		t.Fatal(err)
	}
	return &state.Sources{Runner: c.Runner, Cmd: slurm.Commands{Caps: caps, User: user}}
}

// job returns the current squeue view of one job, polling until cond holds.
func job(t *testing.T, src *state.Sources, id string, cond func(model.Job, bool) bool) model.Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		jobs, err := src.MyJobs(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var j model.Job
		found := false
		for _, x := range jobs {
			if x.ID.Raw == id {
				j, found = x, true
			}
		}
		if cond(j, found) {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s: condition not met; last %+v (found %v)", id, j, found)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func run(t *testing.T, c *Cluster, id string, jobs []model.Job, args actions.Args) {
	t.Helper()
	a, ok := actions.Default().Get(id)
	if !ok {
		t.Fatalf("no action %s", id)
	}
	argvs, err := a.Build(jobs, args)
	if err != nil {
		t.Fatal(err)
	}
	if res := actions.Run(context.Background(), c.Runner, a, argvs); res.Err != nil {
		t.Fatalf("%s %v: %v", id, argvs, res.Err)
	}
}

// TestActionsOnTestJobs exercises hold, release, requeue and cancel on test jobs.
func TestActionsOnTestJobs(t *testing.T) {
	c := NewCluster(t)
	src := sources(t, c)
	later := c.Submit("hold", "--begin=now+1hour", "-t", "10", "--wrap", "sleep 1")
	running := c.Submit("run", "-t", "10", "--wrap", "sleep 300")
	c.WaitState(running, "RUNNING")

	pj := job(t, src, later, func(_ model.Job, ok bool) bool { return ok })
	// Holding as root gives JobHeldAdmin; a normal user gets JobHeldUser.
	run(t, c, "hold", []model.Job{pj}, nil)
	job(t, src, later, func(j model.Job, ok bool) bool { return ok && strings.HasPrefix(j.Reason, "JobHeld") })
	run(t, c, "release", []model.Job{pj}, nil)
	job(t, src, later, func(j model.Job, ok bool) bool { return ok && !strings.HasPrefix(j.Reason, "JobHeld") })

	rj := job(t, src, running, func(j model.Job, ok bool) bool { return ok && j.State == model.StateRunning })
	run(t, c, "cancel", []model.Job{rj}, nil)
	job(t, src, running, func(_ model.Job, ok bool) bool { return !ok })

	again := c.Submit("requeue", "-t", "10", "--wrap", "sleep 300")
	c.WaitState(again, "RUNNING")
	aj := job(t, src, again, func(j model.Job, ok bool) bool { return ok && j.State == model.StateRunning })
	run(t, c, "requeue", []model.Job{aj}, nil)
	job(t, src, again, func(j model.Job, ok bool) bool {
		return ok && (j.State == model.StatePending || j.State == model.StateRequeued || j.State == model.StateCompleting)
	})

	run(t, c, "cancel", []model.Job{pj}, nil)
	job(t, src, later, func(_ model.Job, ok bool) bool { return !ok })
	var final []string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		fs, err := src.FinalStates(context.Background(), []string{later})
		if err == nil && len(fs) == 1 {
			final = []string{string(fs[0].State)}
			if fs[0].State == model.StateCancelled {
				break
			}
		}
		time.Sleep(time.Second)
	}
	if strings.Join(final, "") != string(model.StateCancelled) {
		t.Fatalf("final state of %s = %v", later, final)
	}
}

// TestUnauthorisedMutationIsRefused checks the real runner refuses a
// state-changing command without a grant, before anything runs.
func TestUnauthorisedMutationIsRefused(t *testing.T) {
	c := NewCluster(t)
	if _, err := c.Runner.Run(context.Background(), "scancel", "1"); !errors.Is(err, execx.ErrMutationNotAuthorized) {
		t.Fatalf("unguarded scancel: %v", err)
	}
}
