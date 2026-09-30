// Package execx is the only subprocess gateway: argv only, allowlisted, mutation-guarded, time-limited and output-capped.
package execx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// Result is the outcome of one command.
type Result struct {
	Argv     []string
	Stdout   []byte
	Stderr   []byte
	ExitCode int // -1 when the process did not start or was killed by a signal
	Duration time.Duration
}

// Runner runs one command by argv; a non-zero exit returns the Result plus *ExitError.
type Runner interface {
	Run(ctx context.Context, argv ...string) (Result, error)
}

// Sentinel errors; test with errors.Is.
var (
	// ErrNotFound means the executable is not in PATH.
	ErrNotFound = errors.New("command not found")
	// ErrTimeout means the deadline passed and the process group was killed.
	ErrTimeout = errors.New("command timed out")
	// ErrOutputTooLarge means stdout exceeded the cap.
	ErrOutputTooLarge = errors.New("command output too large")
	// ErrInvalidArgv means argv was empty or had forbidden bytes.
	ErrInvalidArgv = errors.New("invalid command line")
	// ErrNotAllowed means the executable is not allowlisted.
	ErrNotAllowed = errors.New("command not allowed")
	// ErrMutationNotAuthorized means a state-changing argv lacked WithMutation.
	ErrMutationNotAuthorized = errors.New("state-changing command not authorised")
	// ErrClusterToolInTests means a test ran a real cluster tool without opting in.
	ErrClusterToolInTests = errors.New("real cluster tools are disabled in tests; use FakeRunner")
	// ErrNoFixture means a FakeRunner has no response for the argv.
	ErrNoFixture = errors.New("no fixture for command")
)

// ExitError reports a command that ran but exited with a non-zero status.
type ExitError struct {
	Name   string // executable basename
	Code   int
	Stderr string // first non-empty line of stderr, for the flash line
}

func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s: exit status %d", e.Name, e.Code)
	}
	return fmt.Sprintf("%s: exit status %d: %s", e.Name, e.Code, e.Stderr)
}

// FirstLine returns the first non-empty, trimmed line of b.
func FirstLine(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(textsafe.Field(line)); s != "" {
			return s
		}
	}
	return ""
}

// Key returns a readable string for argv (fixtures, logs). Not injective; never use for authorisation.
func Key(argv []string) string {
	return strings.ReplaceAll(strings.Join(argv, " "), "\x1f", "␟")
}

type labelKey struct{}

// WithLabel attaches a short source name (e.g. "myjobs") to ctx for history, logs and fixtures.
func WithLabel(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, labelKey{}, label)
}

// Label returns the label attached with WithLabel, or "".
func Label(ctx context.Context) string {
	s, _ := ctx.Value(labelKey{}).(string)
	return s
}

// LookPath reports where an executable is in PATH; it only inspects the filesystem.
func LookPath(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, ErrNotFound)
	}
	return p, nil
}

// MaxStdin bounds the data a command may receive on stdin.
const MaxStdin = 1 << 20

// RunOpts are per-call options: stdin data and a working directory.
type RunOpts struct {
	Stdin []byte
	Dir   string // absolute; "" means the current directory
}

type inputKey struct{}

// RunWith runs argv with stdin data and a working directory; options travel in ctx so wrappers pass them on.
func RunWith(ctx context.Context, r Runner, opts RunOpts, argv ...string) (Result, error) {
	if err := opts.validate(); err != nil {
		return Result{Argv: argv, ExitCode: -1}, err
	}
	return r.Run(context.WithValue(ctx, inputKey{}, opts), argv...)
}

// InputFrom returns the options RunWith attached to ctx.
func InputFrom(ctx context.Context) (RunOpts, bool) {
	o, ok := ctx.Value(inputKey{}).(RunOpts)
	return o, ok
}

func (o RunOpts) validate() error {
	if len(o.Stdin) > MaxStdin {
		return fmt.Errorf("%w: stdin larger than %d bytes", ErrInvalidArgv, MaxStdin)
	}
	if o.Dir != "" && (!filepath.IsAbs(o.Dir) || filepath.Clean(o.Dir) != o.Dir) {
		return fmt.Errorf("%w: working directory %q must be a clean absolute path", ErrInvalidArgv, o.Dir)
	}
	return nil
}

var errNoDir = errors.New("working directory does not exist")
