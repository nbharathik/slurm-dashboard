package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/report"
)

// fixtureApp runs the CLI against the recorded 23.11 cluster.
func fixtureApp(t *testing.T) (*harness, *app, *execx.FakeRunner) {
	t.Helper()
	h := newHarness(t)
	h.vars["USER"] = "user1"
	f := execx.NewFake()
	if err := f.LoadDir("../../testdata/fixtures/23.11"); err != nil {
		t.Fatal(err)
	}
	for _, probe := range [][]string{
		{"sacct", "-n", "-X", "-S", "now-1minutes", "-E", "now", "-o", "JobID"},
		{"sshare", "-U", "-n", "-P", "-o", "User"},
		{"sprio", "-h", "-u", "user1", "-o", "%i"},
		{"sacct", "-n", "-X", "-S", "now-1hours", "-o", "JobID"},
		{"id", "-un"},
	} {
		f.Set(probe, execx.FakeResponse{Stdout: []byte("user1\n")})
	}
	a := &app{env: h.env(), runner: f, now: func() time.Time { return time.Date(2026, 9, 28, 13, 11, 0, 0, time.UTC) }}
	return h, a, f
}

func runApp(h *harness, a *app, args ...string) int {
	h.stdout.Reset()
	h.stderr.Reset()
	return run(context.Background(), newRoot(a), a, args)
}

