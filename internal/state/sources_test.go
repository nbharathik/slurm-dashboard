package state

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
)

func recordedSources(t *testing.T) (*Sources, *execx.FakeRunner) {
	t.Helper()
	f := execx.NewFake()
	if err := f.LoadDir("../../testdata/fixtures/23.11"); err != nil {
		t.Fatal(err)
	}
	caps := model.Capabilities{Version: "23.11.4", Major: 23, Minor: 11, Patch: 4, HasMe: true, HasOverlap: true}
	return &Sources{Runner: f, Cmd: slurm.Commands{Caps: caps, User: "user1"}}, f
}

func TestSourcesAgainstRecordedCluster(t *testing.T) {
	s, _ := recordedSources(t)
	ctx := context.Background()

	jobs, err := s.MyJobs(ctx)
	if err != nil || len(jobs) != 11 {
		t.Fatalf("MyJobs = %d, %v", len(jobs), err)
	}
	all, err := s.AllJobs(ctx)
	if err != nil || len(all) != 13 {
		t.Fatalf("AllJobs = %d, %v", len(all), err)
	}
	cl, err := s.Cluster(ctx, nil)
	if err != nil || len(cl.Jobs) != 6 {
		t.Fatalf("Cluster = %+v, %v", cl, err)
	}
	nodes, err := s.Nodes(ctx)
	if err != nil || len(nodes) != 5 {
		t.Fatalf("Nodes = %d, %v", len(nodes), err)
	}
	parts, err := s.Partitions(ctx)
	if err != nil || len(parts) != 3 {
		t.Fatalf("Partitions = %d, %v", len(parts), err)
	}
	res, err := s.Reservations(ctx)
	if err != nil || len(res) != 1 || !res[0].IsMaintenance() {
		t.Fatalf("Reservations = %+v, %v", res, err)
	}
	hist, err := s.History(ctx, 7)
	if err != nil || len(hist.Jobs) == 0 || hist.Days != 7 {
		t.Fatalf("History = %d, %v", len(hist.Jobs), err)
	}
	shares, err := s.Fairshare(ctx)
	if err != nil || len(shares) != 1 {
		t.Fatalf("Fairshare = %v, %v", shares, err)
	}
	pr, err := s.Priorities(ctx)
	if err != nil || len(pr) == 0 {
		t.Fatalf("Priorities = %v, %v", pr, err)
	}
	lim := &Sources{Runner: s.Runner, Cmd: slurm.Commands{Caps: s.Cmd.Caps, User: "root"}}
	scopes, err := lim.Limits(ctx)
	if err != nil || len(scopes) != 3 || scopes[0].Kind != "account" || scopes[0].Name != "research" {
		t.Fatalf("Limits = %+v, %v", scopes, err)
	}
	if len(s.WarningCounts()) != 0 {
		t.Fatalf("real fixtures produced parse warnings: %v", s.WarningCounts())
	}
}

func TestLimitsRefusesOddUserNames(t *testing.T) {
	for _, u := range []string{"", "a b", "u,v", "u\nx", strings.Repeat("u", 65)} {
		f := execx.NewFake()
		s := &Sources{Runner: f, Cmd: slurm.Commands{User: u}}
		if _, err := s.Limits(context.Background()); err == nil {
			t.Errorf("user %q: want an error", u)
		}
	}
}

