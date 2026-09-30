package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
)

// demoRun runs sdash against the mixed demo cluster.
func demoRun(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	h := newHarness(t)
	code := h.run(append(args, "--demo=hetero")...)
	return h.stdout.String(), h.stderr.String(), code
}

func wantAll(t *testing.T, what, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, out)
		}
	}
}

func TestNodesCommand(t *testing.T) {
	out, errOut, code := demoRun(t, "nodes")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	wantAll(t, "nodes", out, "cpu01", "login01", "h200-01", "H200 NVL", "RTX PRO 6000 BW", "4/11", "not tracked", `"fan failure"`)
	out, _, _ = demoRun(t, "nodes", "--cpu")
	if strings.Contains(out, "h200") || !strings.Contains(out, "cpu05") {
		t.Errorf("--cpu:\n%s", out)
	}
	out, _, _ = demoRun(t, "nodes", "--gpu", "-p", "ws")
	if strings.Contains(out, "cpu0") || strings.Contains(out, "h200") || !strings.Contains(out, "ws02") {
		t.Errorf("--gpu -p ws:\n%s", out)
	}
	out, _, _ = demoRun(t, "nodes", "--state", "drained")
	if !strings.Contains(out, "cpu04") || strings.Contains(out, "cpu05") {
		t.Errorf("--state drained:\n%s", out)
	}
	out, _, _ = demoRun(t, "nodes", "mig01")
	wantAll(t, "nodes mig01", out, "MIG 1g.33gb", "MIG 1g.16gb", "not tracked by Slurm", "Running jobs", "dave")
	if _, errOut, code = demoRun(t, "nodes", "nope"); code != ExitError || !strings.Contains(errOut, `no node "nope"`) {
		t.Errorf("unknown node: %d %s", code, errOut)
	}
	if _, _, code = demoRun(t, "nodes", "--gpu", "--cpu"); code != ExitUsage {
		t.Errorf("--gpu --cpu: exit %d", code)
	}
	out, _, _ = demoRun(t, "nodes", "--json")
	var doc report.NodesDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Nodes) != 12 {
		t.Fatalf("json: %v %d", err, len(doc.Nodes))
	}
	for _, n := range doc.Nodes {
		if n.Name == "cpu01" && n.MemAllocMB != nil {
			t.Errorf("cpu01 memory should be untracked (null): %+v", n)
		}
	}
}

func TestPartitionsQueueHistory(t *testing.T) {
	out, errOut, code := demoRun(t, "partitions")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	wantAll(t, "partitions", out, "cpu*", "1/8 H200 NVL", "7/11 MIG", "login")
	out, _, _ = demoRun(t, "queue", "--group-by", "user")
	wantAll(t, "queue by user", out, "you: ", "carol: ", "alice")
	out, _, _ = demoRun(t, "queue", "-p", "gpu", "--state", "PD")
	if !strings.Contains(out, "finetune-4x") || strings.Contains(out, "RUNNING") || strings.Contains(out, "stats") {
		t.Errorf("queue -p gpu --state PD:\n%s", out)
	}
	if _, _, code = demoRun(t, "queue", "--group-by", "colour"); code != ExitUsage {
		t.Errorf("bad --group-by: exit %d", code)
	}
	out, _, _ = demoRun(t, "history", "--state", "failed")
	wantAll(t, "history failed", out, "TIMEOUT", "OUT_OF_MEMORY", "FAILED")
	if strings.Contains(out, "COMPLETED") {
		t.Errorf("history --state failed shows completed jobs:\n%s", out)
	}
	if _, _, code = demoRun(t, "history", "--days", "31"); code != ExitUsage {
		t.Errorf("--days 31: exit %d", code)
	}
	out, _, _ = demoRun(t, "gpus", "--all-nodes")
	wantAll(t, "gpus --all-nodes", out, "cpu03", "h200-01")
	out, _, _ = demoRun(t, "gpus")
	if strings.Contains(out, "cpu03") {
		t.Errorf("gpus lists CPU nodes:\n%s", out)
	}
}

