package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// A task still inside a pending range is queued.
func TestQueuedFindsTaskInPendingRange(t *testing.T) {
	jobs := []model.Job{{ID: model.JobID{Raw: "815_[3-9]", ArrayJobID: 815, TaskSpec: "[3-9]"}}}
	for id, want := range map[string]bool{"815_3": true, "815_9": true, "815_2": false, "815": true, "816_3": false} {
		if got := queued(jobs, id); got != want {
			t.Errorf("queued(%q) = %v", id, got)
		}
	}
}

// Stopping wait while a job runs never exits 0.
func TestWaitInterruptedFails(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if code := Main(ctx, []string{"--demo", "wait", "812"}, h.env()); code == ExitOK {
		t.Fatalf("interrupted wait exited 0: %q", h.stderr.String())
	}
	if !strings.Contains(h.stderr.String(), "stopped while waiting for 812") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

// A job that already left the queue is reported at once; the exit code is
// 0 only when every job completed.
func TestWaitReportsFinishedJobs(t *testing.T) {
	h := newHarness(t)
	if code := h.run("--demo", "wait", "806"); code != ExitOK {
		t.Fatalf("completed job: exit %d, %q %q", code, h.stdout.String(), h.stderr.String())
	}
	if out := h.stdout.String(); !strings.Contains(out, "806 COMPLETED") {
		t.Errorf("output = %q", out)
	}
	if code := h.run("--demo", "wait", "806", "809", "--json"); code != ExitError {
		t.Fatalf("a failed job must exit 1, got %d", code)
	}
	var doc struct {
		Schema int `json:"schema"`
		Jobs   []struct {
			ID, State string
		} `json:"jobs"`
	}
	if err := json.Unmarshal(h.stdout.Bytes(), &doc); err != nil || doc.Schema != 1 || len(doc.Jobs) != 2 || doc.Jobs[1].State != "OUT_OF_MEMORY" {
		t.Fatalf("json = %+v %v\n%s", doc, err, h.stdout.String())
	}
	if code := h.run("--demo", "wait", "812; rm"); code != ExitUsage {
		t.Fatalf("bad job ID: exit %d", code)
	}
}
