package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Defaults for Options fields left at zero.
const (
	DefaultMaxConcurrent = 3
	DefaultTimeout       = 30 * time.Second
	DefaultMaxStdout     = 64 << 20
	DefaultMaxStderr     = 1 << 20
	DefaultHistorySize   = 200
	waitDelay            = 2 * time.Second
)

// Options configures a RealRunner. Zero fields take the defaults above.
type Options struct {
	// MaxConcurrent caps simultaneous commands (Slurm runner 3, to protect the controller).
	MaxConcurrent int
	// DefaultTimeout applies when ctx has no deadline.
	DefaultTimeout time.Duration
	// MaxStdout is the stdout cap in bytes; exceeding it kills the command (ErrOutputTooLarge).
	MaxStdout int
	// MaxStderr is the stderr cap in bytes; extra is dropped.
	MaxStderr int
	// HistorySize is how many recent calls History keeps.
	HistorySize int
	// BaseEnv is the environment before sanitising; nil means os.Environ().
	BaseEnv []string
	// Logger gets a debug record per call and a warning per refusal; nil discards.
	Logger *slog.Logger
	// Policy decides which commands may run.
	Policy Policy
}

// CallRecord describes one call for the debug overlay and sdash doctor.
type CallRecord struct {
	Argv     []string
	Label    string
	Start    time.Time
	Duration time.Duration
	ExitCode int
	Err      string // "" on success
	Refused  bool   // true when the policy refused the command
}

// RealRunner runs commands locally; safe for concurrent use.
type RealRunner struct {
	opts Options
	sem  chan struct{}

	mu      sync.Mutex
	history []CallRecord
	next    int
	full    bool

	// exec runs an approved command; tests replace it.
	exec func(ctx context.Context, argv []string) (Result, error)
}

// NewReal returns a RealRunner configured by opts.
func NewReal(opts Options) *RealRunner {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = DefaultMaxConcurrent
	}
	if opts.DefaultTimeout <= 0 {
		opts.DefaultTimeout = DefaultTimeout
	}
	if opts.MaxStdout <= 0 {
		opts.MaxStdout = DefaultMaxStdout
	}
	if opts.MaxStderr <= 0 {
		opts.MaxStderr = DefaultMaxStderr
	}
	if opts.HistorySize <= 0 {
		opts.HistorySize = DefaultHistorySize
	}
	if opts.BaseEnv == nil {
		opts.BaseEnv = os.Environ()
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	r := &RealRunner{
		opts:    opts,
		sem:     make(chan struct{}, opts.MaxConcurrent),
		history: make([]CallRecord, opts.HistorySize),
	}
	r.exec = r.execute
	return r
}

// Run checks argv against the policy, waits for a slot, then runs it sanitised, timed and output-capped.
func (r *RealRunner) Run(ctx context.Context, argv ...string) (Result, error) {
	argv = slices.Clone(argv)
	res := Result{Argv: argv, ExitCode: -1}
	start := time.Now()

	if _, err := r.opts.Policy.Check(ctx, argv); err != nil {
		r.record(ctx, res, start, err, true)
		return res, err
	}

	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		err := fmt.Errorf("%s: waiting for a free slot: %w", filepath.Base(argv[0]), ctx.Err())
		r.record(ctx, res, start, err, false)
		return res, err
	}
	defer func() { <-r.sem }()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.opts.DefaultTimeout)
		defer cancel()
	}

	res, err := r.exec(ctx, argv)
	res.Argv = argv
	r.record(ctx, res, start, err, false)
	return res, err
}

// RunUserShell runs a user-configured command via "sh -c", outside the allowlist on purpose.
// Semaphore, timeout and caps still apply. Pass job data only in extraEnv, never in script.
// Only internal/storage and internal/notify may call it (lint-enforced).
func RunUserShell(ctx context.Context, r *RealRunner, script string, extraEnv []string) (Result, error) {
	argv := []string{"sh", "-c", script}
	res := Result{Argv: argv, ExitCode: -1}
	start := time.Now()
	if strings.TrimSpace(script) == "" || strings.ContainsRune(script, 0) {
		err := fmt.Errorf("%w: empty or invalid shell command", ErrInvalidArgv)
		r.record(ctx, res, start, err, true)
		return res, err
	}
	for _, kv := range extraEnv {
		if !strings.HasPrefix(kv, "SDASH_") || !strings.Contains(kv, "=") || strings.ContainsAny(kv, "\x00") {
			err := fmt.Errorf("%w: extra environment must be SDASH_*=value", ErrInvalidArgv)
			r.record(ctx, res, start, err, true)
			return res, err
		}
	}
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		err := fmt.Errorf("sh: waiting for a free slot: %w", ctx.Err())
		r.record(ctx, res, start, err, false)
		return res, err
	}
	defer func() { <-r.sem }()
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.opts.DefaultTimeout)
		defer cancel()
	}
	res, err := r.executeEnv(WithLabel(ctx, "user-shell"), argv, append(SanitizeEnv(r.opts.BaseEnv), extraEnv...))
	res.Argv = argv
	r.record(WithLabel(ctx, "user-shell"), res, start, err, false)
	return res, err
}

