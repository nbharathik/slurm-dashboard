package insights

import (
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

var now = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func dur(d time.Duration) *time.Duration { return &d }

func id(s string) model.JobID { return model.JobID{Raw: s} }

func keys(as []model.Alert) []string {
	var out []string
	for _, a := range as {
		out = append(out, a.Key)
	}
	return out
}

func TestEveryRuleFires(t *testing.T) {
	in := Input{
		Now: now,
		UID: "1000",
		MyJobs: []model.Job{
			{ID: id("10"), Name: "train", State: model.StateRunning, Partition: "gpu", NodeList: []string{"gpu01"}, TimeLeft: dur(5 * time.Minute)},
			{ID: id("11"), Name: "long", State: model.StatePending, Partition: "gpu", Reason: "PartitionTimeLimit"},
			{ID: id("12"), Name: "ok", State: model.StatePending, Partition: "gpu", Reason: "Resources"},
			{ID: id("13"), Name: "fine", State: model.StateRunning, Partition: "gpu", NodeList: []string{"gpu02"}, TimeLeft: dur(time.Hour)},
		},
		Nodes: []model.Node{
			{Name: "gpu01", State: "MIXED", Flags: []string{"DRAIN"}, Reason: "bad DIMM"},
			{Name: "gpu02", State: "MIXED"},
		},
		Partitions:   []model.Partition{{Name: "gpu", Nodes: []string{"gpu01", "gpu02"}}, {Name: "cpu", Nodes: []string{"cpu01"}}},
		Reservations: []model.Reservation{{Name: "maint", Start: now.Add(51 * time.Hour), End: now.Add(60 * time.Hour), Nodes: []string{"gpu02"}, Flags: []string{"MAINT"}}},
		History: []model.HistoryJob{
			{ID: id("9"), Name: "prep", State: model.StateOOM, End: now.Add(-time.Hour), PeakMemMB: 32563, AllocMemMB: 32768},
			{ID: id("8"), Name: "old", State: model.StateFailed, End: now.Add(-48 * time.Hour)},
		},
		Storage: []model.Quota{
			{Label: "Home", UsedBytes: 98, HardBytes: 100},
			{Label: "Scratch", UsedBytes: 10, HardBytes: 100, UsedFiles: 92, SoftFiles: 100, HardFiles: 200},
			{Label: "Work", UsedBytes: 10, HardBytes: 100},
		},
	}
	got := Compute(in)
	want := []string{
		"storage:Home", "job-failed:9", "unsatisfiable:11", // crit
		"storage:Scratch", "timelimit:10", "node:gpu01:10", // warn
		"maintenance:maint", // info
	}
	if strings.Join(keys(got), " ") != strings.Join(want, " ") {
		t.Fatalf("keys = %v\nwant   %v", keys(got), want)
	}
	msgs := map[string]string{}
	for _, a := range got {
		msgs[a.Key] = a.Message
	}
	for k, sub := range map[string]string{
		"job-failed:9":      "9 prep: OUT_OF_MEMORY (peak 31.8G of 32G)",
		"maintenance:maint": "Maintenance starts in 2d03h; jobs longer than 51h will wait",
		"storage:Scratch":   "92% of its files quota",
		"node:gpu01:10":     "is draining: bad DIMM",
		"unsatisfiable:11":  "can never start: PartitionTimeLimit",
	} {
		if !strings.Contains(msgs[k], sub) {
			t.Errorf("%s = %q, want it to contain %q", k, msgs[k], sub)
		}
	}
}

func TestDismissed(t *testing.T) {
	in := Input{Now: now, Storage: []model.Quota{{Label: "Home", UsedBytes: 98, HardBytes: 100}}}
	in.Dismissed = map[string]time.Time{"storage:Home": now.Add(time.Hour)}
	if got := Compute(in); len(got) != 0 {
		t.Fatalf("dismissed alert shown: %v", got)
	}
	in.Dismissed["storage:Home"] = now.Add(-time.Second)
	if got := Compute(in); len(got) != 1 {
		t.Fatalf("expired dismissal hides alert: %v", got)
	}
}

func TestMaintenance(t *testing.T) {
	base := Input{
		Now: now, MyJobs: []model.Job{{ID: id("1"), State: model.StatePending, Partition: "gpu"}},
		Partitions: []model.Partition{{Name: "gpu", Nodes: []string{"g1"}}},
	}
	cases := []struct {
		name  string
		r     model.Reservation
		level model.Level
		fires bool
	}{
		{"soon", model.Reservation{Name: "m", Start: now.Add(2 * time.Hour), End: now.Add(5 * time.Hour), Flags: []string{"MAINT"}, Nodes: []string{"g1"}}, model.Warn, true},
		{"far", model.Reservation{Name: "m", Start: now.Add(8 * 24 * time.Hour), End: now.Add(9 * 24 * time.Hour), Flags: []string{"MAINT"}}, 0, false},
		{"other nodes", model.Reservation{Name: "m", Start: now.Add(2 * time.Hour), End: now.Add(5 * time.Hour), Flags: []string{"MAINT"}, Nodes: []string{"c1"}}, 0, false},
		{"not maint", model.Reservation{Name: "m", Start: now.Add(2 * time.Hour), End: now.Add(5 * time.Hour)}, 0, false},
		{"ongoing", model.Reservation{Name: "m", Start: now.Add(-time.Hour), End: now.Add(time.Hour), Flags: []string{"MAINT"}}, model.Warn, true},
		{"over", model.Reservation{Name: "m", Start: now.Add(-3 * time.Hour), End: now.Add(-time.Hour), Flags: []string{"MAINT"}}, 0, false},
	}
	for _, tc := range cases {
		in := base
		in.Reservations = []model.Reservation{tc.r}
		got := Compute(in)
		if (len(got) == 1) != tc.fires || (tc.fires && got[0].Level != tc.level) {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
	if got := Compute(Input{Now: now, Reservations: []model.Reservation{cases[0].r}}); len(got) != 0 {
		t.Fatalf("maintenance without any partitions in use: %v", got)
	}
}

func TestLowEfficiencyAndManyFailures(t *testing.T) {
	var hist []model.HistoryJob
	for i := range 5 {
		hist = append(hist, model.HistoryJob{ID: id(string(rune('a' + i))), State: model.StateCompleted, End: now.Add(-time.Duration(i) * time.Hour), Eff: model.Efficiency{Mem: 0.1}})
	}
	got := Compute(Input{Now: now, History: hist})
	if len(got) != 1 || got[0].Kind != "low-eff" || got[0].Key != "low-eff:2026-09-28" || !strings.Contains(got[0].Message, "10%") {
		t.Fatalf("low eff = %v", got)
	}
	hist[2].Eff.Mem, hist[3].Eff.Mem, hist[4].Eff.Mem = 0.5, 0.6, 0.7
	if got := Compute(Input{Now: now, History: hist}); len(got) != 0 {
		t.Fatalf("median 50%% must not alert: %v", got)
	}

	var failed []model.HistoryJob
	for i := range 7 {
		failed = append(failed, model.HistoryJob{ID: id(string(rune('a' + i))), State: model.StateFailed, ExitCode: 1, End: now.Add(-time.Duration(i+1) * time.Minute)})
	}
	got = Compute(Input{Now: now, Ended: failed[:1], History: failed})
	if len(got) != 6 || !strings.Contains(got[5].Message, "2 more jobs failed") || !strings.Contains(got[0].Message, "exited with code 1") {
		t.Fatalf("failures = %v", got)
	}
}

func TestStorageThresholds(t *testing.T) {
	u := model.Quota{UsedBytes: 50, SoftBytes: 100, HardBytes: 200}.Usage()
	if u.BlocksPct != 50 || u.FilesPct != -1 || u.Pct != 50 || u.What != "space" {
		t.Fatalf("Usage = %+v", u)
	}
	// A shared filesystem with no personal quota only informs.
	fs := Compute(Input{Now: now, Storage: []model.Quota{{Label: "Home", UsedBytes: 99, HardBytes: 100, IsFilesystemTotal: true, Grace: "6d"}}})
	if len(fs) != 1 || fs[0].Level != model.Info || fs[0].Message != "Home: the shared filesystem is 99% full (no personal quota) (grace 6d)" {
		t.Fatalf("fs total = %+v", fs)
	}
	// 89.9% is below the default 90% warning.
	if got := Compute(Input{Now: now, Storage: []model.Quota{{Label: "Home", UsedBytes: 899, HardBytes: 1000}}}); len(got) != 0 {
		t.Fatalf("89.9%% alerted: %v", got)
	}
	// Configured thresholds apply.
	got := Compute(Input{Now: now, StorageWarn: 50, StorageCrit: 80, Storage: []model.Quota{{Label: "Home", UsedBytes: 60, HardBytes: 100}}})
	if len(got) != 1 || got[0].Level != model.Warn {
		t.Fatalf("custom thresholds: %+v", got)
	}
}

func TestTimeLeftWarning(t *testing.T) {
	job := func(left time.Duration) model.Job {
		return model.Job{ID: id("812"), Name: "train", State: model.StateRunning, TimeLeft: dur(left)}
	}
	cases := []struct {
		name string
		warn time.Duration
		left time.Duration
		want bool
	}{
		{"default warns inside 10 minutes", 0, 9 * time.Minute, true},
		{"default ignores 11 minutes", 0, 11 * time.Minute, false},
		{"an hour warns at 50 minutes", time.Hour, 50 * time.Minute, true},
		{"5 minutes ignores 9", 5 * time.Minute, 9 * time.Minute, false},
		{"off never warns", -1, time.Minute, false},
	}
	for _, c := range cases {
		got := timeLeft(Input{Now: now, MyJobs: []model.Job{job(c.left)}, TimeLeft: c.warn})
		if (len(got) == 1) != c.want {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
	got := timeLeft(Input{Now: now, MyJobs: []model.Job{job(9 * time.Minute)}})
	if len(got) != 1 || got[0].Message != "812 train has 9m left: checkpoint or save now" || got[0].Key != "timelimit:812" {
		t.Errorf("alert = %+v", got)
	}
}

func TestIdleJobs(t *testing.T) {
	running := func(id string, used time.Duration, cpus int, memMB int64, gpus int) model.Job {
		return model.Job{ID: model.JobID{Raw: id}, Name: "train", State: model.StateRunning, TimeUsed: used, CPUs: cpus, MemPerNodeMB: memMB, Nodes: 1, GPUs: gpus}
	}
	// A sample of CPU seconds: busy is the fraction of the job's CPUs.
	stat := func(j model.Job, busy float64, rssMB int64) model.JobStat {
		return model.JobStat{NTasks: 1, MaxRSSMB: rssMB, TotalCPU: time.Duration(busy * float64(j.CPUs) * float64(j.TimeUsed))}
	}
	in := func(j model.Job, st model.JobStat) Input {
		return Input{Now: now, MyJobs: []model.Job{j}, Stats: map[string]model.JobStat{j.ID.Raw: st}, StatsAt: now.Add(-time.Minute), Waste: true}
	}

	idleCPU := running("1", 45*time.Minute, 16, 64*1024, 0)
	got := idleJobs(in(idleCPU, stat(idleCPU, 0.03, 40000)))
	if len(got) != 1 || got[0].Key != "waste:1" || got[0].Level != model.Info ||
		got[0].Message != "1 train uses 3% of its 16 CPUs after 45m: ask for less next time" {
		t.Errorf("idle CPUs: %+v", got)
	}

	idleMem := running("2", time.Hour, 4, 64*1024, 0)
	got = idleJobs(in(idleMem, stat(idleMem, 0.9, 2048)))
	if len(got) != 1 || !strings.Contains(got[0].Message, "3% of its 64G memory") {
		t.Errorf("idle memory: %+v", got)
	}

	gpu := running("3", time.Hour, 4, 16*1024, 2)
	st := stat(gpu, 0.9, 8000)
	st.GPUUtil, st.HasGPUUtil = 0.07, true
	got = idleJobs(in(gpu, st))
	if len(got) != 1 || !strings.Contains(got[0].Message, "its GPU only 7% of the time") {
		t.Errorf("idle GPU: %+v", got)
	}

	// Nothing to say: busy enough, too new, one CPU, no GPU figure, an array
	// task, stale samples, or the warning off.
	busy := running("4", time.Hour, 8, 32*1024, 0)
	young := running("5", 20*time.Minute, 16, 64*1024, 0)
	single := running("6", time.Hour, 1, 512, 0)
	noUtil := running("7", time.Hour, 4, 16*1024, 1)
	array := running("8_3", time.Hour, 16, 64*1024, 0)
	for name, input := range map[string]Input{
		"busy":         in(busy, stat(busy, 0.8, 20000)),
		"too new":      in(young, stat(young, 0.01, 100)),
		"one cpu":      in(single, stat(single, 0.01, 10)),
		"no gpu data":  in(noUtil, stat(noUtil, 0.9, 8000)),
		"array task":   in(array, stat(array, 0.01, 100)),
		"stale sample": func() Input { i := in(idleCPU, stat(idleCPU, 0.03, 40000)); i.StatsAt = now.Add(-time.Hour); return i }(),
		"warning off":  func() Input { i := in(idleCPU, stat(idleCPU, 0.03, 40000)); i.Waste = false; return i }(),
		"no sample":    {Now: now, MyJobs: []model.Job{idleCPU}, Waste: true},
	} {
		if got := idleJobs(input); len(got) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}
}
