package execx

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRunWith(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	f.Set([]string{"sbatch", "--test-only"}, FakeResponse{Stderr: []byte("sbatch: Job 1 to start at ...")})
	if _, err := RunWith(ctx, f, RunOpts{Stdin: []byte("#!/bin/bash\n"), Dir: "/tmp"}, "sbatch", "--test-only"); err != nil {
		t.Fatal(err)
	}
	if in := f.Inputs(); len(in) != 1 || string(in[0].Stdin) != "#!/bin/bash\n" || in[0].Dir != "/tmp" {
		t.Fatalf("inputs = %+v", in)
	}
	for _, o := range []RunOpts{{Dir: "relative"}, {Dir: "/a/../b"}, {Stdin: make([]byte, MaxStdin+1)}} {
		if _, err := RunWith(ctx, f, o, "sbatch", "--test-only"); !errors.Is(err, ErrInvalidArgv) {
			t.Errorf("%+v accepted: %v", o.Dir, err)
		}
	}
	// History wrappers pass the options through.
	h := WithHistory(f, 4, nil)
	if _, err := RunWith(ctx, h, RunOpts{Stdin: []byte("x")}, "sbatch", "--test-only"); err != nil || string(f.Inputs()[1].Stdin) != "x" {
		t.Fatalf("through history: %v", err)
	}
}

func TestRealRunWith(t *testing.T) {
	dir := t.TempDir()
	r := NewReal(Options{Policy: Policy{ExtraReadOnly: []string{"cat", "pwd"}}})
	ctx := context.Background()
	res, err := RunWith(ctx, r, RunOpts{Stdin: []byte("piped\n")}, "cat")
	if err != nil || string(res.Stdout) != "piped\n" {
		t.Fatalf("cat = %q %v", res.Stdout, err)
	}
	res, err = RunWith(ctx, r, RunOpts{Dir: dir}, "pwd")
	if err != nil || strings.TrimSpace(string(res.Stdout)) != dir {
		t.Fatalf("pwd = %q %v", res.Stdout, err)
	}
	if _, err := RunWith(ctx, r, RunOpts{Dir: dir + "/missing"}, "pwd"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing dir: %v", err)
	}
	// Without RunWith, stdin is empty: a command never reads the TUI's input.
	res, err = r.Run(ctx, "cat")
	if err != nil || len(res.Stdout) != 0 {
		t.Fatalf("cat without input = %q %v", res.Stdout, err)
	}
	_ = os.Remove(dir)
}
