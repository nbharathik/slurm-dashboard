package execx

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// MaxStdin bounds the data a command may receive on stdin (a batch
// script).
const MaxStdin = 1 << 20

// RunOpts are per-call options: data for stdin and a working directory.
type RunOpts struct {
	Stdin []byte
	Dir   string // absolute; "" means the current directory
}

type inputKey struct{}

// RunWith runs argv with stdin data and a working directory, e.g. a batch
// script piped to "sbatch --parsable" from the job's directory. Runners
// read the options from the context, so wrappers pass them through.
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
