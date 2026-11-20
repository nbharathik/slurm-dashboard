package state

import (
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// D1 and D10: a running job takes its GPUs and memory from the allocation,
// since squeue's %m is per CPU for --mem-per-cpu and %b is empty for --gpus.
func TestJoinJobsUsesAllocation(t *testing.T) {
	jobs := []model.Job{
		{ID: model.JobID{Raw: "1"}, State: model.StateRunning, Nodes: 2, CPUs: 16, MemPerNodeMB: 4096, GPUs: 0},
		{ID: model.JobID{Raw: "2"}, State: model.StatePending, Nodes: 1, MemPerNodeMB: 4096},
	}
	running := []model.RunningJob{{ID: model.JobID{Raw: "1"}, GPUs: 8, MemMB: 64 * 1024, NodeList: []string{"a", "b"}}}
	got := JoinJobs(jobs, running, nil)
	if got[0].GPUs != 8 || got[0].MemPerNodeMB != 32*1024 {
		t.Fatalf("running job = %d GPUs, %d MB per node", got[0].GPUs, got[0].MemPerNodeMB)
	}
	if got[1].MemPerNodeMB != 4096 || jobs[0].MemPerNodeMB != 4096 {
		t.Fatal("pending job changed, or the input was modified")
	}
}
