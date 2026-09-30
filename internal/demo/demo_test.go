package demo

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.Local)

func setup(t *testing.T) (*Sim, *state.FakeClock, *state.Sources) {
	t.Helper()
	return setupScenario(t, "default")
}

func setupScenario(t *testing.T, name string) (*Sim, *state.FakeClock, *state.Sources) {
	t.Helper()
	sc, err := Load(name)
	if err != nil {
		t.Fatal(err)
	}
	clock := state.NewFakeClock(t0)
	sim := New(sc, clock)
	r := sim.Runner()
	caps, err := slurm.Probe(context.Background(), r, sc.User)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.HasMe || caps.HasSacct == sc.NoAccounting || caps.HasSshare == sc.NoAccounting || caps.HasSprio == sc.NoAccounting {
		t.Fatalf("caps = %+v", caps)
	}
	return sim, clock, &state.Sources{Runner: r, Cmd: slurm.Commands{Caps: caps, User: sc.User}}
}

// TestEverySourceParsesCleanly runs every collector source against the
// simulation through the real parsers and requires zero parse warnings.
func TestEverySourceParsesCleanly(t *testing.T) {
	names := Names()
	if len(names) < 3 {
		t.Fatalf("scenarios = %v", names)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) { parsesCleanly(t, name) })
	}
}

