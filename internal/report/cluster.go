package report

import (
	"slices"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// Node is one node for "sdash nodes --json".
type Node struct {
	Name       string         `json:"name"`
	State      string         `json:"state"` // idle, mixed, alloc, drained, draining, down, ...
	Flags      []string       `json:"flags"`
	Reason     string         `json:"reason,omitempty"`
	Partitions []string       `json:"partitions"`
	Features   []string       `json:"features"`
	CPUTotal   int            `json:"cpu_total"`
	CPUAlloc   int            `json:"cpu_allocated"`
	CPULoad    float64        `json:"cpu_load"`
	MemTotalMB int64          `json:"mem_total_mb"`
	MemAllocMB *int64         `json:"mem_allocated_mb"` // null when Slurm does not track memory
	MemFreeMB  *int64         `json:"mem_os_free_mb"`   // what the OS reports, null when unknown
	GPUTotal   int            `json:"gpu_total"`        // whole GPUs
	GPUAlloc   int            `json:"gpu_allocated"`
	GPUFree    int            `json:"gpu_free"`
	MIGTotal   int            `json:"mig_total"`
	MIGAlloc   int            `json:"mig_allocated"`
	MIGFree    int            `json:"mig_free"`
	GPUDisplay string         `json:"gpu_type_display,omitempty"`
	Groups     []GPUGroup     `json:"groups"`
	Jobs       int            `json:"jobs"`
	Users      map[string]int `json:"users"` // running jobs per user; "you" for yours
	BootTime   *time.Time     `json:"boot_time"`
}

// NodesDoc is the output of "sdash nodes --json".
type NodesDoc struct {
	Header
	Nodes []Node `json:"nodes"`
}

// Nodes builds the nodes document.
func Nodes(h Header, usage []state.NodeUsage, me string, aliases map[string]string) NodesDoc {
	doc := NodesDoc{Header: h, Nodes: []Node{}}
	for _, u := range usage {
		n := u.Node
		out := Node{
			Name: n.Name, State: state.NodeStateLabel(n), Flags: orEmpty(n.Flags), Reason: state.ShortReason(n.Reason),
			Partitions: orEmpty(n.Partitions), Features: orEmpty(n.Features),
			CPUTotal: n.CPUTotal, CPUAlloc: n.CPUAlloc, CPULoad: n.CPULoad, MemTotalMB: n.MemTotalMB,
			GPUTotal: n.GPUTotal, GPUAlloc: n.GPUAlloc, GPUFree: u.Free, MIGTotal: n.MIGTotal, MIGAlloc: n.MIGAlloc, MIGFree: u.MIGFree,
			GPUDisplay: units.GPUDisplayNames(n.GPUType, aliases), Groups: []GPUGroup{}, Jobs: len(u.Jobs),
			Users: map[string]int{}, BootTime: tp(n.BootTime),
		}
		if n.MemTracked() {
			out.MemAllocMB = &n.MemAllocMB
		}
		if n.MemFreeMB >= 0 {
			out.MemFreeMB = &n.MemFreeMB
		}
		for _, g := range n.GPUs {
			out.Groups = append(out.Groups, GPUGroup{Type: g.Type, Display: units.GPUDisplayName(g.Type, aliases), MIG: g.MIG, Total: g.Total, Allocated: g.Alloc})
		}
		for _, j := range u.Jobs {
			who := j.User
			if who == me {
				who = "you"
			}
			out.Users[who]++
		}
		doc.Nodes = append(doc.Nodes, out)
	}
	return doc
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Partition is one partition for "sdash nodes --partitions --json".
type Partition struct {
	Name        string      `json:"name"`
	State       string      `json:"state"`
	Default     bool        `json:"default"`
	MaxTimeS    *int64      `json:"max_time_s"` // null = no limit
	Nodes       int         `json:"nodes"`
	Idle        int         `json:"nodes_idle"`
	Mixed       int         `json:"nodes_mixed"`
	Alloc       int         `json:"nodes_allocated"`
	Down        int         `json:"nodes_down"`
	CPUTotal    int         `json:"cpu_total"`
	CPUFree     int         `json:"cpu_free"`
	GPUTotal    int         `json:"gpu_total"`
	GPUFree     int         `json:"gpu_free"`
	MIGTotal    int         `json:"mig_total"`
	MIGFree     int         `json:"mig_free"`
	GPUTypes    []TypeCount `json:"gpu_types"`
	Pending     *int        `json:"pending"` // null when unknown
	DownReasons []string    `json:"down_reasons"`
}

// TypeCount is the GPUs of one type in a partition.
type TypeCount struct {
	Type    string `json:"type"`
	Display string `json:"type_display"`
	Total   int    `json:"total"`
	Free    int    `json:"free"`
}

// PartitionsDoc is the output of "sdash partitions --json".
type PartitionsDoc struct {
	Header
	Partitions []Partition `json:"partitions"`
}

// Partitions builds the partitions document.
func Partitions(h Header, sums []state.PartSummary, aliases map[string]string) PartitionsDoc {
	doc := PartitionsDoc{Header: h, Partitions: []Partition{}}
	for _, s := range sums {
		p := Partition{
			Name: s.Partition.Name, State: s.Partition.State, Default: s.Partition.Default, MaxTimeS: secs(s.Partition.MaxTime),
			Nodes: s.Nodes, Idle: s.Idle, Mixed: s.Mixed, Alloc: s.Alloc, Down: s.Down,
			CPUTotal: s.CPUTotal, CPUFree: s.CPUFree, GPUTotal: s.GPUTotal, GPUFree: s.GPUFree,
			MIGTotal: s.MIGTotal, MIGFree: s.MIGFree, GPUTypes: []TypeCount{}, DownReasons: orEmpty(s.DownReasons),
		}
		for _, t := range s.GPUTypes {
			p.GPUTypes = append(p.GPUTypes, TypeCount{Type: t.Type, Display: units.GPUDisplayName(t.Type, aliases), Total: t.Total, Free: t.Free})
		}
		if s.PendingKnown {
			n := s.Pending
			p.Pending = &n
		}
		doc.Partitions = append(doc.Partitions, p)
	}
	return doc
}

// HistoryDoc is the output of "sdash history --json".
type HistoryDoc struct {
	Header
	Days int          `json:"days"`
	Jobs []Efficiency `json:"jobs"`
}

// History builds the history document, newest first.
func History(h Header, days int, jobs []model.HistoryJob, uid string) HistoryDoc {
	doc := HistoryDoc{Header: h, Days: days, Jobs: []Efficiency{}}
	sorted := slices.Clone(jobs)
	slices.SortStableFunc(sorted, func(a, b model.HistoryJob) int { return b.End.Compare(a.End) })
	for _, j := range sorted {
		doc.Jobs = append(doc.Jobs, Eff(j, uid))
	}
	return doc
}

// FitNode is a node in "sdash free --json".
type FitNode struct {
	Node       string     `json:"node"`
	Partitions []string   `json:"partitions"`
	FreeCPUs   int        `json:"free_cpus"`
	FreeGPUs   int        `json:"free_gpus"`
	FreeMemMB  *int64     `json:"free_mem_mb"` // null when Slurm does not track memory
	At         *time.Time `json:"at,omitempty"`
}

// FreeDoc is the output of "sdash free --json".
type FreeDoc struct {
	Header
	Request struct {
		GPUs      int    `json:"gpus"`
		GPUType   string `json:"gpu_type,omitempty"`
		CPUs      int    `json:"cpus"`
		MemMB     int64  `json:"mem_mb"`
		TimeS     int64  `json:"time_s"`
		Nodes     int    `json:"nodes"`
		Partition string `json:"partition,omitempty"`
	} `json:"request"`
	Now      []FitNode `json:"now"`
	Soonest  *FitNode  `json:"soonest"`
	Reasons  []string  `json:"reasons"`
	Note     string    `json:"note"`
	TestOnly *TestOnly `json:"test_only,omitempty"`
}

// TestOnly is the scheduler's answer to "sbatch --test-only".
type TestOnly struct {
	Start     *time.Time `json:"start,omitempty"`
	Nodes     string     `json:"nodes,omitempty"`
	Partition string     `json:"partition,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// FreeNote says what "sdash free" leaves out.
const FreeNote = "Based on running jobs' time limits; jobs waiting ahead of yours are not counted."

// Free builds the "where can I run?" document.
func Free(h Header, req state.Request, res state.FitResult) FreeDoc {
	doc := FreeDoc{Header: h, Now: []FitNode{}, Reasons: orEmpty(res.Reasons), Note: FreeNote}
	r := &doc.Request
	r.GPUs, r.GPUType, r.CPUs, r.MemMB, r.TimeS, r.Nodes, r.Partition = req.GPUs, req.GPUType, req.CPUs, req.MemMB, int64(req.Time.Seconds()), max(req.Nodes, 1), req.Partition
	conv := func(f state.Fit) FitNode {
		n := FitNode{Node: f.Node, Partitions: f.Partitions, FreeCPUs: f.FreeCPUs, FreeGPUs: f.FreeGPUs, At: tp(f.At)}
		if f.FreeMemMB >= 0 {
			m := f.FreeMemMB
			n.FreeMemMB = &m
		}
		return n
	}
	for _, f := range res.Now {
		doc.Now = append(doc.Now, conv(f))
	}
	if res.Soonest != nil {
		s := conv(*res.Soonest)
		doc.Soonest = &s
	}
	return doc
}
