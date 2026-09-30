package report

import (
	"cmp"
	"slices"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// Schema is the version of every JSON document.
const Schema = 1

// Header is common to every document.
type Header struct {
	Schema      int       `json:"schema"`
	Cluster     string    `json:"cluster,omitempty"`
	User        string    `json:"user,omitempty"`
	Slurm       string    `json:"slurm,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}

// NewHeader fills a header from the store.
func NewHeader(s *state.Store, now time.Time) Header {
	return Header{Schema: Schema, Cluster: s.ClusterName, User: s.User, Slurm: s.Caps.Version, GeneratedAt: now.UTC()}
}

// Job is one job in JSON form.
type Job struct {
	ID         string     `json:"id"`
	ArrayJobID uint64     `json:"array_job_id"`
	Task       string     `json:"array_task,omitempty"`
	Name       string     `json:"name"`
	User       string     `json:"user,omitempty"`
	Account    string     `json:"account,omitempty"`
	Partition  string     `json:"partition"`
	QOS        string     `json:"qos,omitempty"`
	State      string     `json:"state"`
	Reason     string     `json:"reason,omitempty"`
	TimeUsedS  int64      `json:"time_used_s"`
	TimeLimitS *int64     `json:"time_limit_s"`
	TimeLeftS  *int64     `json:"time_left_s"`
	Submit     *time.Time `json:"submit_time"`
	Start      *time.Time `json:"start_time"`
	End        *time.Time `json:"end_time"`
	Nodes      int        `json:"nodes"`
	NodeList   []string   `json:"node_list"`
	CPUs       int        `json:"cpus"`
	MemMB      int64      `json:"mem_mb"`
	GPUs       int        `json:"gpus"`
	GPUType    string     `json:"gpu_type,omitempty"`
	Priority   int64      `json:"priority"`
	QueueRank  int        `json:"queue_rank,omitempty"`
	QueueTotal int        `json:"queue_total,omitempty"`
	Dependency string     `json:"dependency,omitempty"`
}

// FromJob converts a model job.
func FromJob(j model.Job) Job {
	nodes := j.NodeList
	if nodes == nil {
		nodes = []string{}
	}
	return Job{
		ID: j.ID.Raw, ArrayJobID: j.ID.ArrayJobID, Task: j.ID.TaskSpec, Name: j.Name, User: j.User,
		Account: j.Account, Partition: j.Partition, QOS: j.QOS, State: string(j.State), Reason: j.Reason,
		TimeUsedS: int64(j.TimeUsed / time.Second), TimeLimitS: secs(j.TimeLimit), TimeLeftS: secs(j.TimeLeft),
		Submit: tp(j.SubmitTime), Start: tp(j.StartTime), End: tp(j.EndTime),
		Nodes: j.Nodes, NodeList: nodes, CPUs: j.CPUs, MemMB: j.MemPerNodeMB, GPUs: j.GPUs, GPUType: j.GPUType,
		Priority: j.Priority, QueueRank: j.QueueRank, QueueTotal: j.QueueTotal, Dependency: j.Dependency,
	}
}

// JobsDoc is the output of "sdash jobs --json".
type JobsDoc struct {
	Header
	Scope string `json:"scope"`
	Jobs  []Job  `json:"jobs"`
}

// Jobs builds the jobs document.
func Jobs(h Header, scope string, jobs []model.Job) JobsDoc {
	out := JobsDoc{Header: h, Scope: scope, Jobs: make([]Job, 0, len(jobs))}
	for _, j := range SortJobs(jobs) {
		out.Jobs = append(out.Jobs, FromJob(j))
	}
	return out
}

// SortJobs orders jobs the way the Jobs tab does by default: running
// first, then pending by queue rank, then the rest by submit time.
func SortJobs(jobs []model.Job) []model.Job {
	out := slices.Clone(jobs)
	group := func(j model.Job) int {
		switch j.State {
		case model.StateRunning, model.StateCompleting, model.StateConfiguring:
			return 0
		case model.StatePending:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(out, func(a, b model.Job) int {
		if ga, gb := group(a), group(b); ga != gb {
			return ga - gb
		}
		if a.State == model.StatePending && b.State == model.StatePending {
			ra, rb := a.QueueRank, b.QueueRank
			if ra == 0 {
				ra = 1 << 30
			}
			if rb == 0 {
				rb = 1 << 30
			}
			if ra != rb {
				return ra - rb
			}
		}
		if c := a.SubmitTime.Compare(b.SubmitTime); c != 0 {
			return c
		}
		return cmp.Compare(a.ID.ArrayJobID, b.ID.ArrayJobID)
	})
	return out
}

// GPUNode is one node's GPU availability.
type GPUNode struct {
	Name       string         `json:"name"`
	State      string         `json:"state"`
	Flags      []string       `json:"flags"`
	Partitions []string       `json:"partitions"`
	GPUType    string         `json:"gpu_type,omitempty"`         // as Slurm names it
	GPUDisplay string         `json:"gpu_type_display,omitempty"` // short name, see [gpu] aliases
	Total      int            `json:"gpu_total"`                  // whole GPUs; MIG slices are in mig_total
	Allocated  int            `json:"gpu_allocated"`
	Free       int            `json:"gpu_free"`
	Mine       int            `json:"gpu_mine"`
	Others     int            `json:"gpu_others"`
	MIGTotal   int            `json:"mig_total"`
	MIGAlloc   int            `json:"mig_allocated"`
	MIGFree    int            `json:"mig_free"`
	Groups     []GPUGroup     `json:"groups"`
	Users      map[string]int `json:"users"`
	FreeBy     *time.Time     `json:"free_by"`
	Estimate   bool           `json:"estimate"`
	Reason     string         `json:"reason,omitempty"`
}

// GPUGroup is the GPUs of one type on a node.
type GPUGroup struct {
	Type      string `json:"type"`
	Display   string `json:"type_display"`
	MIG       bool   `json:"mig"`
	Total     int    `json:"total"`
	Allocated int    `json:"allocated"`
}

// GPUsDoc is the output of "sdash gpus --json" (hidden; nodes --json has the same nodes).
type GPUsDoc struct {
	Header
	Total    int       `json:"gpu_total"`
	Free     int       `json:"gpu_free"`
	MIGTotal int       `json:"mig_total"`
	MIGFree  int       `json:"mig_free"`
	Nodes    []GPUNode `json:"nodes"`
}

// GPUs builds the GPU document: GPU nodes only, or every node with
// allNodes.
func GPUs(h Header, usage []state.NodeUsage, allNodes bool, aliases map[string]string) GPUsDoc {
	doc := GPUsDoc{Header: h, Nodes: []GPUNode{}}
	nodes := usage
	if !allNodes {
		nodes = state.GPUNodes(usage)
	}
	nodes = slices.Clone(nodes)
	state.SortNodes(nodes, "free", false)
	for _, u := range nodes {
		users := map[string]int{}
		for k, v := range u.Users {
			users[k] += v
		}
		flags := u.Node.Flags
		if flags == nil {
			flags = []string{}
		}
		parts := u.Node.Partitions
		if parts == nil {
			parts = []string{}
		}
		groups := []GPUGroup{}
		for _, g := range u.Node.GPUs {
			groups = append(groups, GPUGroup{Type: g.Type, Display: units.GPUDisplayName(g.Type, aliases), MIG: g.MIG, Total: g.Total, Allocated: g.Alloc})
		}
		doc.Nodes = append(doc.Nodes, GPUNode{
			Name: u.Node.Name, State: u.Node.State, Flags: flags, Partitions: parts,
			GPUType: u.Node.GPUType, GPUDisplay: units.GPUDisplayNames(u.Node.GPUType, aliases),
			Total: u.Node.GPUTotal, Allocated: u.Node.GPUAlloc, Free: u.Free, Mine: u.Mine, Others: u.Others,
			MIGTotal: u.Node.MIGTotal, MIGAlloc: u.Node.MIGAlloc, MIGFree: u.MIGFree, Groups: groups,
			Users: users, FreeBy: tp(u.FreeBy), Estimate: u.Estimate, Reason: u.Node.Reason,
		})
		doc.Total += u.Node.GPUTotal
		doc.Free += u.Free
		doc.MIGTotal += u.Node.MIGTotal
		doc.MIGFree += u.MIGFree
	}
	return doc
}

// Alert is an alert in JSON form.
type Alert struct {
	Level   string `json:"level"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
	JobID   string `json:"job_id,omitempty"`
}

// Quota is a storage location in JSON form.
type Quota struct {
	Label             string     `json:"label"`
	Path              string     `json:"path"`
	FSType            string     `json:"fs_type,omitempty"`
	Backend           string     `json:"backend"`
	UsedBytes         int64      `json:"used_bytes"`
	SoftBytes         int64      `json:"soft_bytes"`
	HardBytes         int64      `json:"hard_bytes"`
	UsedFiles         int64      `json:"used_files"`
	SoftFiles         int64      `json:"soft_files"`
	HardFiles         int64      `json:"hard_files"`
	UsedPct           int        `json:"used_pct"`  // space, whole percent rounded down; -1 without a limit
	FilesPct          int        `json:"files_pct"` // files, likewise
	Grace             string     `json:"grace,omitempty"`
	Note              string     `json:"note,omitempty"`
	IsFilesystemTotal bool       `json:"is_filesystem_total"`
	Error             string     `json:"error,omitempty"`
	At                *time.Time `json:"at"`
}

// FromQuota converts a model quota.
func FromQuota(q model.Quota) Quota {
	return Quota{
		Label: q.Label, Path: q.Path, FSType: q.FSType, Backend: q.Backend,
		UsedBytes: q.UsedBytes, SoftBytes: q.SoftBytes, HardBytes: q.HardBytes,
		UsedFiles: q.UsedFiles, SoftFiles: q.SoftFiles, HardFiles: q.HardFiles,
		UsedPct: q.Usage().BlocksPct, FilesPct: q.Usage().FilesPct,
		Grace: q.Grace, Note: q.Note, IsFilesystemTotal: q.IsFilesystemTotal, Error: q.Err, At: tp(q.At),
	}
}

// StatusDoc is the output of "sdash status --json".
type StatusDoc struct {
	Header
	Jobs struct {
		Running int `json:"running"`
		Pending int `json:"pending"`
		Other   int `json:"other"`
	} `json:"jobs"`
	GPUs struct {
		Total         int `json:"total"` // whole GPUs
		Free          int `json:"free"`
		MIGTotal      int `json:"mig_total"` // MIG slices, counted apart
		MIGFree       int `json:"mig_free"`
		NodesWithFree int `json:"nodes_with_free"`
	} `json:"gpus"`
	Alerts  []Alert  `json:"alerts"`
	Storage []Quota  `json:"storage"`
	Errors  []string `json:"errors"`
}

// FromAlert converts a model alert.
func FromAlert(a model.Alert) Alert {
	return Alert{Level: a.Level.String(), Kind: a.Kind, Message: a.Message, JobID: a.JobID}
}

func secs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	v := int64(*d / time.Second)
	return &v
}

