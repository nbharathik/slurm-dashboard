package actions

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// validNode matches Slurm node names. It rejects anything that could be
// read as an option (a leading "-") or contains shell or path syntax.
var validNode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// validShell matches a login shell: a bare name or a clean absolute path.
var validShell = regexp.MustCompile(`^(/[A-Za-z0-9._+-]+)*/?[A-Za-z0-9._+-]+$`)

// ValidateNode rejects values that are not plain node names.
func ValidateNode(n string) error {
	if !validNode.MatchString(n) {
		return fmt.Errorf("%q is not a valid node name", n)
	}
	return nil
}

// ShellArgv builds the command for an interactive shell inside a running
// job. jobID is the job's own ID (a running array task has one
// that differs from "812_3"); node is optional for srun and defaults to
// the job's first node; shell is the user's $SHELL.
func ShellArgv(j model.Job, jobID, node, method, shell string, caps model.Capabilities) ([]string, error) {
	if j.State != model.StateRunning {
		return nil, errors.New("a shell needs a running job")
	}
	if jobID == "" {
		jobID = j.ID.Raw
	}
	if err := ValidateJobID(jobID); err != nil {
		return nil, err
	}
	if node != "" {
		if err := ValidateNode(node); err != nil {
			return nil, err
		}
	}
	switch method {
	case "", "srun":
		if caps.Major > 0 && !caps.HasOverlap {
			return nil, errors.New("srun --overlap needs Slurm 20.11 or newer; set shell = \"ssh\" if your site allows ssh to job nodes")
		}
		if shell == "" || !validShell.MatchString(shell) || (strings.Contains(shell, "/") && filepath.Clean(shell) != shell) {
			shell = "bash"
		}
		argv := []string{"srun", "--jobid=" + jobID, "--overlap", "--pty"}
		if node != "" {
			argv = append(argv, "-w", node)
		}
		return append(argv, shell, "-l"), nil
	case "ssh":
		if node == "" && len(j.NodeList) > 0 {
			node = j.NodeList[0]
		}
		if err := ValidateNode(node); err != nil {
			return nil, err
		}
		return []string{"ssh", "-t", node}, nil
	}
	return nil, fmt.Errorf("unknown shell method %q (use srun or ssh)", method)
}

// Shell returns the shell command ready for tea.Exec. srun is authorised
// for exactly argv, the same way confirmed actions are; ssh is a user
// program and needs no grant.
func Shell(ctx context.Context, p execx.Policy, argv []string) (*execx.Interactive, error) {
	if len(argv) > 0 && argv[0] == "ssh" {
		return execx.NewInteractive("", argv...)
	}
	return execx.NewInteractiveChecked(execx.WithMutation(ctx, "shell", argv), p, "", argv...)
}

// GPUSampleArgv builds the one-shot nvidia-smi query inside a running job.
// It creates a job step, which shows up in sacct.
func GPUSampleArgv(j model.Job, jobID, node string) ([]string, error) {
	if j.State != model.StateRunning {
		return nil, errors.New("GPU sampling needs a running job")
	}
	if j.GPUs == 0 {
		return nil, errors.New("the job has no GPUs")
	}
	if jobID == "" {
		jobID = j.ID.Raw
	}
	if err := ValidateJobID(jobID); err != nil {
		return nil, err
	}
	argv := []string{"srun", "--jobid=" + jobID, "--overlap", "-N1", "-n1"}
	if node != "" {
		if err := ValidateNode(node); err != nil {
			return nil, err
		}
		argv = append(argv, "-w", node)
	}
	return append(argv, "nvidia-smi", "--query-gpu=index,name,utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits"), nil
}

// GPUSample is one GPU's state from nvidia-smi.
type GPUSample struct {
	Index       int
	Name        string
	Util        int // percent
	MemUsedMiB  int
	MemTotalMiB int
}

// SampleGPUs runs the GPU query with a grant for exactly argv and parses
// its CSV output.
func SampleGPUs(ctx context.Context, r execx.Runner, argv []string) ([]GPUSample, error) {
	ctx = execx.WithMutation(ctx, "gpu", argv)
	res, err := r.Run(execx.WithLabel(ctx, "action-gpu"), argv...)
	if err != nil {
		if msg := execx.FirstLine(res.Stderr); msg != "" {
			return nil, fmt.Errorf("srun: %s", msg)
		}
		return nil, err
	}
	return ParseGPUSamples([]byte(textsafe.Text(string(res.Stdout))))
}

// ParseGPUSamples parses "index, name, util, used, total" lines.
func ParseGPUSamples(out []byte) ([]GPUSample, error) {
	rd := csv.NewReader(strings.NewReader(string(out)))
	rd.TrimLeadingSpace = true
	rd.FieldsPerRecord = 5
	rows, err := rd.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("unexpected nvidia-smi output: %w", err)
	}
	var s []GPUSample
	for _, r := range rows {
		var g GPUSample
		var errs [4]error
		g.Index, errs[0] = strconv.Atoi(r[0])
		g.Name = r[1]
		g.Util, errs[1] = strconv.Atoi(r[2])
		g.MemUsedMiB, errs[2] = strconv.Atoi(r[3])
		g.MemTotalMiB, errs[3] = strconv.Atoi(r[4])
		if err := errors.Join(errs[:]...); err != nil {
			return nil, fmt.Errorf("unexpected nvidia-smi output %q: %w", strings.Join(r, ","), err)
		}
		s = append(s, g)
	}
	if len(s) == 0 {
		return nil, errors.New("nvidia-smi reported no GPUs")
	}
	return s, nil
}
