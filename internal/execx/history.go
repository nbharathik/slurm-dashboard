package execx

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// HistoryRunner wraps a runner with a ring of recent calls (for demo mode) and logs each call.
type HistoryRunner struct {
	r   Runner
	log *slog.Logger

	mu   sync.Mutex
	ring []CallRecord
	next int
	full bool
}

// WithHistory wraps r, keeping the last size calls.
func WithHistory(r Runner, size int, log *slog.Logger) *HistoryRunner {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &HistoryRunner{r: r, log: log, ring: make([]CallRecord, max(size, 1))}
}

// Run implements Runner.
func (h *HistoryRunner) Run(ctx context.Context, argv ...string) (Result, error) {
	start := time.Now()
	res, err := h.r.Run(ctx, argv...)
	rec := CallRecord{Argv: slices.Clone(argv), Label: Label(ctx), Start: start, Duration: res.Duration, ExitCode: res.ExitCode}
	if err != nil {
		rec.Err = err.Error()
		rec.Refused = errors.Is(err, ErrNotAllowed) || errors.Is(err, ErrMutationNotAuthorized) || errors.Is(err, ErrInvalidArgv)
	}
	h.mu.Lock()
	h.ring[h.next] = rec
	h.next = (h.next + 1) % len(h.ring)
	h.full = h.full || h.next == 0
	h.mu.Unlock()
	attrs := []any{"argv", Key(argv), "label", rec.Label, "took", rec.Duration.Round(time.Millisecond), "exit", rec.ExitCode}
	switch {
	case rec.Refused:
		h.log.Warn("exec refused", append(attrs, "err", rec.Err)...)
	case err != nil:
		h.log.Debug("exec failed", append(attrs, "err", rec.Err)...)
	default:
		h.log.Debug("exec", attrs...)
	}
	return res, err
}

// History returns the recorded calls, oldest first.
func (h *HistoryRunner) History() []CallRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.full {
		return slices.Clone(h.ring[:h.next])
	}
	return append(slices.Clone(h.ring[h.next:]), h.ring[:h.next]...)
}
