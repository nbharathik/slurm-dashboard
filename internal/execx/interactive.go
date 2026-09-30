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

// Interactive is a terminal-owning command (editor, pager) matching Bubble Tea's ExecCommand.
// It runs with the user's unmodified environment and no timeout.
type Interactive struct {
	argv   []string
	dir    string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// NewInteractive prepares a user-chosen program ($EDITOR, $PAGER); Slurm tools are refused, they need a Runner.
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

// NewInteractiveChecked prepares a Slurm command that owns the terminal (e.g. srun --pty) after the Runner's policy check, including any WithMutation grant.
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
