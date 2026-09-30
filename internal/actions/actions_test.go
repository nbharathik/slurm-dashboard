package actions

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func job(id string, st model.JobState, reason string) model.Job {
	return model.Job{ID: model.JobID{Raw: id}, State: st, Reason: reason}
}

func TestBuildCommands(t *testing.T) {
	r := Default()
	run := job("812", model.StateRunning, "None")
	pend := job("813", model.StatePending, "Priority")
	held := job("814", model.StatePending, "JobHeldUser")
	arr := job("815_[3-9%2]", model.StatePending, "Resources")

	cases := []struct {
		action string
		jobs   []model.Job
		args   Args
		want   [][]string
	}{
		{"cancel", []model.Job{run, pend}, nil, [][]string{{"scancel", "812", "813"}}},
		{"hold", []model.Job{pend, arr}, nil, [][]string{{"scontrol", "hold", "813,815_[3-9%2]"}}},
		{"release", []model.Job{held}, nil, [][]string{{"scontrol", "release", "814"}}},
		{"requeue", []model.Job{run}, nil, [][]string{{"scontrol", "requeue", "812"}}},
	}
	for _, tc := range cases {
		a, ok := r.Get(tc.action)
		if !ok {
			t.Fatalf("missing action %s", tc.action)
		}
		got, err := a.Build(tc.jobs, tc.args)
		if err != nil || !slices.EqualFunc(got, tc.want, slices.Equal) {
			t.Errorf("%s: got %q %v, want %q", tc.action, got, err, tc.want)
		}
		for _, argv := range got {
			if execx.Classify(argv) != execx.Mutating {
				t.Errorf("%s must be classified as mutating: %q", tc.action, argv)
			}
		}
	}
}

func TestRejectsInjection(t *testing.T) {
	cancel, _ := Default().Get("cancel")
	for _, id := range []string{"812; rm -rf ~", "--user=bob", "812 813", "", "812\n813", "-1", "812&&x", "$(id)"} {
		if _, err := cancel.Build([]model.Job{job(id, model.StateRunning, "")}, nil); err == nil {
			t.Errorf("accepted job ID %q", id)
		}
	}
	if _, err := cancel.Build(nil, nil); err == nil {
		t.Error("accepted no jobs")
	}
	for _, gone := range []string{"top", "timelimit"} {
		if _, ok := Default().Get(gone); ok {
			t.Errorf("action %s must not be registered", gone)
		}
	}
}

func TestAppliesTo(t *testing.T) {
	r := Default()
	ids := func(j model.Job) []string {
		var out []string
		for _, a := range r.list {
			if a.AppliesTo(j) {
				out = append(out, a.ID)
			}
		}
		return out
	}
	if got := ids(job("1", model.StateRunning, "None")); !slices.Equal(got, []string{"cancel", "requeue"}) {
		t.Errorf("running: %v", got)
	}
	if got := ids(job("1", model.StatePending, "Priority")); !slices.Equal(got, []string{"cancel", "hold"}) {
		t.Errorf("pending: %v", got)
	}
	if got := ids(job("1", model.StatePending, "JobHeldUser")); !slices.Equal(got, []string{"cancel", "release"}) {
		t.Errorf("held: %v", got)
	}
	if got := ids(job("1", model.StateCompleted, "")); len(got) != 0 {
		t.Errorf("completed: %v", got)
	}
	if len(r.list) != 4 {
		t.Fatal("registry size")
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("unknown action")
	}
}

func TestRunGrantsExactlyTheConfirmedArgv(t *testing.T) {
	f := execx.NewFake()
	f.Set([]string{"scancel", "812"}, execx.FakeResponse{})
	f.Set([]string{"scancel", "999"}, execx.FakeResponse{})
	cancel, _ := Default().Get("cancel")

	res := Run(context.Background(), f, cancel, [][]string{{"scancel", "812"}})
	if res.Err != nil || res.Summary(1) != "Cancelled 1 job" {
		t.Fatalf("run = %+v", res)
	}
	// Without Run's grant, the same fake refuses the mutation.
	if _, err := f.Run(context.Background(), "scancel", "999"); !errors.Is(err, execx.ErrMutationNotAuthorized) {
		t.Fatalf("mutation without grant: %v", err)
	}

	f.Set([]string{"scontrol", "hold", "5"}, execx.FakeResponse{Stderr: []byte("scontrol: error: Job has already finished\n"), ExitCode: 1})
	hold, _ := Default().Get("hold")
	res = Run(context.Background(), f, hold, [][]string{{"scontrol", "hold", "5"}})
	if res.Err == nil || !strings.Contains(res.Summary(1), "Job has already finished") {
		t.Fatalf("failure = %+v", res)
	}
	if Command([][]string{{"scancel", "1", "2"}}) != "scancel 1 2" {
		t.Fatal("Command")
	}
	if (Result{Action: cancel}).Summary(2) != "Cancelled 2 jobs" {
		t.Fatal("plural")
	}
}
