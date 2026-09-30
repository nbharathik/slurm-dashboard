package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

// Exit codes.
const (
	ExitOK           = 0
	ExitError        = 1
	ExitUsage        = 2
	ExitSlurmMissing = 3
)

// usageError marks a mistake in how sdash was invoked (exit 2).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// errSlurmMissing marks a failure caused by Slurm commands not being
// installed or not in PATH (exit 3).
var errSlurmMissing = errors.New("no Slurm commands found in PATH")

// errSilent reports failure (exit 1) when the command already printed
// everything the user needs.
var errSilent = errors.New("")

// usageArgs wraps a cobra argument validator so its errors exit with 2.
func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// exitWith prints err (if it has anything to say) and returns the exit code.
func exitWith(err error, stderr io.Writer) int {
	if err == nil {
		return ExitOK
	}
	var ue usageError
	switch {
	case errors.As(err, &ue):
		_, _ = fmt.Fprintf(stderr, "%s: %v\nRun '%s --help' for usage.\n", meta.AppName, err, meta.AppName)
		return ExitUsage
	case errors.Is(err, errSilent):
		return ExitError
	case errors.Is(err, errSlurmMissing):
		_, _ = fmt.Fprintf(stderr, "%s: %v\nRun '%s doctor' to see which commands are missing.\n", meta.AppName, err, meta.AppName)
		return ExitSlurmMissing
	default:
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", meta.AppName, err)
		return ExitError
	}
}
