package views

import (
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func TestFilter(t *testing.T) {
	jobs := []model.Job{
		{ID: model.JobID{Raw: "812", ArrayJobID: 812}, Name: "train-lora", State: model.StateRunning, Partition: "gpu", User: "alice", GPUs: 1},
		{ID: model.JobID{Raw: "813", ArrayJobID: 813}, Name: "eval-bench", State: model.StatePending, Reason: "Priority", Partition: "gpu", User: "alice"},
		{ID: model.JobID{Raw: "815_3", ArrayJobID: 815}, Name: "sweep", State: model.StatePending, Reason: "JobHeldUser", Partition: "cpu", User: "bob"},
		{ID: model.JobID{Raw: "816", ArrayJobID: 816}, Name: "preproc", State: model.StateOOM, Partition: "cpu", User: "alice", GPUs: 4},
	}
	cases := map[string][]string{
		"":                    {"812", "813", "815_3", "816"},
		"state:R":             {"812"},
		"state:PD,R":          {"812", "813", "815_3"},
		"state:H":             {"815_3"},
		"state:F":             {"816"},
		"part:cpu":            {"815_3", "816"},
		"user:bob":            {"815_3"},
		"gpu:>0":              {"812", "816"},
		"gpu:>=2":             {"816"},
		"gpu:<1":              {"813", "815_3"},
		"gpu:0":               {"813", "815_3"},
		"gpu:<=1":             {"812", "813", "815_3"},
		"name:~^(train|eval)": {"812", "813"},
		"name:sweep":          {"815_3"},
		"trlr":                {"812"},
		"id:815":              {"815_3"},
		"81":                  {"812", "813", "815_3", "816"},
		"part:gpu state:PD":   {"813"},
	}
	for expr, want := range cases {
		f, err := ParseFilter(expr)
		if err != nil {
			t.Fatalf("ParseFilter(%q): %v", expr, err)
		}
		var got []string
		for _, j := range jobs {
			if f.Match(j) {
				got = append(got, j.ID.Raw)
			}
		}
		if len(got) != len(want) {
			t.Errorf("%q matched %v, want %v", expr, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%q matched %v, want %v", expr, got, want)
				break
			}
		}
	}
	for _, bad := range []string{"state:ZZ", "gpu:lots", "name:~(", "colour:red"} {
		if _, err := ParseFilter(bad); err == nil {
			t.Errorf("ParseFilter(%q) should fail", bad)
		}
	}
	if f, _ := ParseFilter("  "); !f.Empty() {
		t.Fatal("blank filter")
	}
}

func TestNodeFilter(t *testing.T) {
	gpu := state.NodeUsage{Node: model.Node{
		Name: "h200-01", State: "MIXED", Partitions: []string{"gpu"}, Features: []string{"ib"},
		CPUTotal: 96, CPUAlloc: 16, GPUTotal: 4, GPUs: []model.GPUGroup{{Type: "nvidia_h200_nvl", Total: 4, Alloc: 3}},
	}, Free: 1}
	cpu := state.NodeUsage{Node: model.Node{Name: "cpu04", State: "IDLE", Flags: []string{"DRAIN"}, Partitions: []string{"cpu"}, CPUTotal: 64}}
	for _, c := range []struct {
		expr     string
		gpu, cpu bool
	}{
		{"", true, true},
		{"state:drain", false, true},
		{"state:mixed", true, false},
		{"part:cpu", false, true},
		{"feat:ib", true, false},
		{"gpu:h200", true, false},
		{"gpu:H200", true, false},
		{"gpu:>0", true, false},
		{"cpu:>=64", true, false}, // a drained node has no free CPUs
		{"cpu:>=81", false, false},
		{"h200", true, false},
		{"cpu0", false, true},
	} {
		f, err := ParseNodeFilter(c.expr)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got := f.Match(gpu, nil); got != c.gpu {
			t.Errorf("%q on the GPU node = %v", c.expr, got)
		}
		if got := f.Match(cpu, nil); got != c.cpu {
			t.Errorf("%q on the CPU node = %v", c.expr, got)
		}
	}
	for _, bad := range []string{"cpu:lots", "colour:red"} {
		if _, err := ParseNodeFilter(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}