func TestFreeCommand(t *testing.T) {
	out, errOut, code := demoRun(t, "free", "--gpus", "1", "--gpu-type", "h200", "--time", "4h", "--test")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	wantAll(t, "free h200", out, "Fits now on 1 node", "h200-01", "would start", "in gpu")
	out, _, _ = demoRun(t, "free", "--gpus", "4", "--gpu-type", "H200 NVL")
	wantAll(t, "free 4 h200", out, "Nothing fits now. Soonest:")
	out, _, _ = demoRun(t, "free", "--gpus", "1", "--gpu-type", "1g.16gb", "--json")
	var doc report.FreeDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Now) != 1 || doc.Now[0].Node != "mig01" || doc.Now[0].FreeMemMB != nil {
		t.Fatalf("free MIG json: %v %+v", err, doc.Now)
	}
	out, _, _ = demoRun(t, "free", "--cpus", "48", "--time", "2-00:00:00", "-p", "cpu")
	wantAll(t, "free cpu", out, "cpu03")
	for _, bad := range [][]string{{"free", "--time", "soon"}, {"free", "--mem", "lots"}, {"free", "--cpus", "0"}} {
		if _, _, code := demoRun(t, bad...); code != ExitUsage {
			t.Errorf("%v: exit %d", bad, code)
		}
	}
}

func TestStatusWatch(t *testing.T) {
	h := newHarness(t)
	a := &app{env: h.env()}
	var out bytes.Buffer
	a.env.Stdout = &out
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- run(ctx, newRoot(a), a, []string{"status", "--demo=hetero", "--watch=2s", "--json"}) }()
	if code := <-done; code != ExitOK {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 1 {
		t.Fatalf("no documents: %q", out.String())
	}
	var doc report.StatusDoc
	if err := json.Unmarshal([]byte(lines[0]), &doc); err != nil || doc.Cluster != "birch" {
		t.Fatalf("ndjson line: %v %q", err, lines[0])
	}
	if code := h.run("status", "--demo", "--watch=1s"); code != ExitUsage {
		t.Errorf("--watch=1s: exit %d", code)
	}
}

func TestCompletions(t *testing.T) {
	h := newHarness(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"__complete", "nodes", "--demo=hetero", ""}, "mig01"},
		{[]string{"__complete", "why", "--demo=hetero", ""}, "4106"},
		{[]string{"__complete", "queue", "--demo=hetero", "-p", ""}, "gpu"},
		{[]string{"__complete", "--demo="}, "hetero"},
		{[]string{"__complete", "queue", "--group-by", ""}, "partition"},
	} {
		h.run(c.args...)
		if !strings.Contains(h.stdout.String(), c.want) {
			t.Errorf("%v lacks %q: %q %q", c.args, c.want, h.stdout.String(), h.stderr.String())
		}
	}
}

// TestNoDashboardAnonymize: --anonymize was removed from the dashboard and
// data commands; only "sdash record --anonymize" keeps it.
func TestNoDashboardAnonymize(t *testing.T) {
	if _, _, code := demoRun(t, "queue", "--anonymize"); code != ExitUsage {
		t.Errorf("queue --anonymize: exit %d, want a usage error", code)
	}
}

// gpus, partitions and eff are hidden aliases: the everyday forms are
// nodes --gpu, nodes --partitions and history JOBID, with the same output.
func TestMergedCommands(t *testing.T) {
	old, _, _ := demoRun(t, "partitions")
	now, _, code := demoRun(t, "nodes", "--partitions")
	if code != ExitOK || old != now || !strings.Contains(now, "PARTITION") {
		t.Errorf("nodes --partitions differs from partitions (exit %d):\n%s\nvs\n%s", code, now, old)
	}
	oldJSON, _, _ := demoRun(t, "partitions", "--json")
	nowJSON, _, _ := demoRun(t, "nodes", "--partitions", "--json")
	stamp := regexp.MustCompile(`"generated_at": "[^"]*"`)
	if stamp.ReplaceAllString(oldJSON, "") != stamp.ReplaceAllString(nowJSON, "") {
		t.Error("nodes --partitions --json differs from partitions --json")
	}
	for _, args := range [][]string{{"nodes", "--partitions", "mig01"}, {"nodes", "--partitions", "--gpu"}, {"nodes", "--partitions", "-p", "gpu"}} {
		if _, _, code := demoRun(t, args...); code != ExitUsage {
			t.Errorf("%v: exit %d, want a usage error", args, code)
		}
	}

	h := newHarness(t)
	h.run("eff", "--demo", "806")
	card := h.stdout.String()
	h2 := newHarness(t)
	code = h2.run("history", "--demo", "806")
	if code != ExitOK || h2.stdout.String() != card || !strings.Contains(card, "eval-bench") {
		t.Errorf("history 806 differs from eff 806 (exit %d):\n%s\nvs\n%s", code, h2.stdout.String(), card)
	}
	if _, _, code := demoRun(t, "history", "806", "--state", "failed"); code != ExitUsage {
		t.Errorf("history JOBID --state: exit %d, want a usage error", code)
	}

	help := newHarness(t)
	help.run("--help")
	for _, hidden := range []string{"  gpus ", "  partitions ", "  eff "} {
		if strings.Contains(help.stdout.String(), hidden) {
			t.Errorf("--help still lists%q", hidden)
		}
	}
}

