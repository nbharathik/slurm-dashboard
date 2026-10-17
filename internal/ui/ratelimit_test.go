package ui

import (
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func TestRateLimitLineShowsAndClears(t *testing.T) {
	const line = "controller is rate-limiting; slowing down"
	f := newFixture(t, 0)
	a := f.app(t, 160, 45, false)
	if strings.Contains(screen(a), line) {
		t.Fatal("rate limit line shown without a rate limit")
	}
	limited := state.Classify(&execx.ExitError{Code: 1}, []byte("squeue: error: slurm_load_jobs error: Rate limit exceeded, please retry later"))
	a.applyUpdate(state.Update{Source: "nodes", Err: limited, At: f.clock.Now()})
	if !strings.Contains(screen(a), line) {
		t.Fatalf("rate limit line missing:\n%s", screen(a))
	}
	a.applyUpdate(state.Update{Source: "nodes", Data: f.st.Nodes.Data, At: f.clock.Now()})
	if strings.Contains(screen(a), line) {
		t.Fatal("rate limit line stayed after the source recovered")
	}
}