// execute starts the process and maps failures onto the package errors.
func (r *RealRunner) execute(ctx context.Context, argv []string) (Result, error) {
	return r.executeEnv(ctx, argv, SanitizeEnv(r.opts.BaseEnv))
}

func (r *RealRunner) executeEnv(ctx context.Context, argv, env []string) (Result, error) {
	res := Result{Argv: argv, ExitCode: -1}
	name := filepath.Base(argv[0])

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = nil // reads from /dev/null; a command can never consume the TUI's input
	if in, ok := InputFrom(ctx); ok {
		if in.Stdin != nil {
			cmd.Stdin = bytes.NewReader(in.Stdin)
		}
		if in.Dir != "" {
			if fi, err := os.Stat(in.Dir); err != nil || !fi.IsDir() {
				return res, fmt.Errorf("%s: %s: %w", name, in.Dir, errNoDir)
			}
			cmd.Dir = in.Dir
		}
	}
	stdout := &capBuffer{limit: r.opts.MaxStdout, onOverflow: func() { cancel(ErrOutputTooLarge) }}
	stderr := &capBuffer{limit: r.opts.MaxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)

	begin := time.Now()
	err := cmd.Run()
	res.Duration = time.Since(begin)
	res.Stdout, res.Stderr = stdout.Bytes(), stderr.Bytes()
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}

	switch {
	case stdout.Overflowed():
		// Checked first: output is truncated even if the command exited before the kill landed.
		return res, fmt.Errorf("%s: stdout exceeded %d bytes: %w", name, r.opts.MaxStdout, ErrOutputTooLarge)
	case err == nil:
		return res, nil
	case cmd.ProcessState == nil && (errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)):
		return res, fmt.Errorf("%s: %w", name, ErrNotFound)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return res, fmt.Errorf("%s: %w after %s: %w", name, ErrTimeout, res.Duration.Round(time.Millisecond), context.DeadlineExceeded)
	case ctx.Err() != nil:
		return res, fmt.Errorf("%s: %w", name, ctx.Err())
	case cmd.ProcessState == nil:
		return res, fmt.Errorf("%s: %w", name, err)
	case errors.Is(err, exec.ErrWaitDelay):
		return res, fmt.Errorf("%s: exited but left its output open: %w", name, err)
	case res.ExitCode != 0:
		return res, &ExitError{Name: name, Code: res.ExitCode, Stderr: FirstLine(res.Stderr)}
	default:
		return res, fmt.Errorf("%s: %w", name, err)
	}
}

// record appends a call to the history ring and the debug log.
func (r *RealRunner) record(ctx context.Context, res Result, start time.Time, err error, refused bool) {
	rec := CallRecord{
		Argv:     res.Argv,
		Label:    Label(ctx),
		Start:    start,
		Duration: res.Duration,
		ExitCode: res.ExitCode,
		Refused:  refused,
	}
	if err != nil {
		rec.Err = err.Error()
	}

	r.mu.Lock()
	r.history[r.next] = rec
	r.next = (r.next + 1) % len(r.history)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()

	attrs := []any{
		"argv", Key(res.Argv),
		"label", rec.Label,
		"took", rec.Duration.Round(time.Millisecond),
		"exit", rec.ExitCode,
	}
	if action, ok := MutationFrom(ctx, res.Argv); ok {
		attrs = append(attrs, "action", action)
	}
	switch {
	case refused:
		r.opts.Logger.Warn("exec refused", append(attrs, "err", rec.Err)...)
	case err != nil:
		r.opts.Logger.Debug("exec failed", append(attrs, "err", rec.Err)...)
	default:
		r.opts.Logger.Debug("exec", attrs...)
	}
}

// History returns up to HistorySize most recent calls, oldest first.
func (r *RealRunner) History() []CallRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return slices.Clone(r.history[:r.next])
	}
	out := make([]CallRecord, 0, len(r.history))
	out = append(out, r.history[r.next:]...)
	return append(out, r.history[:r.next]...)
}
