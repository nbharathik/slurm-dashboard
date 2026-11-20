package report

import (
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestJobsTotalsCountArrayTasks(t *testing.T) {
	jobs := []model.Job{
		{ID: model.JobID{Raw: "48999_[8-17%1]", ArrayJobID: 48999, TaskSpec: "[8-17%1]"}, State: model.StatePending, CPUs: 4, GPUs: 1},
		{ID: model.JobID{Raw: "50000", ArrayJobID: 50000}, State: model.StateRunning, CPUs: 8, GPUs: 2},
	}
	doc := Jobs(Header{}, "all", jobs)
	var arr Job
	for _, j := range doc.Jobs {
		if j.ArrayJobID == 48999 {
			arr = j
		}
	}
	if arr.Tasks != 10 || arr.TotalCPUs != 40 || arr.TotalGPUs != 10 || arr.GPUs != 1 {
		t.Fatalf("array row = %+v", arr)
	}
	if want := (JobTotals{Rows: 2, Tasks: 11, CPUs: 48, GPUs: 12}); doc.Totals != want {
		t.Fatalf("totals = %+v, want %+v", doc.Totals, want)
	}
}
