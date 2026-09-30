//go:build integration

package integration

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// seffReference computes CPU and memory efficiency the way seff does, from
// raw sacct fields: TotalCPU / (AllocCPUS × Elapsed) and the largest step
// MaxRSS over the requested memory.
func seffReference(t *testing.T, c *Cluster, id string) (cpu, mem float64) {
	t.Helper()
	res, err := c.Runner.Run(context.Background(), "sacct", "-n", "-P", "-j", id, "-o", "JobID,TotalCPU,ElapsedRaw,AllocCPUS,ReqMem,MaxRSS")
	if err != nil {
		t.Fatal(err)
	}
	var total, elapsed float64
	var cpus int
	var reqMB, maxRSSMB float64
	for _, line := range strings.Split(strings.TrimSpace(string(res.Stdout)), "\n") {
		f := strings.Split(line, "|")
		if len(f) != 6 {
			continue
		}
		if f[0] == id {
			d, _, err := units.ParseDuration(f[1])
			if err != nil {
				t.Fatalf("TotalCPU %q: %v", f[1], err)
			}
			total = d.Seconds()
			elapsed, _ = strconv.ParseFloat(f[2], 64)
			cpus, _ = strconv.Atoi(f[3])
			mb, perCPU, err := units.ParseMemMB(f[4])
			if err != nil {
				t.Fatalf("ReqMem %q: %v", f[4], err)
			}
			if perCPU {
				mb *= float64(cpus)
			}
			reqMB = mb
		}
		if f[5] != "" {
			if mb, _, err := units.ParseMemMB(f[5]); err == nil {
				maxRSSMB = math.Max(maxRSSMB, mb)
			}
		}
	}
	return total / (float64(cpus) * elapsed), maxRSSMB / reqMB
}

// TestEfficiencyMatchesSeffFormula checks that sdash's
// CPU and memory efficiency are within 2 percentage points of seff's
// formula for a job with a known load (seff itself is not packaged here).
func TestEfficiencyMatchesSeffFormula(t *testing.T) {
	c := NewCluster(t)
	src := sources(t, c)
	// One shell busy loop plus a Python busy loop holding ~300 MB: two
	// CPUs busy for 20 s. Command lines cannot contain newlines.
	script := "timeout 20 sh -c 'while :; do :; done' & " +
		"python3 -c 'import time; x = bytearray(300 << 20); x[::4096] = b\"\\x01\" * len(x[::4096]); " +
		"t = time.time(); any(time.time() - t > 20 for _ in iter(int, 1))'; wait"
	id := c.Submit("eff", "-c", "2", "--mem=1000M", "-t", "5", "--wrap", script)
	c.WaitState(id, "RUNNING")
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if len(c.SqueueIDs("")) == 0 || !contains(c.SqueueIDs(""), id) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	time.Sleep(3 * time.Second) // let slurmdbd record the steps
	h, err := src.HistoryJob(context.Background(), id)
	if err != nil || h == nil {
		t.Fatalf("history of %s: %v", id, err)
	}
	refCPU, refMem := seffReference(t, c, id)
	t.Logf("sdash: cpu %.3f mem %.3f; seff formula: cpu %.3f mem %.3f", h.Eff.CPU, h.Eff.Mem, refCPU, refMem)
	if math.Abs(h.Eff.CPU-refCPU) > 0.02 || math.Abs(h.Eff.Mem-refMem) > 0.02 {
		t.Fatalf("efficiency differs from the seff formula by more than 2 points")
	}
	if h.Eff.CPU < 0.7 || h.Eff.CPU > 1.05 || h.Eff.Mem < 0.15 || h.Eff.Mem > 0.5 {
		t.Fatalf("implausible efficiency for the workload: cpu %.2f mem %.2f", h.Eff.CPU, h.Eff.Mem)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
