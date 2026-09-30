//go:build integration

package integration

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
)

// TestUsageCPUHoursMatchSacct checks that the CPU-hours in
// the usage summary equal the sum of CPUTimeRAW that sacct reports for the
// same finished jobs.
func TestUsageCPUHoursMatchSacct(t *testing.T) {
	c := NewCluster(t)
	src := sources(t, c)
	ctx := context.Background()
	// A short job gives the window something recent to add up.
	id := c.Submit("usage", "-c", "2", "-t", "5", "--wrap", "sleep 3")
	c.WaitState(id, "RUNNING", "COMPLETING", "")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && contains(c.SqueueIDs(""), id) {
		time.Sleep(time.Second)
	}
	time.Sleep(3 * time.Second) // let slurmdbd record the job

	h, err := src.History(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	sum := insights.Summarise(h.Jobs, 30, src.Cmd.User)

	res, err := c.Runner.Run(ctx, "sacct", "-n", "-X", "-P", "-u", src.Cmd.User, "-S", "now-30days", "-E", "now",
		"-s", "CD,F,TO,CA,OOM,NF,PR,DL,BF", "-o", "CPUTimeRAW")
	if err != nil {
		t.Fatal(err)
	}
	var want float64
	for _, f := range strings.Fields(string(res.Stdout)) {
		s, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatalf("CPUTimeRAW %q: %v", f, err)
		}
		want += s / 3600
	}
	t.Logf("usage: %.4f CPU-hours over %d jobs; sacct: %.4f", sum.CPUHours, sum.Finished, want)
	if sum.Finished == 0 || want == 0 || math.Abs(sum.CPUHours-want) > 0.01*want+0.001 {
		t.Fatalf("CPU-hours %.4f, sacct says %.4f", sum.CPUHours, want)
	}
}

// TestUsageLimitsReadable reads the limits of the throwaway cluster's
// association: the query is read-only, must parse without warnings and
// must give a plain answer (lines, or a note that none is set).
func TestUsageLimitsReadable(t *testing.T) {
	c := NewCluster(t)
	src := sources(t, c)
	scopes, err := src.Limits(context.Background())
	if err != nil {
		t.Fatalf("Limits: %v", err)
	}
	acct := insights.BuildAccount(insights.AccountInput{User: src.Cmd.User, Scopes: scopes, LimitsRead: true})
	t.Logf("%d limit lines, note %q", len(acct.Limits), acct.LimitsNote)
	if len(acct.Limits) == 0 && acct.LimitsNote == "" {
		t.Error("no limits and no note")
	}
	if n := src.WarningCounts()["assocmgr"]; n != 0 {
		t.Errorf("%d parse warnings on real assoc_mgr output", n)
	}
}

// TestMyStatsSeveralJobs samples two running jobs with one sstat call and
// checks that CPU time and memory come back per job, and that an array
// task (which sstat cannot single out) is left out.
func TestMyStatsSeveralJobs(t *testing.T) {
	c := NewCluster(t)
	src := sources(t, c)
	busy := c.Submit("stats-busy", "-c", "2", "-t", "5", "--wrap", "timeout 40 sh -c 'while :; do :; done' & timeout 40 sh -c 'while :; do :; done' & wait")
	idle := c.Submit("stats-idle", "-t", "5", "--wrap", "sleep 40")
	c.WaitState(busy, "RUNNING")
	c.WaitState(idle, "RUNNING")
	time.Sleep(6 * time.Second) // let the gatherer take a sample

	stats, err := src.MyStats(context.Background(), []string{busy, idle, busy + "_1", "not-a-job"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats for %v, want just the two plain jobs: %+v", keysOf(stats), stats)
	}
	b, i := stats[busy], stats[idle]
	t.Logf("busy: %+v idle: %+v", b, i)
	if b.NTasks < 1 || i.NTasks < 1 {
		t.Errorf("no tasks: %+v %+v", b, i)
	}
	if b.TotalCPU <= i.TotalCPU {
		t.Errorf("the busy job used %v of CPU, the idle one %v", b.TotalCPU, i.TotalCPU)
	}
}

func keysOf[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
