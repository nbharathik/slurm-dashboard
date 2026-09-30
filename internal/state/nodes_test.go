package state

import (
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

func names(list []NodeUsage) []string {
	out := make([]string, len(list))
	for i, u := range list {
		out[i] = u.Node.Name
	}
	return out
}

func TestSortNodes(t *testing.T) {
	node := func(name string, cpuAlloc, gpuFree int, load float64) NodeUsage {
		return NodeUsage{Node: model.Node{Name: name, State: "MIXED", CPUTotal: 64, CPUAlloc: cpuAlloc, CPULoad: load, GPUTotal: 4}, Free: gpuFree}
	}
	list := []NodeUsage{node("c", 60, 1, 5), node("a", 10, 1, 50), node("b", 0, 2, 1), node("d", 10, 0, 9)}
	for _, c := range []struct {
		by   string
		desc bool
		want string
	}{
		{"free", false, "b a c d"}, // GPUs, then free CPUs (the old tie-break bug), then name
		{"name", false, "a b c d"},
		{"name", true, "d c b a"},
		{"cpu", false, "b a d c"},
		{"gpu", false, "b a c d"},
		{"load", false, "a d c b"},
	} {
		SortNodes(list, c.by, c.desc)
		if got := joinNames(list); got != c.want {
			t.Errorf("SortNodes(%s, %v) = %s, want %s", c.by, c.desc, got, c.want)
		}
	}
}

func joinNames(list []NodeUsage) string {
	s := ""
	for i, n := range names(list) {
		if i > 0 {
			s += " "
		}
		s += n
	}
	return s
}

func TestNodeStateLabelAndLoad(t *testing.T) {
	for _, c := range []struct {
		n    model.Node
		want string
	}{
		{model.Node{State: "IDLE", Flags: []string{"DRAIN"}}, "drained"},
		{model.Node{State: "MIXED", Flags: []string{"DRAIN"}}, "draining"},
		{model.Node{State: "ALLOCATED"}, "alloc"},
		{model.Node{State: "IDLE", Flags: []string{"NOT_RESPONDING"}}, "idle*"},
	} {
		if got := NodeStateLabel(c.n); got != c.want {
			t.Errorf("NodeStateLabel(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
	if r := ShortReason("bad DIMM [root@2026-09-27T12:00:00]"); r != "bad DIMM" {
		t.Errorf("ShortReason = %q", r)
	}
	if l := LoadLevel(model.Node{CPUTotal: 32, CPUAlloc: 32, CPULoad: 32.6}); l != LoadOK {
		t.Errorf("a full node at its CPU count is fine, got %d", l)
	}
	if l := LoadLevel(model.Node{CPUTotal: 32, CPUAlloc: 4, CPULoad: 20}); l != LoadHigh {
		t.Errorf("load beyond the allocation = %d", l)
	}
	if l := LoadLevel(model.Node{CPUTotal: 32, CPUAlloc: 32, CPULoad: 50}); l != LoadOver {
		t.Errorf("load beyond the CPUs = %d", l)
	}
}

func TestPartitionSummaries(t *testing.T) {
	nodes := []model.Node{
		{Name: "g1", State: "MIXED", CPUTotal: 64, CPUAlloc: 16, GPUTotal: 4, GPUAlloc: 1, GPUs: []model.GPUGroup{{Type: "h200", Total: 4, Alloc: 1}}},
		{Name: "m1", State: "IDLE", CPUTotal: 32, MIGTotal: 11, GPUs: []model.GPUGroup{{Type: "1g.33gb", Total: 4, MIG: true}, {Type: "1g.16gb", Total: 7, MIG: true}}},
		{Name: "c1", State: "IDLE", Flags: []string{"DRAIN"}, Reason: "fan", CPUTotal: 64},
	}
	usage := NodeGPUUsage(nodes, nil, "you")
	parts := []model.Partition{{Name: "gpu", State: "UP", Nodes: []string{"g1", "m1"}}, {Name: "cpu", State: "UP", Nodes: []string{"c1"}}}
	s := PartitionSummaries(parts, usage, map[string]int{"gpu": 3}, map[string]bool{"gpu": true})
	g := s[0]
	if g.Nodes != 2 || g.CPUFree != 80 || g.GPUTotal != 4 || g.GPUFree != 3 || g.MIGTotal != 11 || g.MIGFree != 11 || g.Pending != 3 || !g.PendingKnown {
		t.Errorf("gpu = %+v", g)
	}
	if len(g.GPUTypes) != 1 || g.GPUTypes[0] != (TypeCount{Type: "h200", Total: 4, Free: 3}) {
		t.Errorf("gpu types = %+v", g.GPUTypes)
	}
	if c := s[1]; c.Down != 1 || c.CPUFree != 0 || len(c.DownReasons) != 1 || c.PendingKnown {
		t.Errorf("cpu = %+v", c)
	}
}

func TestPendingFromFullQueue(t *testing.T) {
	jobs := []model.Job{
		{ID: model.JobID{Raw: "5"}, State: model.StatePending, Partition: "gpu,cpu", Priority: 10},
		{ID: model.JobID{Raw: "6"}, State: model.StateRunning, Partition: "gpu"},
		{ID: model.JobID{Raw: "7"}, State: model.StatePending, Partition: "cpu", Priority: 5},
	}
	p := PendingJobs(jobs)
	if len(p) != 2 {
		t.Fatalf("PendingJobs = %+v", p)
	}
	in := PendingIn(p, []string{"gpu"})
	if len(in) != 1 || in[0] != (parse.PendingJob{ID: "5", Partition: "gpu", Priority: 10}) {
		t.Fatalf("PendingIn = %+v", in)
	}
	if Hidden(nil, []string{"x"}) || !Hidden([]string{"login"}, []string{"login"}) || Hidden([]string{"login"}, []string{"login", "cpu"}) || Hidden([]string{"login"}, nil) {
		t.Fatal("Hidden")
	}
}
