package execx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// recordMeta is the on-disk companion of a recorded stdout file.
type recordMeta struct {
	Argv       []string  `json:"argv"`
	Label      string    `json:"label,omitempty"`
	ExitCode   int       `json:"exit_code"`
	DurationMS int64     `json:"duration_ms"`
	Stderr     string    `json:"stderr"`
	Error      string    `json:"error,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

// RecordingRunner wraps another Runner and writes every result to Dir as
// <name>.txt (stdout) and <name>.meta.json (argv, exit code, duration,
// stderr). The name is the context label (see WithLabel) or the executable
// name, with -2, -3, ... appended for repeats. FakeRunner.LoadDir reads the
// same layout.
//
// Recorded output can contain real usernames and paths. Anonymise it before
// committing (sdash record --anonymize).
type RecordingRunner struct {
	Inner Runner
	Dir   string

	mu   sync.Mutex
	used map[string]int
}

// Run implements Runner.
func (r *RecordingRunner) Run(ctx context.Context, argv ...string) (Result, error) {
	res, runErr := r.Inner.Run(ctx, argv...)
	if len(argv) == 0 {
		return res, runErr
	}
	if err := r.write(ctx, argv, res, runErr); err != nil {
		if runErr != nil {
			return res, fmt.Errorf("%w (and recording failed: %v)", runErr, err)
		}
		return res, fmt.Errorf("recording %s: %w", filepath.Base(argv[0]), err)
	}
	return res, runErr
}

func (r *RecordingRunner) write(ctx context.Context, argv []string, res Result, runErr error) error {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	name := r.name(ctx, argv)
	meta := recordMeta{
		Argv:       argv,
		Label:      Label(ctx),
		ExitCode:   res.ExitCode,
		DurationMS: res.Duration.Milliseconds(),
		Stderr:     string(res.Stderr),
		RecordedAt: time.Now().UTC(),
	}
	if runErr != nil {
		meta.Error = runErr.Error()
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, name+".txt"), res.Stdout, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Dir, name+".meta.json"), append(raw, '\n'), 0o600)
}

// name picks a unique, filesystem-safe base name for a recording.
func (r *RecordingRunner) name(ctx context.Context, argv []string) string {
	base := Label(ctx)
	if base == "" {
		base = filepath.Base(argv[0])
	}
	base = safeName(base)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used == nil {
		r.used = map[string]int{}
	}
	r.used[base]++
	if n := r.used[base]; n > 1 {
		return fmt.Sprintf("%s-%d", base, n)
	}
	return base
}

// safeName keeps letters, digits, '.', '_' and '-' and replaces the rest.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		}
		return '_'
	}, s)
	s = strings.TrimLeft(s, ".")
	if s == "" {
		return "cmd"
	}
	return s
}