func TestStatusAgainstFixtures(t *testing.T) {
	h, a, _ := fixtureApp(t)
	if code := runApp(h, a, "status", "--json"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	var doc report.StatusDoc
	if err := json.Unmarshal(h.stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != 1 || doc.Cluster != "cluster1" || doc.User != "user1" || doc.Slurm != "23.11.4" {
		t.Fatalf("header = %+v", doc.Header)
	}
	// The recorded squeue --me: 4 running, 7 pending.
	if doc.Jobs.Running != 4 || doc.Jobs.Pending != 7 || doc.Jobs.Other != 0 {
		t.Fatalf("jobs = %+v", doc.Jobs)
	}
	if doc.GPUs.Total != 12 || doc.GPUs.Free != 0 || len(doc.Errors) != 0 {
		t.Fatalf("gpus = %+v, errors = %v", doc.GPUs, doc.Errors)
	}

	if code := runApp(h, a, "status"); code != ExitOK || !strings.Contains(h.stdout.String(), "4 running · 7 pending") {
		t.Fatalf("text status: %d %q", code, h.stdout.String())
	}
}

func TestJobsAgainstFixtures(t *testing.T) {
	h, a, _ := fixtureApp(t)
	if code := runApp(h, a, "jobs", "--json"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	var doc report.JobsDoc
	if err := json.Unmarshal(h.stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Scope != "mine" || len(doc.Jobs) != 11 || doc.Jobs[0].State != "RUNNING" {
		t.Fatalf("jobs doc = %+v", doc)
	}
	last := doc.Jobs[len(doc.Jobs)-1]
	for _, j := range doc.Jobs {
		if j.ID == "13_[2-8%1]" && j.QueueRank == 0 {
			t.Fatalf("pending array has no rank: %+v", j)
		}
	}
	if last.State != "PENDING" {
		t.Fatalf("pending jobs sort after running: %+v", last)
	}

	if code := runApp(h, a, "jobs", "--all"); code != ExitOK || !strings.Contains(h.stdout.String(), "USER") || !strings.Contains(h.stdout.String(), "user3") {
		t.Fatalf("jobs --all: %d %q", code, h.stdout.String())
	}
	if code := runApp(h, a, "jobs"); code != ExitOK || !strings.Contains(h.stdout.String(), "PartitionTimeLimit") {
		t.Fatalf("jobs text: %q", h.stdout.String())
	}
}

func TestGPUsAgainstFixtures(t *testing.T) {
	h, a, _ := fixtureApp(t)
	if code := runApp(h, a, "gpus", "--json"); code != ExitOK {
		t.Fatalf("exit %d: %s", code, h.stderr.String())
	}
	var doc report.GPUsDoc
	if err := json.Unmarshal(h.stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Nodes) != 3 || doc.Total != 12 {
		t.Fatalf("gpus doc = %+v", doc)
	}
	var node01 report.GPUNode
	for _, n := range doc.Nodes {
		if n.Name == "node01" {
			node01 = n
		}
	}
	if node01.Allocated != 4 || node01.Mine != 3 || node01.Users["user3"] != 1 || node01.FreeBy == nil {
		t.Fatalf("node01 = %+v", node01)
	}
	if code := runApp(h, a, "gpus"); code != ExitOK || !strings.Contains(h.stdout.String(), "you 3") {
		t.Fatalf("gpus text: %q", h.stdout.String())
	}
}

func TestDoctorWithFakes(t *testing.T) {
	h, a, _ := fixtureApp(t)
	h.vars["TERM"] = "xterm-256color"
	h.vars["LANG"] = "C.UTF-8"
	code := runApp(h, a, "doctor", "--json")
	var doc doctorDoc
	if err := json.Unmarshal(h.stdout.Bytes(), &doc); err != nil {
		t.Fatalf("doctor json: %v\n%s", err, h.stdout.String())
	}
	status := map[string]string{}
	for _, c := range doc.Checks {
		status[c.Name] = c.Status
	}
	for _, name := range []string{"squeue", "slurm version", "cluster", "controller", "user", "terminal", "locale"} {
		if status[name] != checkOK {
			t.Errorf("check %s = %q (exit %d)", name, status[name], code)
		}
	}
	if code != ExitOK {
		t.Fatalf("doctor exit %d: %+v", code, doc.Checks)
	}
	if code := runApp(h, a, "doctor"); code != ExitOK || !strings.Contains(h.stdout.String(), "✓") {
		t.Fatalf("doctor text: %q", h.stdout.String())
	}
}

func TestDataCommandsReportFailures(t *testing.T) {
	h, a, f := fixtureApp(t)
	down := execx.FakeResponse{Stderr: []byte("squeue: error: Unable to contact slurm controller (connect failure)\n"), ExitCode: 1}
	f.Set([]string{"squeue", "--me", "-h", "-o", strings.Join([]string{"%i", "%F", "%K", "%P", "%q", "%a", "%T", "%M", "%l", "%L", "%D", "%C", "%m", "%b", "%r", "%N", "%S", "%e", "%V", "%Q", "%E", "%u", "%j"}, "\x1f")}, down)
	if code := runApp(h, a, "status"); code != ExitError || !strings.Contains(h.stderr.String(), "Unable to contact") {
		t.Fatalf("controller down: %d %q", code, h.stderr.String())
	}

	h2 := newHarness(t)
	a2 := &app{env: h2.env(), runner: execx.NewFake()}
	h2.vars["USER"] = "x"
	if code := run(context.Background(), newRoot(a2), a2, []string{"status"}); code != ExitError {
		t.Fatalf("no sinfo: exit %d %s", code, h2.stderr.String())
	}
}

func TestRecordWithFakes(t *testing.T) {
	h, a, _ := fixtureApp(t)
	out := filepath.Join(t.TempDir(), "rec")
	if code := runApp(h, a, "record", "--out", out, "--anonymize"); code != ExitOK {
		t.Fatalf("record exit %d: %s %s", code, h.stdout.String(), h.stderr.String())
	}
	dir := filepath.Join(out, "23.11")
	for _, name := range []string{"version.txt", "myjobs.txt", "nodes.txt", "history.txt", "jobdetail-running.txt", "jobdetail-pending.txt", "anonymize.mapping.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "clustername.txt"))
	if strings.Count(string(cfg), "\n") > 2 || !strings.Contains(string(cfg), "ClusterName") {
		t.Fatalf("config dump must be reduced to ClusterName: %q", cfg)
	}
	if !strings.Contains(h.stdout.String(), "do not commit") {
		t.Fatalf("record output: %q", h.stdout.String())
	}
	if code := runApp(h, a, "record"); code != ExitUsage {
		t.Fatalf("missing --out: exit %d", code)
	}
}

func TestRecordPreservesExistingDirectories(t *testing.T) {
	for _, kind := range []string{"empty", "files", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			h, a, _ := fixtureApp(t)
			out := t.TempDir()
			dir := filepath.Join(out, "23.11")
			target := dir
			if kind == "symlink" {
				target = t.TempDir()
				if err := os.Symlink(target, dir); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			body := "notes for " + h.home + "\n"
			if kind != "empty" {
				for _, name := range []string{"unrelated.txt", "version.txt", "unrelated.json"} {
					if err := os.WriteFile(filepath.Join(target, name), []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if code := runApp(h, a, "record", "--out", out, "--anonymize"); code != ExitError || !strings.Contains(h.stderr.String(), "fresh --out") {
				t.Fatalf("record: %d, %s", code, h.stderr.String())
			}
			entries, err := os.ReadDir(target)
			if err != nil {
				t.Fatal(err)
			}
			want := 3
			if kind == "empty" {
				want = 0
			}
			if len(entries) != want {
				t.Fatalf("existing directory changed: %v", entries)
			}
			for _, e := range entries {
				got, err := os.ReadFile(filepath.Join(target, e.Name()))
				if err != nil || string(got) != body {
					t.Fatalf("%s changed: %q, %v", e.Name(), got, err)
				}
			}
		})
	}
}

func TestAnonymizerRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "private.txt")
	body := "alice private notes"
	if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "fixture.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := newAnonymizer("alice", "/home/alice").apply(dir); err == nil {
		t.Fatal("anonymizer accepted a symlink")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != body {
		t.Fatalf("symlink target changed: %q, %v", got, err)
	}
}

func TestAnonymizer(t *testing.T) {
	an := newAnonymizer("alice", "/home/alice")
	an.cluster = "bigcluster"
	an.addUser("bob")
	an.addUser("al") // too short to replace safely
	an.addAccount("physics")
	an.addJob("train-lora")
	an.addJob("batch") // Slurm keyword
	an.addJob("bob-sweep")
	an.reserve("gpu")
	an.addJob("gpu")

	dir := t.TempDir()
	text := "812\x1ftrain-lora\x1falice\x1fphysics\x1f/home/alice/runs/train-lora-812.out\n" +
		"813.batch batch bob bob-sweep bobby alicex ClusterName = bigcluster gpu al\n"
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.meta.json"), []byte(`{"argv":["sacct","-u","alice"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mapPath, err := an.apply(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "x.txt"))
	want := "812\x1fjob1\x1fuser1\x1facct1\x1f/home/user1/runs/job1-812.out\n" +
		"813.batch batch user2 job2 bobby alicex ClusterName = cluster1 gpu al\n"
	if string(got) != want {
		t.Fatalf("anonymized:\n got %q\nwant %q", got, want)
	}
	meta, _ := os.ReadFile(filepath.Join(dir, "x.meta.json"))
	if !strings.Contains(string(meta), `"user1"`) {
		t.Fatalf("meta = %s", meta)
	}
	var m mappingFile
	b, _ := os.ReadFile(mapPath)
	if err := json.Unmarshal(b, &m); err != nil || m.Users["bob"] != "user2" || len(m.Kept) != 3 {
		t.Fatalf("mapping = %+v %v", m, err)
	}
	if fi, _ := os.Stat(mapPath); fi.Mode().Perm() != 0o600 || !strings.HasSuffix(mapPath, ".mapping.json") {
		t.Fatal("mapping file must be private and gitignored")
	}
	if replaceWord("abc", "", "x") != "abc" {
		t.Fatal("empty pattern")
	}
}
