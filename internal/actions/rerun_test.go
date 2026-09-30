package actions

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

const script = "#!/bin/bash\n#SBATCH -t 5\n#SBATCH --mem=64G\nsleep 1\n"

func TestRerunArgv(t *testing.T) {
	argv, err := RerunArgv([]Opt{{"partition", "gpu"}, {"mem", "24G"}, {"time", "1-02:00:00"}, {"gres", "gpu:h200:1"}, {"job-name", "my job"}})
	want := []string{"sbatch", "--parsable", "--partition=gpu", "--mem=24G", "--time=1-02:00:00", "--gres=gpu:h200:1", "--job-name=my job"}
	if err != nil || !reflect.DeepEqual(argv, want) {
		t.Fatalf("RerunArgv = %q %v", argv, err)
	}
	for _, bad := range []Opt{
		{"wrap", "rm -rf ~"},              // not on the list
		{"mem", "1G --wrap=x"},            // a smuggled option
		{"partition", "-gpu"},             // a leading dash
		{"job-name", "-J"},                // a leading dash
		{"job-name", "a\nb"},              // a newline
		{"output", "x\x1b]52;c;aGk=\x07"}, // an escape sequence
		{"time", "soon"},
		{"cpus-per-task", "0"},
		{"gres", "gpu:1 --uid=0"},
	} {
		if _, err := RerunArgv([]Opt{bad}); err == nil {
			t.Errorf("RerunArgv accepted %q", bad)
		}
	}
}

func TestRerun(t *testing.T) {
	argv, _ := RerunArgv([]Opt{{"mem", "24G"}})
	f := execx.NewFake()
	f.Set(argv, execx.FakeResponse{Stdout: []byte("4242;cluster1\n")})
	s, err := Rerun(context.Background(), f, argv, script, "/home/alice/w")
	if err != nil || s.JobID != "4242" || s.Cluster != "cluster1" {
		t.Fatalf("Rerun = %+v %v", s, err)
	}
	if in := f.Inputs(); string(in[0].Stdin) != script || in[0].Dir != "/home/alice/w" {
		t.Fatalf("inputs = %+v", in)
	}
	// Without the grant Rerun adds, the runner refuses sbatch.
	if _, err := f.Run(context.Background(), argv...); !errors.Is(err, execx.ErrMutationNotAuthorized) {
		t.Fatalf("unguarded sbatch: %v", err)
	}
	for _, bad := range []struct {
		argv        []string
		script, dir string
	}{
		{argv, "", "/w"},
		{argv, "sleep 1", "/w"},
		{argv, script, ""},
		{argv, script, "relative"},
		{[]string{"sbatch", "--parsable", "--wrap=id"}, script, "/w"},
		{[]string{"sbatch", "--parsable", "run.sh"}, script, "/w"},
		{[]string{"sbatch", "--mem=1G"}, script, "/w"},
	} {
		if _, err := Rerun(context.Background(), f, bad.argv, bad.script, bad.dir); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	f.Set(argv, execx.FakeResponse{ExitCode: 1, Stderr: []byte("sbatch: error: Invalid account\n")})
	if _, err := Rerun(context.Background(), f, argv, script, "/w"); err == nil || !strings.Contains(err.Error(), "Invalid account") {
		t.Fatalf("error = %v", err)
	}
}

func TestEstimate(t *testing.T) {
	f := execx.NewFake()
	argv := []string{"sbatch", "--test-only", "--partition=gpu"}
	f.Set(argv, execx.FakeResponse{Stderr: []byte("sbatch: Job 23 to start at 2026-09-28T13:38:41 using 1 processors on nodes node01 in partition gpu\n")})
	est, err := Estimate(context.Background(), f, script, "/w", []Opt{{"partition", "gpu"}})
	if err != nil || est.Nodes != "node01" || est.Partition != "gpu" {
		t.Fatalf("Estimate = %+v %v", est, err)
	}
	f.Set(argv, execx.FakeResponse{ExitCode: 1, Stderr: []byte("sbatch: error: Batch job submission failed: Requested time limit is invalid\n")})
	if _, err := Estimate(context.Background(), f, script, "/w", []Opt{{"partition", "gpu"}}); err == nil || !strings.Contains(err.Error(), "time limit is invalid") {
		t.Fatalf("error = %v", err)
	}
	if _, err := Estimate(context.Background(), f, script, "/w", []Opt{{"uid", "0"}}); err == nil {
		t.Fatal("an option off the list was accepted")
	}
}

func TestPlanRerun(t *testing.T) {
	line, _ := parse.SubmitLine("sbatch -p gpu -A proj --dependency afterok:1 --hold run.sh x", "")
	dirs := parse.ScriptDirectives(script + "#SBATCH -c 16\n")
	p := PlanRerun(&line, dirs, map[string]string{"mem": "24G", "time": "00:15:00"})
	got := map[string]RerunField{}
	for _, f := range p.Fields {
		got[f.Name] = f
	}
	if f := got["partition"]; f.Original != "gpu" || !f.FromLine {
		t.Errorf("partition = %+v", f)
	}
	if f := got["mem"]; f.Original != "64G" || f.FromLine || f.Suggest != "24G" {
		t.Errorf("mem = %+v", f)
	}
	if f := got["cpus-per-task"]; f.Original != "" { // after the first command: not a directive
		t.Errorf("cpus = %+v", f)
	}
	if !reflect.DeepEqual(p.Carried, []Opt{{"account", "proj"}}) {
		t.Errorf("carried = %v", p.Carried)
	}
	if !reflect.DeepEqual(p.Dropped, []string{"--dependency=afterok:1", "--hold"}) {
		t.Errorf("dropped = %v", p.Dropped)
	}
	if !reflect.DeepEqual(p.ScriptArgs, []string{"x"}) {
		t.Errorf("script args = %v", p.ScriptArgs)
	}

	// Unchanged values from the script stay in the script; values from the
	// command line and changes are passed.
	opts, err := p.Opts(map[string]string{"mem": "24G"})
	if err != nil || !reflect.DeepEqual(opts, []Opt{{"account", "proj"}, {"partition", "gpu"}, {"mem", "24G"}}) {
		t.Fatalf("Opts = %v %v", opts, err)
	}
	if _, err := p.Opts(map[string]string{"mem": ""}); err == nil {
		t.Error("clearing a value the script sets was accepted")
	}
	opts, err = p.Opts(map[string]string{"partition": ""}) // back to the script or default
	if err != nil || !reflect.DeepEqual(opts, []Opt{{"account", "proj"}}) {
		t.Errorf("Opts without partition = %v %v", opts, err)
	}
	if _, err := p.Opts(map[string]string{"time": "soon"}); err == nil {
		t.Error("a bad time was accepted")
	}

	// Without a recorded line only the script counts.
	p = PlanRerun(nil, dirs, nil)
	if len(p.Carried) != 0 || len(p.Dropped) != 0 {
		t.Errorf("plan without a line = %+v", p)
	}
	// A multi-task job gets no per-task CPU suggestion.
	dirs = parse.ScriptDirectives("#!/bin/sh\n#SBATCH -n 4\n")
	for _, f := range PlanRerun(nil, dirs, map[string]string{"cpus-per-task": "2"}).Fields {
		if f.Name == "cpus-per-task" && f.Suggest != "" {
			t.Errorf("cpus suggestion kept for 4 tasks: %+v", f)
		}
	}
}