func TestSourcesErrorsAndEdgeCases(t *testing.T) {
	f := execx.NewFake()
	s := &Sources{Runner: f, Cmd: slurm.Commands{Caps: model.Capabilities{Major: 23, Minor: 11, HasMe: true}, User: "u"}}
	ctx := context.Background()

	f.Set(s.Cmd.MyJobs(), execx.FakeResponse{Stderr: []byte("squeue: error: Unable to contact slurm controller (connect failure)\n"), ExitCode: 1})
	_, err := s.MyJobs(ctx)
	if KindOf(err) != ErrControllerDown || !strings.Contains(err.Error(), "Unable to contact") {
		t.Fatalf("controller down: %v (kind %v)", err, KindOf(err))
	}
	f.Set(s.Cmd.History(7), execx.FakeResponse{Stderr: []byte("sacct: error: Problem talking to the database: Connection refused"), ExitCode: 1})
	if _, err := s.History(ctx, 7); KindOf(err) != ErrAccountingDown {
		t.Fatalf("accounting down: %v", err)
	}
	if _, err := s.JobDetail(ctx, "99"); KindOf(err) != ErrControllerDown {
		t.Fatal("ownership lookup errors must fail closed")
	}
	f.Set(s.Cmd.MyJobs(), execx.FakeResponse{Stdout: ownQueue("u", "99", "98", "97", "1", "2", "3")})
	f.Set(s.Cmd.JobDetail("99"), execx.FakeResponse{Stderr: []byte("slurm_load_jobs error: Invalid job id specified\n"), ExitCode: 1})
	if d, err := s.JobDetail(ctx, "99"); d != nil || err != nil {
		t.Fatalf("vanished job = %v, %v", d, err)
	}
	f.Set(s.Cmd.JobDetail("98"), execx.FakeResponse{})
	if d, err := s.JobDetail(ctx, "98"); d != nil || err != nil {
		t.Fatalf("empty detail = %v, %v", d, err)
	}
	f.Set(s.Cmd.Sstat("98"), execx.FakeResponse{})
	if st, err := s.JobStat(ctx, "98"); st != nil || err != nil {
		t.Fatalf("no steps = %v, %v", st, err)
	}
	f.Set(s.Cmd.Sstat("97"), execx.FakeResponse{Stdout: []byte("97.batch|1|10M|5M|00:00:05|\n")})
	if st, err := s.JobStat(ctx, "97"); err != nil || st.MaxRSSMB != 10 || st.At.IsZero() {
		t.Fatalf("jobstat = %+v, %v", st, err)
	}
	if q, err := s.QueueRank(ctx, nil); q != nil || err != nil {
		t.Fatal("no partitions means no query")
	}
	if fs, err := s.FinalStates(ctx, nil); fs != nil || err != nil {
		t.Fatal("no ids means no query")
	}
	f.Set(s.Cmd.FinalStates([]string{"5"}), execx.FakeResponse{Stdout: []byte("5|TIMEOUT|0:0|60\n")})
	if fs, err := s.FinalStates(ctx, []string{"5"}); err != nil || fs[0].State != model.StateTimeout {
		t.Fatalf("final = %v, %v", fs, err)
	}
	f.Set(s.Cmd.HistoryJob("5"), execx.FakeResponse{})
	if h, err := s.HistoryJob(ctx, "5"); h != nil || err != nil {
		t.Fatal("empty history job")
	}

	// Batch script: controller first, then accounting, else a clear error.
	f.Set(s.Cmd.BatchScript("1"), execx.FakeResponse{Stdout: []byte("#!/bin/sh\necho hi\n")})
	if sc, err := s.BatchScript(ctx, "1"); err != nil || !strings.Contains(sc, "echo hi") {
		t.Fatalf("script = %q %v", sc, err)
	}
	f.Set(s.Cmd.BatchScript("2"), execx.FakeResponse{Stderr: []byte("error"), ExitCode: 1})
	f.Set(s.Cmd.SacctBatchScript("2"), execx.FakeResponse{Stdout: []byte("Batch Script for 2\n----------\n#!/bin/sh\nrun\n")})
	if sc, err := s.BatchScript(ctx, "2"); err != nil || strings.Contains(sc, "Batch Script") || !strings.Contains(sc, "run") {
		t.Fatalf("sacct script = %q %v", sc, err)
	}
	if _, err := s.BatchScript(ctx, "3"); err == nil || !strings.Contains(err.Error(), "no longer available") {
		t.Fatalf("missing script: %v", err)
	}

	f.Set(s.Cmd.Nodes(), execx.FakeResponse{Stdout: []byte("garbage line\n")})
	if _, err := s.Nodes(ctx); err != nil {
		t.Fatal(err)
	}
	if s.WarningCounts()["nodes"] != 1 {
		t.Fatalf("warnings = %v", s.WarningCounts())
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err    error
		stderr string
		kind   ErrKind
	}{
		{errors.New("x: " + execx.ErrNotFound.Error()), "", ErrOther},
		{&execx.ExitError{Code: 1}, "slurm_load_partitions: Socket timed out on send/recv operation", ErrControllerDown},
		{&execx.ExitError{Code: 1}, "sacct: error: Slurm accounting storage is disabled", ErrAccountingDown},
		{&execx.ExitError{Code: 1}, "something else", ErrOther},
	}
	for _, tc := range cases {
		if k := KindOf(Classify(tc.err, []byte(tc.stderr))); k != tc.kind {
			t.Errorf("Classify(%v, %q) kind = %v, want %v", tc.err, tc.stderr, k, tc.kind)
		}
	}
	if Classify(nil, nil) != nil {
		t.Fatal("nil error")
	}
	wrapped := Classify(errors.Join(execx.ErrNotFound), nil)
	if KindOf(wrapped) != ErrMissing {
		t.Fatalf("missing command kind = %v", KindOf(wrapped))
	}
	if KindOf(errors.New("plain")) != ErrOther {
		t.Fatal("plain error")
	}
	var se *SlurmError
	if !errors.As(wrapped, &se) || se.Unwrap() == nil {
		t.Fatal("unwrap")
	}
}

