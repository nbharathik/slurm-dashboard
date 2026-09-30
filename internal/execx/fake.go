package execx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// FakeResponse is the canned outcome of one argv.
type FakeResponse struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error         // returned as-is when set
	Delay    time.Duration // simulated run time; honours ctx cancellation
}

// FakeRunner answers from canned responses and starts no process; it applies RealRunner's Policy (minus the test guard).
type FakeRunner struct {
	// Handler answers argv without a canned response; nil means ErrNoFixture.
	Handler func(ctx context.Context, argv []string) (Result, error)

	policy    Policy
	mu        sync.Mutex
	responses map[string]FakeResponse
	calls     [][]string
	inputs    []RunOpts
}

// Inputs returns the RunWith options of every call, in order.
func (f *FakeRunner) Inputs() []RunOpts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.inputs)
}

// NewFake returns an empty FakeRunner; extraReadOnly is Policy.ExtraReadOnly.
func NewFake(extraReadOnly ...string) *FakeRunner {
	return &FakeRunner{
		policy:    Policy{ExtraReadOnly: extraReadOnly, NoTestGuard: true},
		responses: map[string]FakeResponse{},
	}
}

// Set registers the response for argv.
func (f *FakeRunner) Set(argv []string, resp FakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[Key(argv)] = resp
}

// Calls returns every argv that reached the fake, refused ones included.
func (f *FakeRunner) Calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = slices.Clone(c)
	}
	return out
}

// Run implements Runner.
func (f *FakeRunner) Run(ctx context.Context, argv ...string) (Result, error) {
	argv = slices.Clone(argv)
	f.mu.Lock()
	f.calls = append(f.calls, argv)
	in, _ := InputFrom(ctx)
	f.inputs = append(f.inputs, in)
	resp, ok := f.responses[Key(argv)]
	f.mu.Unlock()

	res := Result{Argv: argv, ExitCode: -1}
	if _, err := f.policy.Check(ctx, argv); err != nil {
		return res, err
	}
	if !ok {
		if f.Handler != nil {
			r, err := f.Handler(ctx, argv)
			r.Argv = argv
			return r, err
		}
		return res, fmt.Errorf("%w: %s", ErrNoFixture, Key(argv))
	}

	if resp.Delay > 0 {
		t := time.NewTimer(resp.Delay)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return res, fmt.Errorf("%s: %w", filepath.Base(argv[0]), ctx.Err())
		}
	}

	res = Result{
		Argv:     argv,
		Stdout:   slices.Clone(resp.Stdout),
		Stderr:   slices.Clone(resp.Stderr),
		ExitCode: resp.ExitCode,
		Duration: resp.Delay,
	}
	switch {
	case resp.Err != nil:
		return res, resp.Err
	case resp.ExitCode != 0:
		return res, &ExitError{Name: filepath.Base(argv[0]), Code: resp.ExitCode, Stderr: FirstLine(resp.Stderr)}
	}
	return res, nil
}

// LoadDir registers the <name>.meta.json and <name>.txt pairs RecordingRunner wrote in dir.
func (f *FakeRunner) LoadDir(dir string) error {
	metas, err := filepath.Glob(filepath.Join(dir, "*.meta.json"))
	if err != nil {
		return err
	}
	for _, metaPath := range metas {
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			return err
		}
		var m recordMeta
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("%s: %w", metaPath, err)
		}
		if len(m.Argv) == 0 {
			return fmt.Errorf("%s: no argv", metaPath)
		}
		stdout, err := os.ReadFile(strings.TrimSuffix(metaPath, ".meta.json") + ".txt")
		if err != nil {
			return err
		}
		resp := FakeResponse{Stdout: stdout, Stderr: []byte(m.Stderr), ExitCode: m.ExitCode}
		if m.Error != "" && m.ExitCode <= 0 {
			resp.Err = fmt.Errorf("recorded error: %s", m.Error)
		}
		f.Set(m.Argv, resp)
	}
	return nil
}
