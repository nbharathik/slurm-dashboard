package execx

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// Interactive is a command that takes over the terminal, such as the
// user's editor or pager. Its method set matches Bubble Tea's ExecCommand
// interface, so the TUI can suspend itself, run it, and resume without
// importing os/exec.
//
// It runs with the user's unmodified environment (so their editor keeps its
// locale and settings), in the terminal's process group, and without the
// runner's timeout.
type Interactive struct {
	argv   []string
	dir    string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// NewInteractive prepares argv to run in dir ("" means the current
// directory). It is for programs the user chose, such as $EDITOR or $PAGER.
// Slurm commands are refused here: they must go through a Runner so the
// allowlist and mutation guard apply.
func NewInteractive(dir string, argv ...string) (*Interactive, error) {
	if err := validateArgv(argv); err != nil {
		return nil, err
	}
	if IsSlurmTool(argv[0]) {
		return nil, fmt.Errorf("%s: %w: Slurm commands must run through a Runner", filepath.Base(argv[0]), ErrNotAllowed)
	}
	return &Interactive{
		argv:   slices.Clone(argv),
		dir:    dir,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}, nil
}

// NewInteractiveChecked prepares a cluster command that takes over the
// terminal, such as "srun --pty" for a shell inside a job. Unlike
// NewInteractive it accepts Slurm tools, but only after the policy check a
// Runner applies: the allowlist, the test guard, and for state-changing
// commands a WithMutation grant for exactly this argv in ctx.
func NewInteractiveChecked(ctx context.Context, p Policy, dir string, argv ...string) (*Interactive, error) {
	if _, err := p.Check(ctx, argv); err != nil {
		return nil, err
	}
	return &Interactive{
		argv:   slices.Clone(argv),
		dir:    dir,
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	}, nil
}

// Argv returns the command line.
func (c *Interactive) Argv() []string { return slices.Clone(c.argv) }

// SetStdin sets the command's standard input.
func (c *Interactive) SetStdin(r io.Reader) { c.stdin = r }

// SetStdout sets the command's standard output.
func (c *Interactive) SetStdout(w io.Writer) { c.stdout = w }

// SetStderr sets the command's standard error.
func (c *Interactive) SetStderr(w io.Writer) { c.stderr = w }

// Run starts the command and waits for it to finish.
func (c *Interactive) Run() error {
	cmd := exec.Command(c.argv[0], c.argv[1:]...)
	cmd.Dir = c.dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.stdin, c.stdout, c.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(c.argv[0]), err)
	}
	return nil
}
