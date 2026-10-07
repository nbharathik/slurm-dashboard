package state

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
)

func ownQueue(user string, ids ...string) []byte {
	var rows []string
	for _, id := range ids {
		rows = append(rows, strings.Join([]string{id, id, "N/A", "cpu", "", "", "RUNNING", "00:01", "01:00", "00:59", "1", "1", "1M", "N/A", "None", "n1", "Unknown", "Unknown", "Unknown", "1", "", user, "test"}, "\x1f"))
	}
	return []byte(strings.Join(rows, "\n"))
}

func TestForeignPrivateSourcesFailClosed(t *testing.T) {
	f := execx.NewFake()
	s := &Sources{Runner: f, Cmd: slurm.Commands{User: "me"}}
	f.Set(s.Cmd.MyJobs(), execx.FakeResponse{Stdout: ownQueue("me", "1")})
	f.Set(s.Cmd.HistoryJob("2"), execx.FakeResponse{})
	ctx := context.Background()
	checks := map[string]func() error{
		"details":    func() error { _, err := s.JobDetail(ctx, "2"); return err },
		"statistics": func() error { _, err := s.JobStat(ctx, "2"); return err },
		"script":     func() error { _, err := s.BatchScript(ctx, "2"); return err },
		"submitline": func() error { _, _, err := s.SubmitLine(ctx, "2"); return err },
	}
	for name, check := range checks {
		if err := check(); !errors.Is(err, ErrNotOwner) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, argv := range f.Calls() {
		if argv[0] == "scontrol" || argv[0] == "sstat" {
			t.Fatalf("private command launched: %q", argv)
		}
		if argv[0] == "sacct" && !strings.Contains(strings.Join(argv, " "), "-u me") {
			t.Fatal("unscoped accounting query")
		}
	}
}

func TestScheduledPrivateLookupUsesNativeSnapshot(t *testing.T) {
	f := execx.NewFake()
	s := &Sources{Runner: f, Cmd: slurm.Commands{User: "me"}}
	ctx := SnapshotPrivate(context.Background())
	if _, err := s.OwnJob(ctx, "1"); !errors.Is(err, ErrNotOwner) {
		t.Fatal(err)
	}
	if len(f.Calls()) != 0 {
		t.Fatal("scheduled lookup issued extra controller query")
	}
	s.ownJobs = map[string]model.Job{"1": {ID: model.JobID{Raw: "1"}, User: "me"}}
	s.ownAt = time.Now()
	if _, err := s.OwnJob(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	s.ownAt = time.Now().Add(-time.Minute)
	if _, err := s.OwnJob(ctx, "1"); !errors.Is(err, ErrNotOwner) {
		t.Fatal("expired witness authorised access")
	}
	if len(f.Calls()) != 0 {
		t.Fatal("scheduled lookup issued extra controller query")
	}
}

func TestMissingOwnerAndFailedRefreshRevokePrivateWitness(t *testing.T) {
	f := execx.NewFake()
	s := &Sources{Runner: f, Cmd: slurm.Commands{User: "me"}}
	f.Set(s.Cmd.MyJobs(), execx.FakeResponse{Stdout: ownQueue("", "1")})
	if _, err := s.OwnJob(context.Background(), "1"); !errors.Is(err, ErrNotOwner) {
		t.Fatal("missing owner authorised private access")
	}
	f.Set(s.Cmd.MyJobs(), execx.FakeResponse{Stdout: ownQueue("me", "1")})
	if _, err := s.MyJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Set(s.Cmd.MyJobs(), execx.FakeResponse{Err: errors.New("access denied")})
	_, _ = s.MyJobs(context.Background())
	if _, err := s.OwnJob(SnapshotPrivate(context.Background()), "1"); !errors.Is(err, ErrNotOwner) {
		t.Fatal("failed ownership refresh retained private authority")
	}
}

func TestRestrictedScriptDoesNotTryAccounting(t *testing.T) {
	f := execx.NewFake()
	s := &Sources{Runner: f, Cmd: slurm.Commands{User: "me"}}
	f.Set(s.Cmd.MyJobs(), execx.FakeResponse{Stdout: ownQueue("me", "1")})
	f.Set(s.Cmd.BatchScript("1"), execx.FakeResponse{Err: errors.New("access denied")})
	if _, err := s.BatchScript(context.Background(), "1"); err == nil {
		t.Fatal("restricted script succeeded")
	}
	for _, argv := range f.Calls() {
		if argv[0] == "sacct" {
			t.Fatal("restricted controller data triggered alternate accounting lookup")
		}
	}
}
