package state

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

func fixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/fixtures/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

func TestJoinJobsRealCluster(t *testing.T) {
	jobs, _ := parse.MyJobs(fixture(t, "23.11/myjobs.txt"))
	running, _ := parse.Running(fixture(t, "23.11/cluster.txt"))
	pending, _ := parse.QueueRank(fixture(t, "23.11/queuerank.txt"))
	joined := JoinJobs(jobs, running, pending)

	byID := map[string]model.Job{}
	for _, j := range joined {
		byID[j.ID.Raw] = j
	}
	if j := byID["13_[2-8%1]"]; j.QueueRank == 0 || j.QueueTotal != 5 {
		t.Fatalf("pending array must be ranked through its base ID: %+v", j)
	}
	if j := byID["17"]; j.QueueRank < 1 || j.QueueTotal != 5 {
		t.Fatalf("pending job 17 rank: %+v", j)
	}
	if j := byID["19"]; j.QueueRank != 1 || j.QueueTotal != 1 {
		t.Fatalf("only pending job in 'short': %+v", j)
	}
	if j := byID["11"]; j.GPUs != 1 || j.QueueRank != 0 {
		t.Fatalf("running job keeps its allocated GPUs: %+v", j)
	}
	if len(jobs) != len(joined) || &jobs[0] == &joined[0] {
		t.Fatal("JoinJobs must copy")
	}
}

func TestQueueRanksOrdering(t *testing.T) {
	ranks := QueueRanks([]parse.PendingJob{
		{ID: "10", Partition: "gpu", Priority: 100},
		{ID: "9", Partition: "gpu", Priority: 500},
		{ID: "11", Partition: "gpu", Priority: 100},
		{ID: "12", Partition: "cpu", Priority: 1},
	})
	want := map[string]Rank{"9": {1, 3}, "10": {2, 3}, "11": {3, 3}, "12": {1, 1}}
	for id, w := range want {
		if ranks[id] != w {
			t.Errorf("rank[%s] = %+v, want %+v", id, ranks[id], w)
		}
	}
}

func TestNodeGPUUsage(t *testing.T) {
	nodes, _ := parse.Nodes(fixture(t, "handwritten/nodes.txt"))
	running, _ := parse.Running(fixture(t, "handwritten/cluster.txt"))
	usage := NodeGPUUsage(nodes, running, "user1")
	by := map[string]NodeUsage{}
	for _, u := range usage {
		by[u.Node.Name] = u
	}

	g1 := by["gpu01"] // user1: 812 (1) + 813 (4 over 2 nodes = 2); user2: 700 (1). AllocTRES says 3.
	if g1.Mine != 3 || g1.Others != 1 || g1.Users["user2"] != 1 || !g1.Estimate {
		t.Fatalf("gpu01 = %+v", g1)
	}
	if g1.Free != 0 || g1.FreeBy.IsZero() {
		t.Fatalf("gpu01 is full, free by the earliest end: %+v", g1)
	}
	if want := time.Date(2026, 9, 28, 15, 48, 0, 0, time.UTC); !g1.FreeBy.Equal(want) {
		t.Fatalf("gpu01 free by %v, want %v", g1.FreeBy, want)
	}

	g2 := by["gpu02"] // 813 (2) + user3 701 (3) = 5 > 4 total; allocated 4
	if g2.Free != 0 || g2.Mine != 2 || g2.Users["user3"] != 3 {
		t.Fatalf("gpu02 = %+v", g2)
	}
	if want := time.Date(2026, 9, 28, 14, 5, 0, 0, time.UTC); !g2.FreeBy.Equal(want) {
		t.Fatalf("gpu02 free by %v, want %v", g2.FreeBy, want)
	}

	g3 := by["gpu03"] // drained: nothing is free even though 7 of 8 are idle
	if g3.Free != 0 || Available(g3.Node) {
		t.Fatalf("drained gpu03 = %+v", g3)
	}
	if by["cpu08"].Free != 0 || Available(by["cpu08"].Node) {
		t.Fatal("not-responding node is unavailable")
	}

	// PrivateData=jobs: allocated GPUs without visible jobs count as others.
	hidden := NodeGPUUsage(nodes[:1], nil, "user1")
	if hidden[0].Others != 3 || hidden[0].Users["others"] != 3 || !hidden[0].FreeBy.IsZero() {
		t.Fatalf("hidden jobs = %+v", hidden[0])
	}

	gpuOnly := GPUNodes(usage)
	if len(gpuOnly) != 3 {
		t.Fatalf("GPU nodes = %d", len(gpuOnly))
	}
	idle := model.Node{Name: "a", State: "IDLE", GPUTotal: 4}
	mixed := model.Node{Name: "b", State: "MIXED", GPUTotal: 4, GPUAlloc: 2}
	list := NodeGPUUsage([]model.Node{mixed, idle}, nil, "me")
	SortByFree(list)
	if list[0].Node.Name != "a" || list[0].Free != 4 || list[1].Free != 2 || !list[0].FreeBy.IsZero() {
		t.Fatalf("sort by free = %+v", list)
	}
	if GPUShare(model.RunningJob{GPUs: 3}) != 0 || GPUShare(model.RunningJob{GPUs: 3, NodeList: []string{"a", "b"}}) != 2 {
		t.Fatal("GPUShare")
	}
}

