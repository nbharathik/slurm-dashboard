package insights

import (
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestTypicalWait(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	h := func(id string, arr uint64, task, part string, wait time.Duration) model.HistoryJob {
		return model.HistoryJob{ID: model.JobID{Raw: id, ArrayJobID: arr, TaskSpec: task}, Partition: part, Submit: t0, Start: t0.Add(wait)}
	}
	hist := []model.HistoryJob{
		h("1", 1, "", "gpu", 10*time.Minute),
		h("2", 2, "", "gpu", 40*time.Minute),
		h("3_1", 3, "1", "gpu", 2*time.Hour),
		h("3_2", 3, "2", "gpu", 2*time.Hour), // same array: counted once
		h("4", 4, "", "cpu", time.Minute),
		{ID: model.JobID{Raw: "5"}, Partition: "gpu", Submit: t0}, // never started
	}
	if d, n, ok := TypicalWait(hist, "gpu"); !ok || n != 3 || d != 40*time.Minute {
		t.Fatalf("gpu: %s over %d, %v", d, n, ok)
	}
	if _, _, ok := TypicalWait(hist, "cpu"); ok {
		t.Fatal("one cpu job is not enough")
	}
	if l := TypicalWaitLine(hist, "gpu,cpu"); !strings.Contains(l, "gpu usually waited 40m (median of 3") {
		t.Fatal(l)
	}
}

func TestCannotRun(t *testing.T) {
	d := func(h int) *time.Duration { x := time.Duration(h) * time.Hour; return &x }
	parts := []model.Partition{
		{Name: "gpu", MaxTime: d(48), Nodes: []string{"g1", "g2"}},
		{Name: "cpu", MaxTime: d(168), Nodes: []string{"c1"}},
	}
	nodes := []model.Node{
		{Name: "g1", CPUTotal: 64, MemTotalMB: 512000, GPUTotal: 4},
		{Name: "g2", CPUTotal: 64, MemTotalMB: 512000, GPUTotal: 4},
		{Name: "c1", CPUTotal: 128, MemTotalMB: 1024000},
	}
	pend := func(part string, limit, nodesN, cpus, gpus int, mem int64) model.Job {
		return model.Job{State: model.StatePending, Partition: part, TimeLimit: d(limit), Nodes: nodesN, CPUs: cpus, GPUs: gpus, MemPerNodeMB: mem}
	}
	for _, tc := range []struct {
		name string
		job  model.Job
		want string // "" = fits
	}{
		{"fits", pend("gpu", 24, 1, 16, 2, 64000), ""},
		{"time", pend("gpu", 72, 1, 16, 2, 64000), "allows at most 2d"},
		{"gpus per node", pend("gpu", 24, 1, 16, 8, 64000), "8 GPUs per node, the largest node in gpu has 4"},
		{"two nodes of 4 fit", pend("gpu", 24, 2, 32, 8, 64000), ""},
		{"too many nodes", pend("gpu", 24, 3, 48, 3, 0), "3 nodes, partition gpu has 2"},
		{"cpus", pend("cpu", 24, 1, 256, 0, 0), "256 CPUs per node"},
		{"one partition fits", pend("gpu,cpu", 100, 1, 16, 0, 0), ""},
		{"unknown partition", pend("other", 9999, 1, 1, 0, 0), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(CannotRun(tc.job, parts, nodes), "; ")
			if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	// Missing node data says nothing about sizes.
	if r := CannotRun(pend("gpu", 24, 1, 999, 99, 0), parts, nodes[2:]); len(r) != 0 {
		t.Fatalf("incomplete node data: %v", r)
	}
	big := pend("gpu", 24, 1, 16, 8, 0)
	big.ID, big.Name = model.JobID{Raw: "77"}, "huge"
	alerts := Compute(Input{Now: time.Now(), MyJobs: []model.Job{big}, Partitions: parts, Nodes: nodes})
	if len(alerts) != 1 || alerts[0].Kind != "unsatisfiable" || !strings.Contains(alerts[0].Message, "77 huge can never start: it asks for 8 GPUs") {
		t.Fatalf("alerts = %+v", alerts)
	}
	running := pend("gpu", 999, 1, 1, 0, 0)
	running.State = model.StateRunning
	if CannotRun(running, parts, nodes) != nil {
		t.Fatal("only pending jobs")
	}
}
