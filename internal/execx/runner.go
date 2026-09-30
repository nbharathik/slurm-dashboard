package execx

import (
	"context"
	"errors"
	"fmt"
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

// Runner runs one command described by argv and captures its output.
//
// Run returns a nil error only when the command ran and exited with status
// 0. A non-zero exit returns the populated Result together with an
// *ExitError.
type Runner interface {
	Run(ctx context.Context, argv ...string) (Result, error)
}

// Sentinel errors. Use errors.Is to test for them.
var (
	// ErrNotFound means the executable is not installed or not in PATH.
	ErrNotFound = errors.New("command not found")
	// ErrTimeout means the command exceeded its deadline and its process
	// group was killed. errors.Is(err, context.DeadlineExceeded) also holds.
	ErrTimeout = errors.New("command timed out")
	// ErrOutputTooLarge means stdout exceeded the runner's cap and the
	// command was killed.
	ErrOutputTooLarge = errors.New("command output too large")
	// ErrInvalidArgv means argv was empty or contained forbidden bytes.
	ErrInvalidArgv = errors.New("invalid command line")
	// ErrNotAllowed means the executable is not on the allowlist.
	ErrNotAllowed = errors.New("command not allowed")
	// ErrMutationNotAuthorized means a command that changes cluster state
	// was attempted without an authorisation from WithMutation for that
	// exact argv.
	ErrMutationNotAuthorized = errors.New("state-changing command not authorised")
	// ErrClusterToolInTests means a test binary tried to run a real cluster
	// tool through a runner that did not opt in.
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

// FirstLine returns the first non-empty, trimmed line of b. It is used for
// short error messages from stderr.
func FirstLine(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(textsafe.Field(line)); s != "" {
			return s
		}
	}
	return ""
}

// Key returns a readable, stable string for argv, used for fixture lookup
// and logs. The ASCII unit separator is shown as "␟". Key is not injective
// for arguments containing spaces; never use it for authorisation.
func Key(argv []string) string {
	return strings.ReplaceAll(strings.Join(argv, " "), "\x1f", "␟")
}