func TestStoreApplyAndTransitions(t *testing.T) {
	var s Store
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	j := func(id string, st model.JobState) model.Job {
		return model.Job{ID: model.JobID{Raw: id}, State: st}
	}
	if tr := s.Apply(Update{Source: "myjobs", At: now, Data: []model.Job{j("1", model.StatePending), j("2", model.StateRunning), j("3", model.StateRunning)}}); tr != nil {
		t.Fatalf("first snapshot has no transitions: %v", tr)
	}
	tr := s.Apply(Update{Source: "myjobs", At: now.Add(10 * time.Second), Data: []model.Job{j("1", model.StateRunning), j("3", model.StateCompleting), j("4", model.StatePending)}})
	if len(tr) != 3 {
		t.Fatalf("transitions = %+v", tr)
	}
	kinds := map[string]TransitionKind{}
	for _, x := range tr {
		kinds[x.Job.ID.Raw] = x.Kind
	}
	if kinds["1"] != Started || kinds["2"] != Ended || kinds["3"] != Changed {
		t.Fatalf("kinds = %v", kinds)
	}

	s.Apply(Update{Source: "myjobs", At: now.Add(20 * time.Second), Err: os.ErrDeadlineExceeded})
	if s.MyJobs.Err == nil || len(s.MyJobs.Data) != 3 || !s.MyJobs.Has {
		t.Fatal("a failed update keeps the last good data")
	}
	if f := s.MyJobs.Freshness(now.Add(21*time.Second), 10*time.Second); f != Failing {
		t.Fatalf("freshness = %v, want failing", f)
	}
	s.Apply(Update{Source: "myjobs", At: now.Add(30 * time.Second), Data: []model.Job{}})
	if f := s.MyJobs.Freshness(now.Add(35*time.Second), 10*time.Second); f != Fresh {
		t.Fatalf("freshness = %v, want fresh", f)
	}
	if f := s.MyJobs.Freshness(now.Add(60*time.Second), 10*time.Second); f != Stale {
		t.Fatalf("freshness = %v, want stale", f)
	}
	if f := s.Nodes.Freshness(now, time.Second); f != Unknown {
		t.Fatalf("never loaded = %v", f)
	}

	for _, src := range []string{"alljobs", "cluster", "queuerank", "nodes", "partitions", "reservations", "jobdetail", "history", "fairshare", "sprio", "storage", "unknown"} {
		s.Apply(Update{Source: src, At: now, Data: struct{}{}}) // wrong type is ignored
	}
	s.Apply(Update{Source: "nodes", At: now, Data: []model.Node{{Name: "n1"}}})
	if !s.Nodes.Has || s.Nodes.Data[0].Name != "n1" {
		t.Fatal("nodes not applied")
	}
}

func TestPartitionsAndCounts(t *testing.T) {
	jobs := []model.Job{
		{State: model.StatePending, Partition: "gpu,cpu"},
		{State: model.StatePending, Partition: "gpu"},
		{State: model.StateRunning, Partition: "short"},
		{State: model.StateCompleting},
		{State: model.StateSuspended},
	}
	if got := PartitionsOfPending(jobs); len(got) != 2 || got[0] != "cpu" || got[1] != "gpu" {
		t.Fatalf("PartitionsOfPending = %v", got)
	}
	if c := CountJobs(jobs); c.Running != 2 || c.Pending != 2 || c.Other != 1 {
		t.Fatalf("CountJobs = %+v", c)
	}
}

