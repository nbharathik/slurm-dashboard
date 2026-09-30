package insights

import (
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func d(x time.Duration) *time.Duration { return &x }

func TestFailureHints(t *testing.T) {
	cases := []struct {
		h     model.HistoryJob
		title string
	}{
		{model.HistoryJob{State: model.StateCompleted}, ""},
		{model.HistoryJob{State: model.StateOOM, PeakMemMB: 32000, AllocMemMB: 32768}, "Out of memory"},
		{model.HistoryJob{State: model.StateFailed, Signal: 9, PeakMemMB: 32000, AllocMemMB: 32768}, "Out of memory"},
		{model.HistoryJob{State: model.StateFailed, Signal: 9, PeakMemMB: 100, AllocMemMB: 32768}, "Killed (SIGKILL)"},
		{model.HistoryJob{State: model.StateFailed, ExitCode: 137}, "Killed (SIGKILL)"},
		{model.HistoryJob{State: model.StateFailed, ExitCode: 139}, "Segmentation fault"},
		{model.HistoryJob{State: model.StateFailed, Signal: 11}, "Segmentation fault"},
		{model.HistoryJob{State: model.StateFailed, ExitCode: 134}, "Aborted"},
		{model.HistoryJob{State: model.StateFailed, ExitCode: 143}, "Terminated"},
		{model.HistoryJob{State: model.StateFailed, Signal: 7}, "Killed by signal 7"},
		{model.HistoryJob{State: model.StateFailed, ExitCode: 127}, "Command not found"},
		{model.HistoryJob{State: model.StateFailed, ExitCode: 3}, "Exited with code 3"},
		{model.HistoryJob{State: model.StateFailed}, "Failed"},
		{model.HistoryJob{State: model.StateTimeout, TimeLimit: d(time.Hour)}, "Time limit reached"},
		{model.HistoryJob{State: model.StateNodeFail}, "Node failure"},
		{model.HistoryJob{State: model.StateBootFail}, "Node failure"},
		{model.HistoryJob{State: model.StatePreempted}, "Preempted"},
		{model.HistoryJob{State: model.StateCancelled, CancelledByUID: "1000"}, "Cancelled by you"},
		{model.HistoryJob{State: model.StateCancelled, CancelledByUID: "0"}, "Cancelled by root (an administrator)"},
		{model.HistoryJob{State: model.StateCancelled, CancelledByUID: "1234"}, "Cancelled by UID 1234"},
		{model.HistoryJob{State: model.StateCancelled}, "Cancelled"},
		{model.HistoryJob{State: model.StateDeadline}, "Deadline passed"},
		{model.HistoryJob{State: model.StateCompleted, ExitCode: 2}, "Exited with code 2"},
		{model.HistoryJob{State: model.StateFailed, ExitCode: 11}, "Exited with code 11"},
	}
	for _, tc := range cases {
		h := ExplainFailure(tc.h, "1000")
		if h.Title != tc.title {
			t.Errorf("%s %d:%d = %q, want %q", tc.h.State, tc.h.ExitCode, tc.h.Signal, h.Title, tc.title)
		}
	}
	if h := ExplainFailure(model.HistoryJob{State: model.StateFailed, ExitCode: 11}, ""); !strings.Contains(h.Text, "SIGSEGV") {
		t.Fatalf("code 11 hint = %q", h.Text)
	}
	oom := ExplainFailure(model.HistoryJob{State: model.StateOOM, PeakMemMB: 32000, AllocMemMB: 32768}, "")
	if !strings.Contains(oom.Text, "peak 31.2G of 32G") || !strings.Contains(oom.Text, "--mem=48G") {
		t.Fatalf("oom text = %q", oom.Text)
	}
}

func TestRightSize(t *testing.T) {
	h := model.HistoryJob{
		State: model.StateCompleted, AllocMemMB: 32768, PeakMemMB: 3000, Elapsed: 40 * time.Minute, TimeLimit: d(4 * time.Hour),
		AllocCPUs: 8, TotalCPU: 40 * time.Minute * 2, GPUs: 1,
	}
	h.ComputeEfficiency(0.1)
	got := RightSize(h)
	lines := map[string]string{}
	for _, s := range got {
		lines[s.What] = s.Line + " | " + s.Reason
	}
	for what, want := range map[string]string{
		"Memory": "#SBATCH --mem=4G",
		"Time":   "#SBATCH --time=01:00:00",
		"CPUs":   "#SBATCH --cpus-per-task=3",
		"GPUs":   "GPU mostly idle",
	} {
		if !strings.Contains(lines[what], want) {
			t.Errorf("%s = %q, want %q", what, lines[what], want)
		}
	}
	if sbatchTime(26*time.Hour+15*time.Minute) != "1-02:15:00" {
		t.Fatal(sbatchTime(26*time.Hour + 15*time.Minute))
	}
	h.State = model.StateFailed
	if RightSize(h) != nil {
		t.Fatal("right-sizing is for completed jobs only")
	}
	tiny := model.HistoryJob{State: model.StateCompleted, AllocMemMB: 4096, PeakMemMB: 10, Elapsed: time.Minute, TimeLimit: d(time.Hour)}
	tiny.ComputeEfficiency(-1)
	for _, s := range RightSize(tiny) {
		if s.What == "Memory" && s.Line != "#SBATCH --mem=1G" || s.What == "Time" && s.Line != "#SBATCH --time=00:15:00" {
			t.Errorf("minimums: %+v", s)
		}
	}
}

func TestEffCard(t *testing.T) {
	h := model.HistoryJob{
		State: model.StateFailed, ExitCode: 1, NodeList: []string{"n1"}, Nodes: 1, AllocCPUs: 4, Elapsed: time.Hour,
		TimeLimit: d(2 * time.Hour), TotalCPU: 2 * time.Hour, AllocMemMB: 8192, PeakMemMB: 4096, GPUs: 2,
	}
	h.ComputeEfficiency(0.5)
	card := strings.Join(EffCard(h), "\n")
	for _, want := range []string{"FAILED (exit 1:0)", "Nodes:         1 (n1)", "01:00:00 of 02:00:00 (50%)", "CPU used:      02:00:00 (50%)", "Memory:        4G of 8G (50%)", "GPUs:          2, 2.0 GPU-hours, utilisation 50%"} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
	h.TimeLimit, h.PeakMemMB = nil, 0
	h.ComputeEfficiency(-1)
	card = strings.Join(EffCard(h), "\n")
	if !strings.Contains(card, "of none") || !strings.Contains(card, "unknown of 8G") {
		t.Fatalf("card without limits:\n%s", card)
	}
}
