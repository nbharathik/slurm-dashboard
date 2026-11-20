package state

import (
	"errors"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Every source the scheduler can deliver is kept by the Store, and an
// update of the wrong type is refused rather than stored.
func TestRegistryCoversEverySource(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range SourceNames() {
		if seen[name] {
			t.Fatalf("%s registered twice", name)
		}
		seen[name] = true
	}
	for _, name := range []string{"myjobs", "alljobs", "cluster", "queuerank", "nodes", "partitions", "reservations", "jobdetail", "history", "fairshare", "sprio", "limits", "mystats", "storage", "groups", "sdiag"} {
		if !seen[name] {
			t.Errorf("source %s is not registered", name)
		}
	}
	s := &Store{}
	now := time.Now()
	s.Apply(Update{Source: "sdiag", Data: model.SchedStats{Running: 3}, At: now})
	if st := s.Status("sdiag"); !st.Has || !st.At.Equal(now) || s.Sched.Data.Running != 3 {
		t.Fatalf("sdiag status %+v", st)
	}
	s.Apply(Update{Source: "nodes", Data: "not nodes", At: now})
	if s.Status("nodes").Has {
		t.Fatal("wrong data type stored")
	}
	s.Apply(Update{Source: "nodes", Err: Classify(errors.New("exit 1"), []byte(rateLimitText)), At: now})
	if !s.RateLimited(now) {
		t.Fatal("rate limit through the registry not seen")
	}
	if st := s.Status("nope"); st.Registered {
		t.Fatal("unknown source reported as registered")
	}
}