func parsesCleanly(t *testing.T, name string) {
	sim, clock, src := setupScenario(t, name)
	ctx := context.Background()
	var ids []string
	for _, j := range sim.Scenario().Jobs[:2] {
		ids = append(ids, strconv.Itoa(j.ID))
	}
	for _, at := range []time.Duration{0, 100 * time.Second, 5 * time.Minute, 9*time.Minute + 59*time.Second, 11 * time.Minute} {
		clock.Advance(t0.Add(at).Sub(clock.Now()))
		mine, err := src.MyJobs(ctx)
		must(t, err)
		_, err = src.AllJobs(ctx)
		must(t, err)
		_, err = src.Cluster(ctx, nil)
		must(t, err)
		_, err = src.QueueRank(ctx, state.PartitionsOfPending(mine))
		must(t, err)
		_, err = src.Nodes(ctx)
		must(t, err)
		_, err = src.Partitions(ctx)
		must(t, err)
		_, err = src.Reservations(ctx)
		must(t, err)
		for _, j := range mine {
			_, err = src.JobDetail(ctx, j.ID.Raw)
			must(t, err)
			if j.State == model.StateRunning && !sim.Scenario().NoUsageGather {
				_, err = src.JobStat(ctx, j.ID.Raw)
				must(t, err)
			}
			_, err = src.BatchScript(ctx, j.ID.Raw)
			must(t, err)
		}
		if src.Cmd.Caps.HasSacct {
			for _, days := range []int{1, 7, 30} {
				_, err = src.History(ctx, days)
				must(t, err)
			}
			_, err = src.Fairshare(ctx)
			must(t, err)
			_, err = src.Priorities(ctx)
			must(t, err)
			_, err = src.FinalStates(ctx, ids)
			must(t, err)
		}
		if w := src.WarningCounts(); len(w) > 0 {
			t.Fatalf("at %v: parse warnings %v", at, w)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func byID(jobs []model.Job) map[string]model.Job {
	m := map[string]model.Job{}
	for _, j := range jobs {
		m[j.ID.Raw] = j
	}
	return m
}

// TestScript follows the scripted story: 816 fails OOM at 90 s, 813
// completes at 4 min and the first sweep task takes its GPU; the loop
// then starts over.
func TestScript(t *testing.T) {
	_, clock, src := setup(t)
	ctx := context.Background()
	st := &state.Store{User: "you"}
	apply := func() []state.Transition {
		jobs, err := src.MyJobs(ctx)
		must(t, err)
		return st.Apply(state.Update{Source: "myjobs", Data: jobs, At: clock.Now()})
	}
	apply()
	jobs := byID(st.MyJobs.Data)
	if len(jobs) != 5 {
		t.Fatalf("start: %d jobs: %v", len(jobs), jobs)
	}
	if j := jobs["815_[3-9]"]; j.State != model.StatePending || j.Reason != "Resources" {
		t.Fatalf("sweep = %+v", j)
	}
	if j := jobs["813"]; j.TimeLeft == nil || *j.TimeLeft != 8*time.Minute {
		t.Fatalf("813 time left = %v", j.TimeLeft)
	}

	clock.Advance(t0.Add(91 * time.Second).Sub(clock.Now()))
	ts := apply()
	if len(ts) != 1 || ts[0].Kind != state.Ended || ts[0].Job.ID.Raw != "816" {
		t.Fatalf("at 91s: %+v", ts)
	}
	fs, err := src.FinalStates(ctx, []string{"816"})
	must(t, err)
	if len(fs) != 1 || fs[0].State != model.StateOOM {
		t.Fatalf("816 final = %+v", fs)
	}

	clock.Advance(t0.Add(4*time.Minute + time.Second).Sub(clock.Now()))
	ts = apply()
	jobs = byID(st.MyJobs.Data)
	if _, ok := jobs["813"]; ok {
		t.Fatal("813 still queued after 4 min")
	}
	if j := jobs["815_3"]; j.State != model.StateRunning || len(j.NodeList) != 1 || j.NodeList[0] != "gpu01" {
		t.Fatalf("815_3 = %+v", j)
	}
	if _, ok := jobs["815_[4-9]"]; !ok || len(ts) != 2 {
		t.Fatalf("pending remainder missing or transitions %v", ts)
	}
	nodes, err := src.Nodes(ctx)
	must(t, err)
	cl, err := src.Cluster(ctx, nil)
	must(t, err)
	for _, u := range state.NodeGPUUsage(nodes, cl.Jobs, "you") {
		if u.Node.Name == "gpu01" && (u.Mine != 2 || u.Others != 2 || u.Free != 0) {
			t.Fatalf("gpu01 usage %+v", u)
		}
	}

	h, err := src.History(ctx, 1)
	must(t, err)
	var oom, done bool
	for _, j := range h.Jobs {
		oom = oom || (j.ID.Raw == "816" && j.State == model.StateOOM && j.PeakMemMB > 0)
		done = done || (j.ID.Raw == "813" && j.State == model.StateCompleted)
	}
	if !oom || !done {
		t.Fatalf("history misses 816/813: %+v", h.Jobs)
	}

	clock.Advance(t0.Add(10*time.Minute + time.Second).Sub(clock.Now())) // loop restarts
	apply()
	if len(byID(st.MyJobs.Data)) != 5 {
		t.Fatalf("after loop: %v", st.MyJobs.Data)
	}
}

// TestActions runs actions through the real actions package: the
// mutation guard applies, and the simulation reacts.
func TestActions(t *testing.T) {
	sim, clock, src := setup(t)
	ctx := context.Background()
	r := sim.Runner()
	reg := actions.Default()
	run := func(id string, jobs []model.Job, args actions.Args) actions.Result {
		t.Helper()
		a, _ := reg.Get(id)
		argvs, err := a.Build(jobs, args)
		must(t, err)
		return actions.Run(ctx, r, a, argvs)
	}
	mine, _ := src.MyJobs(ctx)
	jobs := byID(mine)

	// Without a grant, mutations are refused by the execx policy.
	if _, err := r.Run(ctx, "scancel", "812"); !errors.Is(err, execx.ErrMutationNotAuthorized) {
		t.Fatalf("unguarded scancel: %v", err)
	}

	if res := run("hold", []model.Job{jobs["815_[3-9]"]}, nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	mine, _ = src.MyJobs(ctx)
	if j := byID(mine)["815_[3-9]"]; j.Reason != "JobHeldUser" {
		t.Fatalf("after hold: %+v", j)
	}
	clock.Advance(t0.Add(5 * time.Minute).Sub(clock.Now()))
	mine, _ = src.MyJobs(ctx)
	if _, ok := byID(mine)["815_3"]; ok {
		t.Fatal("held array task started")
	}
	if res := run("release", []model.Job{jobs["815_[3-9]"]}, nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	if res := run("release", []model.Job{jobs["818"]}, nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	mine, _ = src.MyJobs(ctx)
	if j := byID(mine)["818"]; j.Reason == "JobHeldUser" {
		t.Fatalf("818 still held: %+v", j)
	}

	if res := run("cancel", []model.Job{jobs["812"]}, nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	mine, _ = src.MyJobs(ctx)
	if _, ok := byID(mine)["812"]; ok {
		t.Fatal("812 still queued after cancel")
	}
	fs, _ := src.FinalStates(ctx, []string{"812"})
	if len(fs) != 1 || fs[0].State != model.StateCancelled {
		t.Fatalf("812 final = %+v", fs)
	}

	// Other users' jobs are refused by the simulated Slurm too.
	all, _ := src.AllJobs(ctx)
	res := run("cancel", []model.Job{byID(all)["795"]}, nil)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "permission denied") {
		t.Fatalf("cancel of bob's job: %v", res.Err)
	}
}

func TestLogFS(t *testing.T) {
	sim, clock, src := setup(t)
	ctx := context.Background()
	d, err := src.JobDetail(ctx, "812")
	if err != nil || d == nil {
		t.Fatal(err)
	}
	fsys := sim.LogFS()
	r := logs.NewReader(fsys, d.StdOut, 1<<20)
	c := r.Poll()
	if c.State != logs.Reading || !strings.Contains(string(c.Data), "step") {
		t.Fatalf("first poll = %+v", c.State)
	}
	clock.Advance(8 * time.Second)
	if c := r.Poll(); c.Reset || !strings.HasPrefix(string(c.Data), "step") {
		t.Fatalf("growth = %q reset=%v", c.Data, c.Reset)
	}
	if _, err := fsys.Open("/nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	// 816 fails with OUT_OF_MEMORY at 90 s; its stderr then has the trace.
	d816, _ := src.JobDetail(ctx, "816")
	clock.Advance(2 * time.Minute)
	c = logs.NewReader(fsys, d816.StdErr, 0).Poll()
	if !strings.Contains(string(c.Data), "oom_kill event") || !strings.Contains(string(c.Data), "Traceback") {
		t.Fatalf("816 stderr = %q", c.Data)
	}
	hl, _ := logs.NewHighlighter(nil, nil)
	buf := logs.NewBuffer(0, false, hl)
	buf.Write(c.Data)
	if buf.Line(0).Level != logs.Error {
		t.Fatal("traceback not highlighted")
	}
}

func TestGPUSampleInDemo(t *testing.T) {
	sim, _, src := setup(t)
	ctx := context.Background()
	mine, _ := src.MyJobs(ctx)
	j := byID(mine)["812"]
	argv, err := actions.GPUSampleArgv(j, "", "")
	must(t, err)
	s, err := actions.SampleGPUs(ctx, sim.Runner(), argv)
	if err != nil || len(s) != 1 || s[0].Util < 80 {
		t.Fatalf("samples = %+v %v", s, err)
	}
}

// TestHeteroGPUs checks the mixed scenario through the real parsers: typed
// whole GPUs, MIG slices counted apart, and untracked memory.
func TestHeteroGPUs(t *testing.T) {
	_, clock, src := setupScenario(t, "hetero")
	clock.Advance(5 * time.Minute) // the first sweep task has started
	nodes, err := src.Nodes(context.Background())
	must(t, err)
	by := map[string]model.Node{}
	for _, n := range nodes {
		by[n.Name] = n
	}
	if len(by) != 12 {
		t.Fatalf("%d nodes", len(by))
	}
	if n := by["h200-01"]; n.GPUTotal != 4 || n.GPUAlloc != 4 || len(n.GPUs) != 1 || n.GPUs[0].Type != "nvidia_h200_nvl" {
		t.Errorf("h200-01 = total %d alloc %d %+v", n.GPUTotal, n.GPUAlloc, n.GPUs)
	}
	mig := by["mig01"]
	if mig.GPUTotal != 0 || mig.MIGTotal != 11 || mig.MIGAlloc != 4 || !mig.HasGPUs() {
		t.Errorf("mig01 = total %d, MIG %d/%d %+v", mig.GPUTotal, mig.MIGAlloc, mig.MIGTotal, mig.GPUs)
	}
	if by["cpu01"].HasGPUs() || by["cpu01"].MemTracked() {
		t.Errorf("cpu01 = %+v", by["cpu01"])
	}
	if by["cpu06"].CPUAlloc != 32 || by["cpu06"].CPULoad < 32 {
		t.Errorf("cpu06 = alloc %d load %v", by["cpu06"].CPUAlloc, by["cpu06"].CPULoad)
	}
	if w := src.WarningCounts(); len(w) > 0 {
		t.Fatalf("parse warnings %v", w)
	}
}

// Each scenario reports its site settings, and the settings that a site can
// lack fail the matching commands the way Slurm does.
func TestSiteSettings(t *testing.T) {
	for _, name := range Names() {
		sim, _, src := setupScenario(t, name)
		ctx := context.Background()
		res, err := sim.Runner().Run(ctx, "scontrol", "show", "config")
		must(t, err)
		info, err := parse.ClusterConfig(res.Stdout)
		must(t, err)
		sc := sim.Scenario()
		if info.AccountingOff() != sc.NoAccounting || info.BasicPriority() != (sc.Priority == "basic") ||
			info.NoUsageGather() != sc.NoUsageGather || info.NoJobScripts() != (sc.NoAccounting || sc.NoJobScripts) {
			t.Errorf("%s: config says %+v, scenario %+v", name, info.Settings, sc)
		}
		if sc.Priority == "basic" && src.Cmd.Caps.HasSshare {
			t.Errorf("%s: sshare must fail with priority/basic", name)
		}
	}
}