func tp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t
	return &u
}

// QuotaDoc is "sdash quota --json".
type QuotaDoc struct {
	Header
	Locations []Quota `json:"locations"`
}

// WhyDoc is "sdash why --json".
type WhyDoc struct {
	Header
	Job            Job        `json:"job"`
	Code           string     `json:"reason_code"`
	Explanation    string     `json:"explanation"`
	Action         string     `json:"action,omitempty"`
	NeverStarts    bool       `json:"never_starts"`
	KnownReason    bool       `json:"known_reason"`
	EstimatedStart *time.Time `json:"estimated_start"`
	Priority       string     `json:"priority,omitempty"`
}

// Efficiency is one finished job's efficiency in JSON form; fractions are
// 0..1, or null when unknown.
type Efficiency struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	State       string       `json:"state"`
	ExitCode    int          `json:"exit_code"`
	Signal      int          `json:"signal"`
	ElapsedS    int64        `json:"elapsed_s"`
	TimeLimitS  *int64       `json:"time_limit_s"`
	CPUs        int          `json:"cpus"`
	CPUEff      *float64     `json:"cpu_efficiency"`
	MemEff      *float64     `json:"mem_efficiency"`
	TimeEff     *float64     `json:"time_efficiency"`
	PeakMemMB   int64        `json:"peak_mem_mb"`
	AllocMemMB  int64        `json:"alloc_mem_mb"`
	GPUs        int          `json:"gpus"`
	GPUHours    float64      `json:"gpu_hours"`
	GPUUtil     *float64     `json:"gpu_utilisation"`
	End         *time.Time   `json:"end_time"`
	Hint        string       `json:"hint,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

// Suggestion is a right-sizing proposal.
type Suggestion struct {
	What   string `json:"what"`
	Reason string `json:"reason"`
	Line   string `json:"sbatch,omitempty"`
}

// EffDoc is "sdash history --json".
type EffDoc struct {
	Header
	Days int          `json:"days,omitempty"`
	Jobs []Efficiency `json:"jobs"`
}

func frac(f float64) *float64 {
	if f < 0 {
		return nil
	}
	return &f
}

// FromHistoryJob converts a finished job; hint and suggestions come from
// the explain package.
func FromHistoryJob(h model.HistoryJob, hint string, sugg []Suggestion) Efficiency {
	return Efficiency{
		ID: h.ID.Raw, Name: h.Name, State: string(h.State), ExitCode: h.ExitCode, Signal: h.Signal,
		ElapsedS: int64(h.Elapsed / time.Second), TimeLimitS: secs(h.TimeLimit), CPUs: h.AllocCPUs,
		CPUEff: frac(h.Eff.CPU), MemEff: frac(h.Eff.Mem), TimeEff: frac(h.Eff.Time),
		PeakMemMB: h.PeakMemMB, AllocMemMB: h.AllocMemMB, GPUs: h.GPUs, GPUHours: h.Eff.GPUHours,
		GPUUtil: frac(h.Eff.GPUUtil), End: tp(h.End), Hint: hint, Suggestions: sugg,
	}
}
