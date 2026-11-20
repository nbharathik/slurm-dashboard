package views

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/keys"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// Folded and unfolded rows, group headings and the summary agree on GPUs.
func TestQueueGPUTotalsMatchWhenFolded(t *testing.T) {
	id := func(raw string, arr uint64, task string) model.JobID {
		return model.JobID{Raw: raw, ArrayJobID: arr, TaskSpec: task}
	}
	jobs := []model.Job{
		{ID: id("900_0", 900, "0"), User: "a", Name: "arr", State: model.StateRunning, CPUs: 4, GPUs: 1},
		{ID: id("900_[1-4]", 900, "[1-4]"), User: "a", Name: "arr", State: model.StatePending, CPUs: 4, GPUs: 1},
		{ID: id("901", 901, ""), User: "b", Name: "big", State: model.StateRunning, Nodes: 2, CPUs: 64, GPUs: 0},
	}
	st := &state.Store{User: "a"}
	st.AllJobs.Data, st.AllJobs.Has = jobs, true
	// 901 asked with --gpus=8, which squeue -o does not show; the cluster query does.
	st.Cluster.Data.Jobs = []model.RunningJob{{ID: id("901", 901, ""), GPUs: 8}, {ID: id("900_0", 900, "0"), GPUs: 1}}
	st.Cluster.Has = true
	ctx := &Context{Store: st, Config: config.Default(), Keys: keys.Default(), Theme: theme.New(theme.Dark, true, true, true)}

	v := NewQueue(ctx)
	v.groupBy = "user"
	v.Refresh(ctx)
	cells := func(id string) map[string]string {
		for _, r := range v.table.Rows {
			if r.ID == id {
				return r.Cells
			}
		}
		t.Fatalf("no row %s", id)
		return nil
	}
	plain := func(s string) string { return strings.TrimSpace(ansi.Strip(s)) }
	if got := plain(cells("arr:900")["gpus"]); got != "5" {
		t.Errorf("folded array GPUs = %q, want 5", got)
	}
	if got := plain(cells("arr:900")["res"]); got != "5 GPU 20 CPU" {
		t.Errorf("folded array res = %q", got)
	}
	if got := plain(cells("grp:a")["res"]); got != "5 GPU 20 CPU" || plain(cells("grp:a")["name"]) != "5 jobs" {
		t.Errorf("user a heading = %v", cells("grp:a"))
	}
	if got := plain(cells("901")["gpus"]); got != "8" {
		t.Errorf("--gpus job = %q, want the allocated 8", got)
	}

	v.expanded[900] = true
	v.Refresh(ctx)
	if a, b := plain(cells("900_0")["gpus"]), plain(cells("900_[1-4]")["gpus"]); a != "1" || b != "4" {
		t.Errorf("unfolded GPUs = %q + %q, want 1 + 4", a, b)
	}

	for _, u := range v.summary.users {
		if u.user == "b" && u.gpus != 8 {
			t.Errorf("summary for b = %+v", u)
		}
	}

	f, _ := ParseFilter("gpu:>0")
	v.filter = f
	v.Refresh(ctx)
	if _, ok := v.lookup("901"); !ok || !hasRow(v, "901") {
		t.Error("gpu:>0 must keep a running --gpus job")
	}
}

// A card on a pending range stays open when a task starts and the range
// is renamed; it closes when the array is gone.
func TestCardFollowsRenamedPendingRange(t *testing.T) {
	pend := func(raw, spec string) model.Job {
		return model.Job{ID: model.JobID{Raw: raw, ArrayJobID: 815, TaskSpec: spec}, User: "a", State: model.StatePending}
	}
	st := &state.Store{User: "a"}
	st.AllJobs.Data, st.AllJobs.Has = []model.Job{pend("815_[3-9]", "[3-9]")}, true
	ctx := &Context{Store: st, Config: config.Default(), Keys: keys.Default(), Theme: theme.New(theme.Dark, true, true, true)}
	v := NewQueue(ctx)
	v.Refresh(ctx)
	v.detail, v.detailID = true, "815_[3-9]"
	st.AllJobs.Data = []model.Job{pend("815_[4-9]", "[4-9]"), {ID: model.JobID{Raw: "815_3", ArrayJobID: 815, TaskSpec: "3"}, User: "a", State: model.StateRunning}}
	v.Refresh(ctx)
	if !v.detail || v.detailID != "815_[4-9]" {
		t.Fatalf("card = %v %q", v.detail, v.detailID)
	}
	st.AllJobs.Data = []model.Job{{ID: model.JobID{Raw: "7", ArrayJobID: 7}, User: "a", State: model.StateRunning}}
	v.Refresh(ctx)
	if v.detail {
		t.Fatal("card stayed open after the array left")
	}
}

func hasRow(v *Jobs, id string) bool {
	for _, r := range v.table.Rows {
		if r.ID == id {
			return true
		}
	}
	return false
}
