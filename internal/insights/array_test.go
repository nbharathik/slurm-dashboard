package insights

import (
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestArraySummary(t *testing.T) {
	id := func(raw, task string) model.JobID { return model.JobID{Raw: raw, ArrayJobID: 815, TaskSpec: task} }
	queued := []model.Job{
		{ID: id("815_3", "3"), State: model.StateRunning},
		{ID: id("815_[4-9]", "[4-9]"), State: model.StatePending},
		{ID: model.JobID{Raw: "900", ArrayJobID: 900}, State: model.StateRunning},
	}
	hist := []model.HistoryJob{
		{ID: id("815_0", "0"), State: model.StateCompleted},
		{ID: id("815_2", "2"), State: model.StateOOM},
		{ID: id("815_1", "1"), State: model.StateTimeout},
		{ID: id("815_3", "3"), State: model.StateRunning}, // still in the queue
	}
	got := ArraySummary(815, queued, hist)
	if want := "Array 815: 1 running · 6 pending · 1 done · 2 failed (815_1, 815_2)"; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if ArraySummary(900, queued, hist) != "" {
		t.Fatal("a plain job has no array line")
	}
	if ArraySummary(815, queued[1:2], nil) != "" {
		t.Fatal("an array that is all pending needs no line")
	}
}
