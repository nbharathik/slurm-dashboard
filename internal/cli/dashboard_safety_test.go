package cli

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

func TestDetailOpeningRefreshesMissingOwnershipOnce(t *testing.T) {
	f := execx.NewFake()
	src := &state.Sources{Runner: f, Cmd: slurm.Commands{User: "user1"}}
	for _, item := range []struct {
		argv []string
		file string
	}{{src.Cmd.MyJobs(), "myjobs.txt"}, {src.Cmd.JobDetail("14"), "jobdetail-running.txt"}} {
		raw, err := os.ReadFile("../../testdata/fixtures/23.11/" + item.file)
		if err != nil {
			t.Fatal(err)
		}
		f.Set(item.argv, execx.FakeResponse{Stdout: raw})
	}
	vs := &viewState{}
	vs.onViewState(views.DetailMsg{ID: "14"})
	for range 2 {
		d, err := vs.collectDetail(context.Background(), src, false)
		if err != nil || d.Job == nil || d.Job.ID.Raw != "14" {
			t.Fatalf("owned detail unavailable: %+v, %v", d, err)
		}
	}
	// Reopening also reuses the equivalent fresh native query.
	vs.onViewState(views.DetailMsg{})
	vs.onViewState(views.DetailMsg{ID: "14"})
	if _, err := vs.collectDetail(context.Background(), src, false); err != nil {
		t.Fatal(err)
	}
	queries := 0
	for _, argv := range f.Calls() {
		if slices.Equal(argv, src.Cmd.MyJobs()) {
			queries++
		}
	}
	if queries != 1 {
		t.Fatalf("scheduled polls or fresh reopening repeated ownership queries: %d", queries)
	}
	vs.onViewState(views.DetailMsg{ID: "999"})
	if _, err := vs.collectDetail(context.Background(), src, false); !errors.Is(err, state.ErrNotOwner) {
		t.Fatalf("foreign detail did not fail closed: %v", err)
	}
	for _, argv := range f.Calls() {
		if slices.Equal(argv, src.Cmd.JobDetail("999")) {
			t.Fatal("foreign controller lookup launched")
		}
	}
}
