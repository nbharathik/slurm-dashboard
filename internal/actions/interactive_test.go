package actions

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
)

var caps = model.Capabilities{Major: 23, Minor: 11, HasOverlap: true}

func running(id string, gpus int, nodes ...string) model.Job {
	return model.Job{ID: model.JobID{Raw: id}, State: model.StateRunning, GPUs: gpus, NodeList: nodes}
}

func TestShellArgv(t *testing.T) {
	j := running("812", 1, "gpu01", "gpu02")
	cases := []struct {
		jobID, node, method, shell string
		want                       string
		err                        string
	}{
		{"", "", "srun", "/bin/zsh", "srun --jobid=812 --overlap --pty /bin/zsh -l", ""},
		{"", "gpu02", "", "", "srun --jobid=812 --overlap --pty -w gpu02 bash -l", ""},
		{"4242", "", "srun", "fish", "srun --jobid=4242 --overlap --pty fish -l", ""},
		{"", "", "srun", "/bin/../evil", "srun --jobid=812 --overlap --pty bash -l", ""},
		{"", "", "srun", "zsh; rm -rf ~", "srun --jobid=812 --overlap --pty bash -l", ""},
		{"", "", "ssh", "", "ssh -t gpu01", ""},
		{"", "gpu02", "ssh", "", "ssh -t gpu02", ""},
		{"", "-oProxyCommand=touch x", "ssh", "", "", "not a valid node"},
		{"", "a b", "srun", "", "", "not a valid node"},
		{"812; ls", "", "srun", "", "", "not a valid job ID"},
		{"", "", "mosh", "", "", "unknown shell method"},
	}
	for _, tc := range cases {
		argv, err := ShellArgv(j, tc.jobID, tc.node, tc.method, tc.shell, caps)
		switch {
		case tc.err != "":
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%+v: err %v", tc, err)
			}
		case err != nil || strings.Join(argv, " ") != tc.want:
			t.Errorf("%+v: %q %v", tc, argv, err)
		}
	}
	if _, err := ShellArgv(model.Job{ID: model.JobID{Raw: "1"}, State: model.StatePending}, "", "", "srun", "", caps); err == nil {
		t.Fatal("pending job accepted")
	}
	old := model.Capabilities{Major: 20, Minor: 2}
	if _, err := ShellArgv(j, "", "", "srun", "", old); err == nil || !strings.Contains(err.Error(), "20.11") {
		t.Fatalf("old Slurm: %v", err)
	}
	if _, err := ShellArgv(running("1", 0), "", "", "ssh", "", caps); err == nil {
		t.Fatal("ssh without a node accepted")
	}
}

func TestShellAuthorisesExactArgv(t *testing.T) {
	argv := []string{"srun", "--jobid=812", "--overlap", "--pty", "bash", "-l"}
	c, err := Shell(context.Background(), execx.Policy{NoTestGuard: true}, argv)
	if err != nil || !slices.Equal(c.Argv(), argv) {
		t.Fatalf("Shell = %v", err)
	}
	if _, err := Shell(context.Background(), execx.Policy{}, argv); !errors.Is(err, execx.ErrClusterToolInTests) {
		t.Fatalf("test guard: %v", err)
	}
	if c, err := Shell(context.Background(), execx.Policy{}, []string{"ssh", "-t", "gpu01"}); err != nil || c.Argv()[0] != "ssh" {
		t.Fatalf("ssh: %v", err)
	}
}

func TestGPUSample(t *testing.T) {
	j := running("812", 2, "gpu01")
	argv, err := GPUSampleArgv(j, "", "gpu01")
	if err != nil || strings.Join(argv[:6], " ") != "srun --jobid=812 --overlap -N1 -n1 -w" {
		t.Fatalf("argv = %q %v", argv, err)
	}
	if _, err := GPUSampleArgv(running("1", 0), "", ""); err == nil {
		t.Fatal("no-GPU job accepted")
	}
	if _, err := GPUSampleArgv(j, "", "--bad"); err == nil {
		t.Fatal("bad node accepted")
	}
	f := execx.NewFake()
	f.Set(argv, execx.FakeResponse{Stdout: []byte("0, NVIDIA H100 80GB HBM3, 93, 61234, 81559\n1, NVIDIA H100 80GB HBM3, 88, 60000, 81559\n")})
	s, err := SampleGPUs(context.Background(), f, argv)
	if err != nil || len(s) != 2 || s[0].Util != 93 || s[1].MemUsedMiB != 60000 || s[0].Name != "NVIDIA H100 80GB HBM3" {
		t.Fatalf("samples = %+v %v", s, err)
	}
	// Without the grant SampleGPUs adds, the runner refuses srun.
	if _, err := f.Run(context.Background(), argv...); !errors.Is(err, execx.ErrMutationNotAuthorized) {
		t.Fatalf("unguarded srun: %v", err)
	}
	f.Set(argv, execx.FakeResponse{ExitCode: 1, Stderr: []byte("srun: error: Unable to create step\n")})
	if _, err := SampleGPUs(context.Background(), f, argv); err == nil || !strings.Contains(err.Error(), "Unable to create step") {
		t.Fatalf("error = %v", err)
	}
	for _, bad := range []string{"", "0, x, y, 1, 2\n", "1,2\n"} {
		if _, err := ParseGPUSamples([]byte(bad)); err == nil {
			t.Errorf("ParseGPUSamples(%q) accepted", bad)
		}
	}
}