func TestMyStatsBatchesAndFilters(t *testing.T) {
	f := execx.NewFake()
	var calls [][]string
	f.Handler = func(_ context.Context, argv []string) (execx.Result, error) {
		calls = append(calls, argv)
		var b strings.Builder
		for _, id := range strings.Split(argv[5], ",") { // sstat -a -n -P -j IDS
			b.WriteString(id + ".batch|1|1000K|900K|00:01:00|cpu=00:01:00,mem=1000K\n")
		}
		b.WriteString("999999.batch|1|1000K|900K|00:01:00|cpu=00:01:00,mem=1000K\n")
		return execx.Result{Stdout: []byte(b.String())}, nil
	}
	s := &Sources{Runner: f, Cmd: slurm.Commands{Caps: model.Capabilities{Major: 23, Minor: 11}, User: "u"}}

	var ids []string
	for i := 1; i <= 45; i++ {
		ids = append(ids, strconv.Itoa(1000+i))
	}
	s.ownAt = time.Now()
	s.ownJobs = map[string]model.Job{}
	for _, id := range ids {
		s.ownJobs[id] = model.Job{ID: model.JobID{Raw: id}, User: "u"}
	}
	ids = append(ids, "999999", "815_3", "12+1", "abc", "", "1; rm -rf /")
	got, err := s.MyStats(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 45 || got["1001"].NTasks != 1 || got["1045"].TotalCPU != time.Minute || got["1001"].At.IsZero() {
		t.Errorf("stats = %d entries, first %+v", len(got), got["1001"])
	}
	if len(calls) != 3 {
		t.Fatalf("%d sstat calls for 45 jobs, want 3 (20 + 20 + 5)", len(calls))
	}
	for i, want := range []int{20, 20, 5} {
		if n := len(strings.Split(calls[i][5], ",")); n != want {
			t.Errorf("call %d sampled %d jobs, want %d", i, n, want)
		}
	}
	calls = nil
	if got, err := s.MyStats(context.Background(), []string{"815_3"}); err != nil || len(got) != 0 || len(calls) != 0 {
		t.Errorf("array tasks only: %v, %v, %d calls", got, err, len(calls))
	}
}
