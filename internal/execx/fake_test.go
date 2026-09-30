package execx

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestKey(t *testing.T) {
	got := Key([]string{"squeue", "--me", "-o", "%i\x1f%j"})
	if got != "squeue --me -o %i␟%j" {
		t.Fatalf("Key = %q", got)
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine([]byte("\n  \n  squeue: error: Invalid user  \nmore\n")); got != "squeue: error: Invalid user" {
		t.Fatalf("FirstLine = %q", got)
	}
	if FirstLine(nil) != "" {
		t.Fatal("FirstLine(nil)")
	}
}

func TestFakeRunner(t *testing.T) {
	f := NewFake()
	f.Set([]string{"squeue", "--me"}, FakeResponse{Stdout: []byte("812\n")})
	f.Set([]string{"sinfo", "--version"}, FakeResponse{Stderr: []byte("sinfo: error: boom\n"), ExitCode: 1})

	res, err := f.Run(context.Background(), "squeue", "--me")
	if err != nil || string(res.Stdout) != "812\n" {
		t.Fatalf("Run = %+v, %v", res, err)
	}

	_, err = f.Run(context.Background(), "sinfo", "--version")
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 1 || ee.Stderr != "sinfo: error: boom" {
		t.Fatalf("want ExitError, got %v", err)
	}

	_, err = f.Run(context.Background(), "sacct", "-X")
	if !errors.Is(err, ErrNoFixture) {
		t.Fatalf("want ErrNoFixture, got %v", err)
	}

	want := [][]string{{"squeue", "--me"}, {"sinfo", "--version"}, {"sacct", "-X"}}
	if got := f.Calls(); !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("Calls = %q", got)
	}
}

func TestFakeRunnerAppliesPolicy(t *testing.T) {
	f := NewFake()
	f.Set([]string{"scancel", "812"}, FakeResponse{})
	if _, err := f.Run(context.Background(), "scancel", "812"); !errors.Is(err, ErrMutationNotAuthorized) {
		t.Fatalf("unauthorised scancel: %v", err)
	}
	ctx := WithMutation(context.Background(), "cancel", []string{"scancel", "812"})
	if _, err := f.Run(ctx, "scancel", "812"); err != nil {
		t.Fatalf("authorised scancel: %v", err)
	}
	if _, err := f.Run(context.Background(), "rm", "-rf", "/"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("rm: %v", err)
	}
}

func TestFakeRunnerHandlerAndDelay(t *testing.T) {
	f := NewFake("myquota")
	f.Handler = func(context.Context, []string) (Result, error) {
		return Result{Stdout: []byte("handled")}, nil
	}
	res, err := f.Run(context.Background(), "myquota")
	if err != nil || string(res.Stdout) != "handled" || res.Argv[0] != "myquota" {
		t.Fatalf("Handler: %+v, %v", res, err)
	}

	f.Set([]string{"squeue", "--me"}, FakeResponse{Delay: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := f.Run(ctx, "squeue", "--me"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Delay should honour ctx, got %v", err)
	}
}

func TestRecordingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	inner := NewFake()
	inner.Set([]string{"squeue", "--me", "-o", "%i\x1f%j"}, FakeResponse{Stdout: []byte("812\x1ftrain\n")})
	inner.Set([]string{"sinfo", "--version"}, FakeResponse{Stdout: []byte("slurm 23.11.4\n")})
	inner.Set([]string{"scontrol", "show", "job", "-o", "1"}, FakeResponse{Stderr: []byte("slurm_load_jobs error: Invalid job id specified\n"), ExitCode: 1})

	rec := &RecordingRunner{Inner: inner, Dir: dir}
	ctx := WithLabel(context.Background(), "myjobs")
	if _, err := rec.Run(ctx, "squeue", "--me", "-o", "%i\x1f%j"); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Run(context.Background(), "sinfo", "--version"); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Run(context.Background(), "sinfo", "--version"); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Run(context.Background(), "scontrol", "show", "job", "-o", "1"); err == nil {
		t.Fatal("exit status 1 should surface")
	}

	for _, name := range []string{"myjobs", "sinfo", "sinfo-2", "scontrol"} {
		for _, ext := range []string{".txt", ".meta.json"} {
			fi, err := os.Stat(filepath.Join(dir, name+ext))
			if err != nil {
				t.Fatalf("missing %s%s: %v", name, ext, err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Fatalf("%s%s mode = %v, want 0600", name, ext, fi.Mode().Perm())
			}
		}
	}
	var meta recordMeta
	raw, _ := os.ReadFile(filepath.Join(dir, "myjobs.meta.json"))
	if err := json.Unmarshal(raw, &meta); err != nil || meta.Label != "myjobs" || meta.Argv[3] != "%i\x1f%j" {
		t.Fatalf("meta = %+v, %v", meta, err)
	}

	replay := NewFake()
	if err := replay.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	res, err := replay.Run(context.Background(), "squeue", "--me", "-o", "%i\x1f%j")
	if err != nil || string(res.Stdout) != "812\x1ftrain\n" {
		t.Fatalf("replay = %+v, %v", res, err)
	}
	_, err = replay.Run(context.Background(), "scontrol", "show", "job", "-o", "1")
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("replayed exit error = %v", err)
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"myjobs":           "myjobs",
		"../../etc/passwd": "_.._etc_passwd",
		"a b/c":            "a_b_c",
		"":                 "cmd",
		"...":              "cmd",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewInteractive(t *testing.T) {
	if _, err := NewInteractive("", "srun", "--pty", "bash"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("srun via Interactive: %v", err)
	}
	if _, err := NewInteractive(""); !errors.Is(err, ErrInvalidArgv) {
		t.Fatalf("empty: %v", err)
	}
	c, err := NewInteractive(t.TempDir(), "sh", "-c", "exit 0")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Equal(c.Argv(), []string{"sh", "-c", "exit 0"}) {
		t.Fatalf("Argv = %q", c.Argv())
	}
}