func TestAddEndedAndAlerts(t *testing.T) {
	var s Store
	for i := range MaxEnded + 5 {
		s.AddEnded(model.HistoryJob{ID: model.JobID{Raw: strconv.Itoa(i)}})
	}
	if len(s.Ended) != MaxEnded || s.Ended[0].ID.Raw != "5" {
		t.Fatalf("Ended = %d, first %s", len(s.Ended), s.Ended[0].ID.Raw)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s.Ended = []model.HistoryJob{{ID: model.JobID{Raw: "9"}, State: model.StateFailed, ExitCode: 1, End: now.Add(-time.Minute)}}
	if a := s.Alerts(now, nil); len(a) != 1 || a[0].JobID != "9" {
		t.Fatalf("Alerts = %v", a)
	}
}

// MIG slices are counted apart from whole GPUs: a node with 4 + 7 slices
// is not "1 of 11 GPUs".
func TestMIGUsage(t *testing.T) {
	nodes, _ := parse.Nodes(fixture(t, "handwritten/nodes-mig.txt"))
	running, _ := parse.Running(fixture(t, "handwritten/cluster-typed.txt"))
	usage := NodeGPUUsage(nodes, running, "alice")
	byName := map[string]NodeUsage{}
	for _, u := range usage {
		byName[u.Node.Name] = u
	}
	mig := byName["mig01"]
	if mig.Node.GPUTotal != 0 || mig.Node.MIGTotal != 11 || mig.MIGMine != 1 || mig.MIGFree != 10 || mig.Free != 0 || !mig.FreeBy.IsZero() {
		t.Errorf("mig01 = %+v", mig)
	}
	h := byName["h200-01"]
	if h.Free != 2 || h.Others != 2 || h.Users["bob"] != 2 || h.MIGFree != 0 {
		t.Errorf("h200-01 = %+v", h)
	}
	if byName["cpu05"].Node.MemTracked() {
		t.Error("cpu05 has jobs but no allocated memory: memory is not tracked")
	}
	got := SumGPUs(GPUNodes(usage))
	if got != (GPUTotals{Total: 4, Free: 2, MIGTotal: 11, MIGFree: 10, NodesWithFree: 2}) {
		t.Errorf("SumGPUs = %+v", got)
	}
}

// TestUntypedGPUsOnMixedNode uses what Slurm 23.11 printed for a node with
// two whole GPUs and two MIG slices: only an untyped gres/gpu total, and
// untyped counts on the jobs.
func TestUntypedGPUsOnMixedNode(t *testing.T) {
	nodes, _ := parse.Nodes(fixture(t, "23.11/nodes-typed.txt"))
	running, _ := parse.Running(fixture(t, "23.11/cluster-typed.txt"))
	for _, u := range NodeGPUUsage(nodes, running, "user1") {
		if u.Node.Name != "node01" {
			continue
		}
		if u.Mine+u.Others != u.Node.GPUAlloc || u.MIGMine+u.MIGOthers != u.Node.MIGAlloc || u.Users["others"] != 0 {
			t.Errorf("node01 double counts: %+v", u)
		}
		if !u.Estimate {
			t.Error("the split of untyped GPUs is an estimate")
		}
		return
	}
	t.Fatal("node01 missing")
}

// TestApplyEverySource guards against a collector whose updates the store
// silently drops: each source's data must land in its slot.
func TestApplyEverySource(t *testing.T) {
	var s Store
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		source string
		data   any
		has    func() bool
	}{
		{"myjobs", []model.Job{}, func() bool { return s.MyJobs.Has }},
		{"alljobs", []model.Job{}, func() bool { return s.AllJobs.Has }},
		{"cluster", ClusterData{}, func() bool { return s.Cluster.Has }},
		{"queuerank", []parse.PendingJob{}, func() bool { return s.QueueRank.Has }},
		{"nodes", []model.Node{}, func() bool { return s.Nodes.Has }},
		{"partitions", []model.Partition{}, func() bool { return s.Partitions.Has }},
		{"reservations", []model.Reservation{}, func() bool { return s.Reservations.Has }},
		{"jobdetail", Detail{}, func() bool { return s.Detail.Has }},
		{"history", HistoryData{}, func() bool { return s.History.Has }},
		{"fairshare", []model.Share{}, func() bool { return s.Fairshare.Has }},
		{"sprio", []model.PriorityFactors{}, func() bool { return s.Priorities.Has }},
		{"limits", []model.LimitScope{}, func() bool { return s.Limits.Has }},
		{"mystats", map[string]model.JobStat{}, func() bool { return s.MyStats.Has }},
		{"storage", []model.Quota{}, func() bool { return s.Storage.Has }},
	}
	for _, c := range cases {
		s.Apply(Update{Source: c.source, At: now, Data: c.data})
		if !c.has() {
			t.Errorf("source %q: data was not applied", c.source)
		}
	}
}
