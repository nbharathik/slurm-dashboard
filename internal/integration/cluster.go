//go:build integration

package integration

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// ThrowawayCluster is the only cluster these tests may use.
// Tests refuse to run against any other cluster, so a stray
// SDASH_INTEGRATION=local on a login node cannot touch real jobs.
const ThrowawayCluster = "sdashtest"

// Cluster gives a test a real runner and cleans up the jobs it submits.
type Cluster struct {
	t      testing.TB
	Runner *execx.RealRunner
	jobs   []string
}

// NewCluster connects to the throwaway cluster, or skips the test.
func NewCluster(t testing.TB) *Cluster {
	t.Helper()
	if os.Getenv("SDASH_INTEGRATION") != "local" {
		t.Skip("set SDASH_INTEGRATION=local to run against a throwaway sdashtest cluster")
	}
	c := &Cluster{t: t, Runner: execx.NewReal(execx.Options{Policy: execx.Policy{AllowClusterToolsInTests: true}})}
	res, err := c.Runner.Run(context.Background(), "scontrol", "show", "config")
	if err != nil {
		t.Fatalf("scontrol show config: %v", err)
	}
	if name, err := parse.ClusterName(res.Stdout); err != nil || name != ThrowawayCluster {
		t.Fatalf("refusing to run: cluster is %q, not the throwaway %q (%v)", name, ThrowawayCluster, err)
	}
	t.Cleanup(c.cleanup)
	return c
}

// Submit runs sbatch for a test job and returns its ID. Names always start
// with sdash-test-.
func (c *Cluster) Submit(name string, args ...string) string {
	c.t.Helper()
	argv := append([]string{"sbatch", "--parsable", "-J", "sdash-test-" + name, "-o", "/dev/null"}, args...)
	ctx := execx.WithMutation(context.Background(), "integration-submit", argv)
	res, err := c.Runner.Run(ctx, argv...)
	if err != nil {
		c.t.Fatalf("sbatch %s: %v (%s)", name, err, res.Stderr)
	}
	id := strings.TrimSpace(strings.Split(string(res.Stdout), ";")[0])
	c.jobs = append(c.jobs, id)
	return id
}

// Track makes cleanup cancel a job the test submitted another way (through
// the UI). Only sdash-test-* jobs may be tracked.
func (c *Cluster) Track(id string) { c.jobs = append(c.jobs, id) }

func (c *Cluster) cleanup() {
	if len(c.jobs) == 0 {
		return
	}
	argv := append([]string{"scancel"}, c.jobs...)
	ctx := execx.WithMutation(context.Background(), "integration-cleanup", argv)
	_, _ = c.Runner.Run(ctx, argv...)
}

// SqueueIDs lists the IDs squeue shows for the current user and states.
func (c *Cluster) SqueueIDs(states string) []string {
	c.t.Helper()
	argv := []string{"squeue", "--me", "-h", "-o", "%i"}
	if states != "" {
		argv = append(argv, "-t", states)
	}
	res, err := c.Runner.Run(context.Background(), argv...)
	if err != nil {
		c.t.Fatalf("squeue: %v", err)
	}
	ids := strings.Fields(string(res.Stdout))
	slices.Sort(ids)
	return ids
}

// WaitState polls until the job reaches one of the states.
func (c *Cluster) WaitState(id string, states ...string) {
	c.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		res, _ := c.Runner.Run(context.Background(), "squeue", "-h", "-j", id, "-o", "%T")
		if slices.Contains(states, strings.TrimSpace(string(res.Stdout))) {
			return
		}
		time.Sleep(time.Second)
	}
	c.t.Fatalf("job %s never reached %v", id, states)
}
