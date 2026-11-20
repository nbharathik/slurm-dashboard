package cli

import (
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
)

func TestQueueCountsArrayTasks(t *testing.T) {
	doc := report.Jobs(report.Header{}, "all", []model.Job{
		{ID: model.JobID{Raw: "815_[3-9]", ArrayJobID: 815, TaskSpec: "[3-9]"}, User: "you", State: model.StatePending, CPUs: 8, GPUs: 1},
		{ID: model.JobID{Raw: "818", ArrayJobID: 818}, User: "you", State: model.StatePending, CPUs: 32, GPUs: 4},
	})
	if doc.Totals.Tasks != 8 || doc.Totals.CPUs != 88 || doc.Totals.GPUs != 11 {
		t.Errorf("JSON totals = %+v", doc.Totals)
	}
	var b strings.Builder
	if err := renderQueue(&b, doc, "user", "you", false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "you: 8 jobs, 88 CPUs, 11 GPUs") {
		t.Errorf("heading:\n%s", out)
	}
	if !strings.Contains(out, "7 (7x1)") {
		t.Errorf("array row GPUs:\n%s", out)
	}
}
