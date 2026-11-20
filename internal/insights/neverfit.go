package insights

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// CannotRun says why a pending job can never start in any of its
// partitions: a time limit above the partition's, more nodes than it has,
// or more per node than its largest node. Nil when it may run somewhere or
// when the data to tell is missing. GPU types are not compared, as sites
// name them differently in requests and node data.
func CannotRun(j model.Job, parts []model.Partition, nodes []model.Node) []string {
	if j.State != model.StatePending || len(parts) == 0 {
		return nil
	}
	byName := make(map[string]model.Node, len(nodes))
	for _, n := range nodes {
		byName[n.Name] = n
	}
	var reasons []string
	checked := 0
	for _, name := range strings.Split(j.Partition, ",") {
		var p *model.Partition
		for i := range parts {
			if parts[i].Name == name {
				p = &parts[i]
			}
		}
		if p == nil {
			continue
		}
		checked++
		r := partitionMisfit(j, *p, byName)
		if len(r) == 0 {
			return nil // it fits here
		}
		reasons = append(reasons, r...)
	}
	if checked == 0 {
		return nil
	}
	return reasons
}

func partitionMisfit(j model.Job, p model.Partition, nodes map[string]model.Node) []string {
	var out []string
	if j.TimeLimit != nil && p.MaxTime != nil && *j.TimeLimit > *p.MaxTime {
		out = append(out, fmt.Sprintf("it asks for %s, partition %s allows at most %s", units.FormatLimit(*j.TimeLimit), p.Name, units.FormatLimit(*p.MaxTime)))
	}
	if len(p.Nodes) > 0 && j.Nodes > len(p.Nodes) {
		out = append(out, fmt.Sprintf("it asks for %d nodes, partition %s has %d", j.Nodes, p.Name, len(p.Nodes)))
	}
	n := max(j.Nodes, 1)
	cpus, gpus, mem := (j.CPUs+n-1)/n, j.GPUs/n, j.MemPerNodeMB
	var big model.Node
	known := 0
	for _, name := range p.Nodes {
		nd, ok := nodes[name]
		if !ok {
			continue
		}
		known++
		if nd.CPUTotal >= cpus && (mem <= 0 || nd.MemTotalMB <= 0 || nd.MemTotalMB >= mem) && nd.GPUTotal+nd.MIGTotal >= gpus {
			return out // a node fits; only the limits above can stop it
		}
		big.CPUTotal = max(big.CPUTotal, nd.CPUTotal)
		big.MemTotalMB = max(big.MemTotalMB, nd.MemTotalMB)
		big.GPUTotal = max(big.GPUTotal, nd.GPUTotal+nd.MIGTotal)
	}
	if known < len(p.Nodes) || known == 0 {
		return out // node data incomplete: say nothing about sizes
	}
	switch {
	case gpus > big.GPUTotal:
		out = append(out, fmt.Sprintf("it asks for %d GPUs per node, the largest node in %s has %d", gpus, p.Name, big.GPUTotal))
	case cpus > big.CPUTotal:
		out = append(out, fmt.Sprintf("it asks for %d CPUs per node, the largest node in %s has %d", cpus, p.Name, big.CPUTotal))
	case mem > big.MemTotalMB:
		out = append(out, fmt.Sprintf("it asks for %s per node, the largest node in %s has %s", units.FormatMB(float64(mem)), p.Name, units.FormatMB(float64(big.MemTotalMB))))
	default:
		out = append(out, fmt.Sprintf("no single node in %s has all it asks for at once", p.Name))
	}
	return out
}

// minWaitJobs is how many started jobs a typical wait needs.
const minWaitJobs = 3

// TypicalWait is the median time the user's jobs waited in a partition
// before starting, from history. ok is false with fewer than minWaitJobs.
// Array tasks count once per array, so one big sweep does not dominate.
func TypicalWait(history []model.HistoryJob, partition string) (median time.Duration, n int, ok bool) {
	seen := map[uint64]bool{}
	var waits []time.Duration
	for _, h := range history {
		if h.Partition != partition || h.Submit.IsZero() || h.Start.IsZero() || h.Start.Before(h.Submit) {
			continue
		}
		if h.ID.IsArray() {
			if seen[h.ID.ArrayJobID] {
				continue
			}
			seen[h.ID.ArrayJobID] = true
		}
		waits = append(waits, h.Start.Sub(h.Submit))
	}
	if len(waits) < minWaitJobs {
		return 0, len(waits), false
	}
	slices.Sort(waits)
	return waits[len(waits)/2], len(waits), true
}

// TypicalWaitLine is "Your jobs in gpu usually waited 42m (median of 12)."
func TypicalWaitLine(history []model.HistoryJob, partition string) string {
	first, _, _ := strings.Cut(partition, ",")
	d, n, ok := TypicalWait(history, first)
	if !ok {
		return ""
	}
	return fmt.Sprintf("Your jobs in %s usually waited %s (median of %d in your history).", first, units.FormatShort(d), n)
}

// CannotRunLines are the why-card lines for CannotRun's reasons.
func CannotRunLines(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	return []string{
		"This job can never start as requested: " + strings.Join(reasons, "; ") + ".",
		"Tip: cancel it and resubmit with a smaller request or another partition; sdash free shows what fits now.",
	}
}
