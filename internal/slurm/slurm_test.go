package slurm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestCommandsMatchSpec(t *testing.T) {
	c := Commands{Caps: model.Capabilities{Major: 23, Minor: 11, HasMe: true}, User: "alice"}
	us := "\x1f"
	checks := map[string][]string{
		"myjobs":       c.MyJobs(),
		"alljobs":      c.AllJobs(),
		"cluster":      c.Cluster(nil),
		"cluster-part": c.Cluster([]string{"gpu", "cpu"}),
		"queuerank":    c.QueueRank([]string{"gpu"}),
		"nodes":        c.Nodes(),
		"partitions":   c.Partitions(),
		"reservations": c.Reservations(),
		"jobdetail":    c.JobDetail("812"),
		"sstat":        c.Sstat("812"),
		"history":      c.History(7),
		"historyjob":   c.HistoryJob("812"),
		"fairshare":    c.Fairshare(),
		"sprio":        c.Priorities(),
		"script":       c.BatchScript("812"),
		"sacctscript":  c.SacctBatchScript("812"),
		"final":        c.FinalStates([]string{"1", "2"}),
		"submitline":   c.SubmitLine("812"),
		"version":      Version(),
		"config":       Config(),
	}
	for name, argv := range checks {
		if execx.Classify(argv) != execx.ReadOnly {
			t.Errorf("%s must be read-only: %q", name, argv)
		}
	}
	if got := strings.Join(checks["myjobs"][:4], " "); got != "squeue --me -h -o" {
		t.Errorf("myjobs = %q", got)
	}
	if f := checks["myjobs"][4]; strings.Count(f, us) != 22 || !strings.HasSuffix(f, "%u"+us+"%j") {
		t.Errorf("myjobs format = %q", f)
	}
	if !slices.Contains(checks["cluster-part"], "gpu,cpu") || slices.Contains(checks["cluster"], "-p") {
		t.Errorf("cluster partitions: %q / %q", checks["cluster-part"], checks["cluster"])
	}
	if h := checks["history"]; !slices.Contains(h, "now-7days") || !slices.Contains(h, "alice") || !slices.Contains(h, "--delimiter="+us) {
		t.Errorf("history = %q", h)
	}
	if h := c.History(90); !slices.Contains(h, "now-30days") {
		t.Errorf("history must cap at 30 days: %q", h)
	}
	if h := c.History(0); !slices.Contains(h, "now-1days") {
		t.Errorf("history floor: %q", h)
	}
	if f := checks["final"]; !slices.Contains(f, "1,2") || !slices.Contains(f, "-X") {
		t.Errorf("final = %q", f)
	}

	old := Commands{Caps: model.Capabilities{Major: 20, Minor: 2}, User: "bob"}
	if argv := old.MyJobs(); !slices.Contains(argv, "-u") || !slices.Contains(argv, "bob") || slices.Contains(argv, "--me") {
		t.Errorf("pre-20.11 must use -u: %q", argv)
	}
}

func TestUser(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	f := execx.NewFake()
	if u, err := User(context.Background(), env(map[string]string{"USER": "alice"}), f); err != nil || u != "alice" || len(f.Calls()) != 0 {
		t.Fatalf("User from $USER = %q %v", u, err)
	}
	f.Set([]string{"id", "-un"}, execx.FakeResponse{Stdout: []byte("bob\n")})
	if u, err := User(context.Background(), env(nil), f); err != nil || u != "bob" {
		t.Fatalf("User from id = %q %v", u, err)
	}
	f2 := execx.NewFake()
	f2.Set([]string{"id", "-un"}, execx.FakeResponse{Stdout: []byte("\n")})
	if _, err := User(context.Background(), env(nil), f2); err == nil {
		t.Fatal("empty id output must fail")
	}
	if _, err := User(context.Background(), env(nil), execx.NewFake()); !errors.Is(err, execx.ErrNoFixture) {
		t.Fatalf("id failure: %v", err)
	}
}

func probeFake(version string) *execx.FakeRunner {
	f := execx.NewFake()
	f.Set(Version(), execx.FakeResponse{Stdout: []byte(version)})
	f.Set([]string{"sacct", "-n", "-X", "-S", "now-1minutes", "-E", "now", "-o", "JobID"}, execx.FakeResponse{})
	f.Set([]string{"sshare", "-U", "-n", "-P", "-o", "User"}, execx.FakeResponse{Stderr: []byte("sshare: error: not supported"), ExitCode: 1})
	f.Set([]string{"sprio", "-h", "-u", "alice", "-o", "%i"}, execx.FakeResponse{})
	f.Set(Config(), execx.FakeResponse{Stdout: []byte("Foo = 1\nClusterName             = mycluster\n")})
	return f
}

func TestProbeAndCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	f := probeFake("slurm 23.11.4\n")

	caps, err := CachedProbe(context.Background(), f, "alice", dir, now)
	if err != nil || caps.Version != "23.11.4" || !caps.HasMe || !caps.HasSacct || caps.HasSshare || !caps.HasSprio {
		t.Fatalf("caps = %+v %v", caps, err)
	}
	fi, err := os.Stat(filepath.Join(dir, "capabilities.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cache file: %v %v", fi, err)
	}
	calls := len(f.Calls())

	caps2, err := CachedProbe(context.Background(), f, "alice", dir, now.Add(time.Hour))
	if err != nil || caps2 != caps || len(f.Calls()) != calls+1 { // only sinfo --version again
		t.Fatalf("cached probe re-ran probes: %d calls (was %d)", len(f.Calls()), calls)
	}
	_, _ = CachedProbe(context.Background(), f, "alice", dir, now.Add(25*time.Hour))
	if len(f.Calls()) <= calls+2 {
		t.Fatal("expired cache must probe again")
	}

	upgraded := probeFake("slurm 24.05.1\n")
	caps3, _ := CachedProbe(context.Background(), upgraded, "alice", dir, now.Add(2*time.Hour))
	if caps3.Version != "24.05.1" || len(upgraded.Calls()) < 4 {
		t.Fatalf("a new version must invalidate the cache: %+v", caps3)
	}

	if _, err := CachedProbe(context.Background(), execx.NewFake(), "alice", dir, now); err == nil {
		t.Fatal("missing sinfo must fail")
	}
	bad := execx.NewFake()
	bad.Set(Version(), execx.FakeResponse{Stdout: []byte("???")})
	if _, err := CachedProbe(context.Background(), bad, "alice", dir, now); err == nil {
		t.Fatal("unparseable version must fail")
	}
	if c, err := Probe(context.Background(), f, "alice"); err != nil || c.Version != "23.11.4" {
		t.Fatalf("Probe = %+v %v", c, err)
	}
	if _, err := Probe(context.Background(), execx.NewFake(), "alice"); err == nil {
		t.Fatal("Probe without sinfo")
	}
	if _, err := Probe(context.Background(), bad, "alice"); err == nil {
		t.Fatal("Probe with bad version")
	}
}

func TestSstatProbeUsesRunner(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	f := probeFake("slurm 23.11.4\n")
	f.Set([]string{"sstat", "--version"}, execx.FakeResponse{Stdout: []byte("slurm 23.11.4\n")})
	caps, err := Probe(context.Background(), f, "alice")
	if err != nil || !caps.HasSstat {
		t.Fatalf("simulated sstat must work without a host installation: %+v %v", caps, err)
	}
	f.Set([]string{"sstat", "--version"}, execx.FakeResponse{ExitCode: 1})
	caps, err = Probe(context.Background(), f, "alice")
	if err != nil || caps.HasSstat {
		t.Fatalf("failed sstat probe must disable usage: %+v %v", caps, err)
	}
}

func TestClusterInfoCache(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	f := probeFake("slurm 23.11.4")
	f.Set(Config(), execx.FakeResponse{Stdout: []byte("ClusterName = mycluster\nPrivateData = jobs,usage\n")})
	info, err := ClusterInfo(context.Background(), f, dir, now)
	if err != nil || info.Name != "mycluster" || !info.HidesJobs() {
		t.Fatalf("ClusterInfo = %+v %v", info, err)
	}
	n := len(f.Calls())
	if info, _ := ClusterInfo(context.Background(), f, dir, now.Add(time.Hour)); info.Name != "mycluster" || !info.HidesJobs() || len(f.Calls()) != n {
		t.Fatal("cluster info must come from the cache")
	}
	if _, err := ClusterInfo(context.Background(), execx.NewFake(), t.TempDir(), now); err == nil {
		t.Fatal("no scontrol")
	}
	empty := execx.NewFake()
	empty.Set(Config(), execx.FakeResponse{Stdout: []byte("nothing\n")})
	if _, err := ClusterInfo(context.Background(), empty, t.TempDir(), now); err == nil {
		t.Fatal("no ClusterName line")
	}
	if err := writeJSON("", 1); err == nil {
		t.Fatal("writeJSON without a directory")
	}
}
