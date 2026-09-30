package state

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// Request is what a job would ask for, per node.
type Request struct {
	GPUs      int
	GPUType   string // raw or short name ("h200", "H200 NVL") or a MIG profile; "" = any
	CPUs      int
	MemMB     int64
	Time      time.Duration // 0 = no limit asked
	Nodes     int           // how many nodes; 0 or 1 = one
	Partition string        // "" = any
}

// Fit is a node that could take the request.
type Fit struct {
	Node       string
	Partitions []string // the eligible ones
	FreeCPUs   int
	FreeGPUs   int   // of the requested type (MIG slices for a MIG profile)
	FreeMemMB  int64 // -1 when Slurm does not track memory on the node
	At         time.Time
}

// FitResult answers "where can I run?". Now lists nodes that fit at once;
// when none (or too few) do, Soonest is the node that frees up first by
// the end times of its running jobs. Jobs waiting ahead of this one are
// not taken into account.
type FitResult struct {
	Now     []Fit
	Soonest *Fit
	// Reasons explains an empty answer: no node has that GPU type, every
	// partition's time limit is too short, ...
	Reasons []string
}

// FitNodes finds the nodes that fit req now, or the soonest one.
func FitNodes(usage []NodeUsage, parts []model.Partition, req Request, aliases map[string]string) FitResult {
	var res FitResult
	eligible := map[string][]string{} // node → partitions it may run in
	partsOK := 0
	for _, p := range parts {
		if req.Partition != "" && p.Name != req.Partition {
			continue
		}
		if p.State != "UP" || (req.Time > 0 && p.MaxTime != nil && *p.MaxTime < req.Time) {
			continue
		}
		partsOK++
		for _, n := range p.Nodes {
			eligible[n] = append(eligible[n], p.Name)
		}
	}
	switch {
	case req.Partition != "" && !slices.ContainsFunc(parts, func(p model.Partition) bool { return p.Name == req.Partition }):
		res.Reasons = append(res.Reasons, "no partition "+req.Partition)
	case partsOK == 0:
		res.Reasons = append(res.Reasons, "no partition that is up allows that time limit")
	}
	typed := false
	busy := 0 // nodes big enough but full, with no end times to go by
	for _, u := range usage {
		ps, ok := eligible[u.Node.Name]
		if !ok || !Available(u.Node) {
			continue
		}
		total, free := gpusOf(u.Node, req.GPUType, aliases)
		if req.GPUs > 0 && total < req.GPUs {
			continue // never enough GPUs of that type here
		}
		typed = true
		if req.CPUs > u.Node.CPUTotal || (req.MemMB > 0 && u.Node.MemTotalMB > 0 && req.MemMB > u.Node.MemTotalMB) {
			continue
		}
		f := Fit{Node: u.Node.Name, Partitions: ps, FreeCPUs: max(u.Node.CPUTotal-u.Node.CPUAlloc, 0), FreeGPUs: free, FreeMemMB: -1}
		if u.Node.MemTracked() && u.Node.MemTotalMB > 0 {
			f.FreeMemMB = max(u.Node.MemTotalMB-u.Node.MemAllocMB, 0)
		}
		if fits(f, req) {
			res.Now = append(res.Now, f)
			continue
		}
		// Release the node's jobs in end-time order until it fits.
		freed := false
		for _, j := range u.Jobs {
			if j.EndTime.IsZero() {
				break // no end time: we cannot tell
			}
			n := max(len(j.NodeList), 1)
			f.FreeCPUs += j.CPUs / n
			f.FreeGPUs += spread(j.GPUs, n) // assumes the job used the requested type
			if f.FreeMemMB >= 0 {
				f.FreeMemMB += j.MemMB / int64(n)
			}
			if fits(f, req) {
				f.At = j.EndTime
				if res.Soonest == nil || f.At.Before(res.Soonest.At) {
					res.Soonest = &f
				}
				freed = true
				break
			}
		}
		if !freed {
			busy++
		}
	}
	if len(res.Now) == 0 && res.Soonest == nil && busy > 0 {
		res.Reasons = append(res.Reasons, strconv.Itoa(busy)+" node(s) could run it but are busy, and the end times of the jobs there are not visible")
	}
	if req.GPUs > 0 && !typed && len(res.Reasons) == 0 {
		what := strconv.Itoa(req.GPUs) + " GPU"
		if req.GPUType != "" {
			what += " of type " + req.GPUType
		}
		if req.GPUs > 1 {
			what = strings.Replace(what, " GPU", " GPUs", 1)
		}
		res.Reasons = append(res.Reasons, "no available node in a matching partition has "+what)
	}
	slices.SortFunc(res.Now, func(a, b Fit) int {
		return cmp.Or(cmp.Compare(b.FreeGPUs, a.FreeGPUs), cmp.Compare(b.FreeCPUs, a.FreeCPUs), strings.Compare(a.Node, b.Node))
	})
	return res
}

func fits(f Fit, req Request) bool {
	return f.FreeCPUs >= max(req.CPUs, 1) && f.FreeGPUs >= req.GPUs && (f.FreeMemMB < 0 || f.FreeMemMB >= req.MemMB)
}

// gpusOf counts a node's GPUs matching want (all whole GPUs and MIG slices
// when want is empty) and how many of them are free.
func gpusOf(n model.Node, want string, aliases map[string]string) (total, free int) {
	w := strings.ToLower(strings.TrimSpace(want))
	for _, g := range n.GPUs {
		if w != "" && !matchesType(g.Type, w, aliases) {
			continue
		}
		if w == "" && g.MIG && n.GPUTotal > 0 {
			continue // an untyped request counts whole GPUs where there are any
		}
		total += g.Total
		free += max(g.Total-g.Alloc, 0)
	}
	return total, free
}

func matchesType(raw, want string, aliases map[string]string) bool {
	if strings.EqualFold(raw, want) {
		return true
	}
	if units.IsMIG(want) != units.IsMIG(raw) {
		return false
	}
	short := strings.ToLower(units.GPUDisplayName(raw, aliases))
	squash := func(s string) string {
		return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(s))
	}
	return strings.Contains(squash(raw), squash(want)) || strings.Contains(squash(short), squash(want))
}

// ResolveGPUType returns Slurm's name for the first GPU type matching want
// ("h200" → "nvidia_h200_nvl"), or want itself when nothing matches.
func ResolveGPUType(usage []NodeUsage, want string, aliases map[string]string) string {
	w := strings.ToLower(strings.TrimSpace(want))
	for _, u := range usage {
		for _, g := range u.Node.GPUs {
			if w != "" && g.Type != "" && matchesType(g.Type, w, aliases) {
				return g.Type
			}
		}
	}
	return want
}
