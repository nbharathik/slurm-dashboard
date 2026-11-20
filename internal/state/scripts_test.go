package state

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
)

// A script fetched once is still there for a rerun after Slurm forgot it.
func TestBatchScriptKeptCopy(t *testing.T) {
	f := execx.NewFake()
	raw, err := os.ReadFile("../../testdata/fixtures/23.11/myjobs.txt")
	if err != nil {
		t.Fatal(err)
	}
	src := &Sources{Runner: f, Cmd: slurm.Commands{User: "user1"}, ScriptDir: filepath.Join(t.TempDir(), "scripts", "c")}
	f.Set(src.Cmd.MyJobs(), execx.FakeResponse{Stdout: raw})
	f.Set(src.Cmd.BatchScript("14"), execx.FakeResponse{Stdout: []byte("#!/bin/bash\necho hi\n")})
	ctx := context.Background()
	if got, err := src.BatchScript(ctx, "14"); err != nil || got != "#!/bin/bash\necho hi\n" {
		t.Fatalf("first fetch: %q %v", got, err)
	}
	st, err := os.Stat(filepath.Join(src.ScriptDir, "14.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("copy mode %v", st.Mode().Perm())
	}
	gone := execx.FakeResponse{Stderr: []byte("scontrol: error: Invalid job id specified\n"), ExitCode: 1}
	f.Set(src.Cmd.BatchScript("14"), gone)
	f.Set(src.Cmd.SacctBatchScript("14"), gone)
	if got, err := src.BatchScript(ctx, "14"); err != nil || got != "#!/bin/bash\necho hi\n" {
		t.Fatalf("after Slurm forgot it: %q %v", got, err)
	}
	if p := src.scriptPath("../14"); p != "" {
		t.Fatalf("unsafe id gave a path %q", p)
	}
}

// The cap drops the oldest copies, never the one just saved, whatever the
// names sort as.
func TestKeptScriptsCapKeepsNewest(t *testing.T) {
	src := &Sources{ScriptDir: filepath.Join(t.TempDir(), "scripts")}
	old := time.Now().Add(-time.Hour)
	for i := range maxScripts {
		id := strconv.Itoa(900000 + i)
		src.keepScript(id, "x")
		_ = os.Chtimes(filepath.Join(src.ScriptDir, id+".sh"), old, old.Add(time.Duration(i)*time.Second))
	}
	src.keepScript("1000000", "new")
	if _, ok := src.keptScript("1000000"); !ok {
		t.Fatal("the newest script was dropped")
	}
	if _, ok := src.keptScript("900000"); ok {
		t.Error("the oldest script was kept past the cap")
	}
	if _, ok := src.keptScript("900001"); !ok {
		t.Error("a newer script was dropped")
	}
}
