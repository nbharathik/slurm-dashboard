package state

import (
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestFitNodes(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	h200 := func(name string, alloc int) model.Node {
		return model.Node{
			Name: name, State: "MIXED", CPUTotal: 96, CPUAlloc: 16 * alloc, MemTotalMB: 1 << 20, GPUTotal: 4, GPUAlloc: alloc,
			GPUs: []model.GPUGroup{{Type: "nvidia_h200_nvl", Total: 4, Alloc: alloc}},
		}
	}
	mig := model.Node{
		Name: "mig01", State: "MIXED", CPUTotal: 64, CPUAlloc: 8, MIGTotal: 11, MIGAlloc: 2,
		GPUs: []model.GPUGroup{{Type: "1g.33gb", Total: 4, Alloc: 2, MIG: true}, {Type: "1g.16gb", Total: 7, MIG: true}},
	}
	cpu := model.Node{Name: "cpu01", State: "ALLOCATED", CPUTotal: 64, CPUAlloc: 64}
	nodes := []model.Node{h200("h200-01", 3), h200("h200-02", 4), mig, cpu}
	running := []model.RunningJob{
		{ID: model.JobID{Raw: "1"}, User: "alice", NodeList: []string{"h200-02"}, GPUs: 4, CPUs: 64, EndTime: now.Add(3 * time.Hour)},
		{ID: model.JobID{Raw: "2"}, User: "carol", NodeList: []string{"cpu01"}, CPUs: 64, EndTime: now.Add(time.Hour)},
	}
	usage := NodeGPUUsage(nodes, running, "you")
	gpu, long := 2*day, 7*day
	parts := []model.Partition{
		{Name: "gpu", State: "UP", MaxTime: &gpu, Nodes: []string{"h200-01", "h200-02"}},
		{Name: "mig", State: "UP", MaxTime: &gpu, Nodes: []string{"mig01"}},
		{Name: "cpu", State: "UP", MaxTime: &long, Nodes: []string{"cpu01"}},
	}

	r := FitNodes(usage, parts, Request{GPUs: 1, GPUType: "h200", CPUs: 8, Time: 4 * time.Hour}, nil)
	if len(r.Now) != 1 || r.Now[0].Node != "h200-01" || r.Now[0].FreeGPUs != 1 {
		t.Fatalf("1 H200 now = %+v", r)
	}
	r = FitNodes(usage, parts, Request{GPUs: 2, GPUType: "H200 NVL", CPUs: 8}, nil)
	if len(r.Now) != 0 || r.Soonest == nil || r.Soonest.Node != "h200-02" || !r.Soonest.At.Equal(now.Add(3*time.Hour)) {
		t.Fatalf("2 H200 soonest = %+v %+v", r, r.Soonest)
	}
	r = FitNodes(usage, parts, Request{GPUs: 1, GPUType: "1g.16gb"}, nil)
	if len(r.Now) != 1 || r.Now[0].Node != "mig01" || r.Now[0].FreeGPUs != 7 {
		t.Fatalf("MIG profile = %+v", r)
	}
	r = FitNodes(usage, parts, Request{GPUs: 1, GPUType: "a100"}, nil)
	if len(r.Now) != 0 || r.Soonest != nil || len(r.Reasons) != 1 {
		t.Fatalf("unknown type = %+v", r)
	}
	r = FitNodes(usage, parts, Request{CPUs: 32, Time: 3 * day, Partition: "cpu"}, nil)
	if len(r.Now) != 0 || r.Soonest == nil || r.Soonest.Node != "cpu01" {
		t.Fatalf("cpu soonest = %+v", r)
	}
	r = FitNodes(usage, parts, Request{GPUs: 1, Time: 3 * day}, nil)
	if len(r.Now) != 0 || len(r.Reasons) == 0 {
		t.Fatalf("time limit too long for GPU partitions = %+v", r)
	}
	r = FitNodes(usage, parts, Request{CPUs: 1, Partition: "nope"}, nil)
	if len(r.Reasons) != 1 || r.Reasons[0] != "no partition nope" {
		t.Fatalf("unknown partition = %+v", r.Reasons)
	}
}
