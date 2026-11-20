package model

import "testing"

func TestJobIDTasks(t *testing.T) {
	for spec, want := range map[string]int{
		"": 1, "4": 1, "[3-9]": 7, "[3-9%2]": 7, "[1,3,5-7]": 5, "[x]": 1, "[9-3]": 1,
		"[0-98:2]": 50, "[1-9:2%3]": 5, "[0,10-20:5]": 4,
	} {
		if got := (JobID{TaskSpec: spec}).Tasks(); got != want {
			t.Errorf("Tasks(%q) = %d, want %d", spec, got, want)
		}
	}
	for _, c := range []struct {
		spec string
		task int
		want bool
	}{
		{"[3-9]", 3, true},
		{"[3-9]", 10, false},
		{"[1-9:2]", 4, false},
		{"[1-9:2]", 5, true},
		{"[1,7%2]", 7, true},
		{"4", 4, true},
		{"4", 5, false},
		{"[x]", 0, false},
	} {
		if got := (JobID{TaskSpec: c.spec}).HasTask(c.task); got != c.want {
			t.Errorf("HasTask(%q, %d) = %v", c.spec, c.task, got)
		}
	}
	j := Job{ID: JobID{TaskSpec: "[0-3]"}, CPUs: 8, GPUs: 2}
	if j.TotalCPUs() != 32 || j.TotalGPUs() != 8 {
		t.Errorf("totals = %d CPU %d GPU", j.TotalCPUs(), j.TotalGPUs())
	}
}