func TestUsageCommand(t *testing.T) {
	out, errOut, code := demoRun(t, "usage")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	wantAll(t, "usage", out, "Your jobs over the last 7 days: 7 finished", "Outcomes", "Failed", "Hours", "CPU ",
		"H200 NVL", "Wasted", "CPU idle", "GPU idle", "Least efficient completed jobs", "--time=",
		"Limits", "you in lab-you", "GPUs in use", "longest job", "Priority", "fairshare 0.48", "over your share", "job 4105 pending")

	out, _, _ = demoRun(t, "usage", "--json")
	var doc struct {
		Schema   int `json:"schema"`
		Days     int `json:"days"`
		Finished int `json:"finished_jobs"`
		Outcomes []struct {
			State string `json:"state"`
			Count int    `json:"count"`
		} `json:"outcomes"`
		GPUTypes []struct {
			GPUType string `json:"gpu_type"`
			Display string `json:"gpu_type_display"`
		} `json:"gpu_types"`
		Waste struct {
			CPUJobs      int      `json:"cpu_jobs"`
			IdleCPUHours float64  `json:"idle_cpu_hours"`
			IdleGPUHours *float64 `json:"idle_gpu_hours"`
		} `json:"waste"`
		Worst []struct {
			ID string `json:"id"`
		} `json:"least_efficient"`
		Limits []struct {
			Scope string   `json:"scope"`
			Name  string   `json:"name"`
			Max   float64  `json:"max"`
			Used  *float64 `json:"used"`
		} `json:"limits"`
		Fairshare []struct {
			Account string `json:"account"`
		} `json:"fairshare"`
		Pending []struct {
			Job string `json:"job"`
		} `json:"pending_priority"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if doc.Schema != 1 || doc.Days != 7 || doc.Finished != 7 || len(doc.Outcomes) == 0 || doc.Waste.CPUJobs == 0 ||
		doc.Waste.IdleGPUHours == nil || len(doc.Worst) == 0 || len(doc.GPUTypes) == 0 {
		t.Errorf("doc = %+v", doc)
	}
	if len(doc.Limits) == 0 || len(doc.Fairshare) != 1 || len(doc.Pending) == 0 {
		t.Errorf("limits and priority missing: %+v", doc)
	}
	for _, l := range doc.Limits {
		if l.Name == "MaxWall" && l.Used != nil {
			t.Errorf("a per-job cap has no usage: %+v", l)
		}
	}
	seen := false
	for _, g := range doc.GPUTypes {
		seen = seen || (g.GPUType == "nvidia_h200_nvl" && g.Display == "H200 NVL")
	}
	if !seen {
		t.Errorf("raw and display GPU names missing: %+v", doc.GPUTypes)
	}

	h := newHarness(t)
	if code := h.run("usage", "--demo=basic"); code != ExitError || !strings.Contains(h.stderr.String(), "job accounting") {
		t.Errorf("usage without accounting: exit %d %q", code, h.stderr.String())
	}
	if _, _, code := demoRun(t, "usage", "--days", "31"); code != ExitUsage {
		t.Errorf("--days 31: exit %d", code)
	}
}

func TestRunningToSample(t *testing.T) {
	job := func(id string, state model.JobState, used time.Duration) model.Job {
		return model.Job{ID: model.JobID{Raw: id}, State: state, TimeUsed: used}
	}
	jobs := []model.Job{
		job("1", model.StateRunning, 40*time.Minute),
		job("2", model.StateRunning, 3*time.Hour),
		job("3", model.StateRunning, 10*time.Minute), // too new
		job("4", model.StatePending, 0),              // not running
		job("5_2", model.StateRunning, 4*time.Hour),  // array task
		job("6+1", model.StateRunning, 4*time.Hour),  // heterogeneous
		job("7", model.StateCompleting, 4*time.Hour), // not running
		job("", model.StateRunning, 4*time.Hour),
	}
	if got := runningToSample(jobs); !slices.Equal(got, []string{"2", "1"}) {
		t.Errorf("runningToSample = %v, want the plain jobs older than %v, longest first", got, statAge)
	}
	var many []model.Job
	for i := 1; i <= 100; i++ {
		many = append(many, job(strconv.Itoa(i), model.StateRunning, time.Hour))
	}
	if n := len(runningToSample(many)); n != maxSampled {
		t.Errorf("sampled %d jobs, want at most %d", n, maxSampled)
	}
}
