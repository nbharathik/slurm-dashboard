//go:build integration

package integration

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// TestRerunOnTestJob checks that a held
// sdash-test-* job submitted with command-line options is read back from
// SubmitLine and its stored script, and submitted again with more memory.
func TestRerunOnTestJob(t *testing.T) {
	c := NewCluster(t)
	src := sources(t, c)
	ctx := context.Background()
	orig := c.Submit("rerun", "--hold", "-p", "cpu", "-t", "5", "--mem", "100M", "--wrap", "sleep 1")

	var line, dir string
	for deadline := time.Now().Add(30 * time.Second); line == "" && time.Now().Before(deadline); time.Sleep(time.Second) {
		line, dir, _ = src.SubmitLine(ctx, orig) // accounting lags the controller
	}
	script, err := src.BatchScript(ctx, orig)
	if err != nil || line == "" || dir == "" {
		t.Fatalf("script %v, line %q, dir %q", err, line, dir)
	}
	parsed, err := parse.SubmitLine(line, "sdash-test-rerun")
	if err != nil || len(parsed.Unread) > 0 {
		t.Fatalf("SubmitLine(%q) = %+v %v", line, parsed, err)
	}
	plan := actions.PlanRerun(&parsed, parse.ScriptDirectives(script), nil)
	if !slices.Contains(plan.Dropped, "--hold") {
		t.Errorf("--hold should not be carried: %+v", plan)
	}
	opts, err := plan.Opts(map[string]string{"mem": "200M"})
	if err != nil {
		t.Fatal(err)
	}
	argv, err := actions.RerunArgv(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--job-name=sdash-test-rerun", "--partition=cpu", "--time=5", "--mem=200M"} {
		if !slices.Contains(argv, want) {
			t.Errorf("argv %q lacks %s", argv, want)
		}
	}
	res, err := actions.Rerun(ctx, c.Runner, argv, script, dir)
	if err != nil {
		t.Fatal(err)
	}
	c.Track(res.JobID)
	out, err := c.Runner.Run(ctx, "scontrol", "show", "job", "-o", res.JobID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"JobName=sdash-test-rerun", "Partition=cpu", "MinMemoryNode=200M", "TimeLimit=00:05:00"} {
		if !strings.Contains(string(out.Stdout), want) {
			t.Errorf("rerun job lacks %s:\n%s", want, out.Stdout)
		}
	}
}
