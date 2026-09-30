package state

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// NodeSorts are the orders SortNodes knows, in the order s cycles them.
var NodeSorts = []string{"free", "name", "state", "cpu", "gpu", "load"}

// SortNodes orders node usage by NodeSorts key; ties fall back to name for stability.
func SortNodes(list []NodeUsage, by string, desc bool) {
	freeCPU := func(u NodeUsage) int {
		if !Available(u.Node) {
			return 0
		}
		return max(u.Node.CPUTotal-u.Node.CPUAlloc, 0)
	}
	key := func(a, b NodeUsage) int {
		switch by {
		case "name":
			return strings.Compare(a.Node.Name, b.Node.Name)
		case "state":
			return cmp.Compare(NodeStateLabel(a.Node), NodeStateLabel(b.Node))
		case "cpu":
			return cmp.Compare(freeCPU(b), freeCPU(a))
		case "gpu":
			return cmp.Or(cmp.Compare(b.Free, a.Free), cmp.Compare(b.MIGFree, a.MIGFree))
		case "load":
			return cmp.Compare(b.Node.CPULoad, a.Node.CPULoad)
		}
		return cmp.Or(cmp.Compare(b.Free, a.Free), cmp.Compare(b.MIGFree, a.MIGFree), cmp.Compare(freeCPU(b), freeCPU(a)))
	}
	slices.SortStableFunc(list, func(a, b NodeUsage) int {
		c := key(a, b)
		if desc {
			c = -c
		}
		return cmp.Or(c, strings.Compare(a.Node.Name, b.Node.Name))
	})
}

// NodeStateLabel is a node's state in one short word ("*" if not responding).
func NodeStateLabel(n model.Node) string {
	s := strings.ToLower(n.State)
	switch {
	case n.HasFlag("DRAIN") && (n.State == "IDLE" || n.State == "DOWN"):
		s = "drained"
	case n.HasFlag("DRAIN"):
		s = "draining"
	case n.HasFlag("NOT_RESPONDING"):
		s += "*"
	case n.HasFlag("MAINTENANCE"):
		s = "maint"
	}
	if s == "allocated" {
		s = "alloc"
	}
	return s
}

// reasonStamp is the "[root@2026-09-27T12:00:00]" Slurm appends to a
// node's reason.
var reasonStamp = regexp.MustCompile(`\s*\[[^\]]*@[^\]]*\]\s*$`)

// ShortReason drops the who-and-when stamp from a node's reason.
func ShortReason(r string) string { return reasonStamp.ReplaceAllString(r, "") }

// Load levels.
const (
	LoadOK = iota
	LoadHigh
	LoadOver
)

// LoadLevel rates CPU load: LoadOver above 125% of CPUs, LoadHigh well above allocated CPUs.
func LoadLevel(n model.Node) int {
	switch {
	case n.CPUTotal == 0:
		return LoadOK
	case n.CPULoad > 1.25*float64(n.CPUTotal):
		return LoadOver
	case n.CPULoad > 1.25*float64(n.CPUAlloc)+2:
		return LoadHigh
	}
	return LoadOK
}

// PartSummary is one partition's capacity, from its nodes.
type PartSummary struct {
	Partition                 model.Partition
	Nodes, Idle, Mixed, Alloc int
	Down                      int // nodes that cannot take jobs
	CPUTotal, CPUFree         int
	GPUTotal, GPUFree         int
	MIGTotal, MIGFree         int
	GPUTypes                  []TypeCount // whole GPUs by type: Total and Free
	Pending                   int
	PendingKnown              bool
	DownReasons               []string // "node: reason", sorted
}

// TypeCount is a number of GPUs of one type, and how many are free.
type TypeCount struct {
	Type        string
	Total, Free int
}

// PartitionSummaries adds up each partition's nodes. pending holds the
// pending jobs per partition when known (see PendingByPartition).
func PartitionSummaries(parts []model.Partition, usage []NodeUsage, pending map[string]int, known map[string]bool) []PartSummary {
	byNode := make(map[string]NodeUsage, len(usage))
	for _, u := range usage {
		byNode[u.Node.Name] = u
	}
	out := make([]PartSummary, 0, len(parts))
	for _, p := range parts {
		s := PartSummary{Partition: p, Pending: pending[p.Name], PendingKnown: known[p.Name]}
		types := map[string]*TypeCount{}
		var order []string
		for _, name := range p.Nodes {
			u, ok := byNode[name]
			if !ok {
				continue
			}
			n := u.Node
			s.Nodes++
			s.CPUTotal += n.CPUTotal
			s.GPUTotal += n.GPUTotal
			s.MIGTotal += n.MIGTotal
			if !Available(n) {
				s.Down++
				if n.Reason != "" {
					s.DownReasons = append(s.DownReasons, n.Name+": "+ShortReason(n.Reason))
				}
			} else {
				s.CPUFree += max(n.CPUTotal-n.CPUAlloc, 0)
				s.GPUFree += u.Free
				s.MIGFree += u.MIGFree
				switch n.State {
				case "IDLE":
					s.Idle++
				case "MIXED":
					s.Mixed++
				default:
					s.Alloc++
				}
			}
			for _, g := range n.GPUs {
				if g.MIG {
					continue
				}
				tc, seen := types[g.Type]
				if !seen {
					tc = &TypeCount{Type: g.Type}
					types[g.Type] = tc
					order = append(order, g.Type)
				}
				tc.Total += g.Total
				if Available(n) {
					tc.Free += max(g.Total-g.Alloc, 0)
				}
			}
		}
		for _, t := range order {
			s.GPUTypes = append(s.GPUTypes, *types[t])
		}
		slices.Sort(s.DownReasons)
		out = append(out, s)
	}
	return out
}

// PendingByPartition counts pending jobs per partition (all jobs, else queue-rank);
// known marks partitions whose count is complete.
func PendingByPartition(st *Store) (pending map[string]int, known map[string]bool) {
	pending, known = map[string]int{}, map[string]bool{}
	switch {
	case st.AllJobs.Has:
		for _, j := range st.AllJobs.Data {
			if j.State == model.StatePending {
				for _, p := range strings.Split(j.Partition, ",") {
					pending[p]++
				}
			}
		}
		for _, p := range st.Partitions.Data {
			known[p.Name] = true
		}
	case st.QueueRank.Has:
		for _, q := range st.QueueRank.Data {
			pending[q.Partition]++
			known[q.Partition] = true
		}
	}
	return pending, known
}

// Hidden reports whether every one of a node's partitions is in hide (the
// hide_partitions setting). A node in no partition is never hidden.
func Hidden(hide, parts []string) bool {
	if len(hide) == 0 || len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !slices.Contains(hide, p) {
			return false
		}
	}
	return true
}

// QueueGroups are the ways the queue can be grouped.
var QueueGroups = []string{"state", "user", "partition", "none"}
