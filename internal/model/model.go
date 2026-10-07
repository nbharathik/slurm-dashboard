// Package model defines the domain types shared by parsers, views and the CLI (memory in MB, nil duration = unlimited).
package model

import (
	"math"
	"time"
)

// JobState is a Slurm job state such as "RUNNING" or "OUT_OF_MEMORY".
type JobState string

// Job states sdash treats specially; others render neutrally.
const (
	StatePending     JobState = "PENDING"
	StateRunning     JobState = "RUNNING"
	StateSuspended   JobState = "SUSPENDED"
	StateCompleting  JobState = "COMPLETING"
	StateCompleted   JobState = "COMPLETED"
	StateCancelled   JobState = "CANCELLED"
	StateFailed      JobState = "FAILED"
	StateTimeout     JobState = "TIMEOUT"
	StateNodeFail    JobState = "NODE_FAIL"
	StatePreempted   JobState = "PREEMPTED"
	StateBootFail    JobState = "BOOT_FAIL"
	StateDeadline    JobState = "DEADLINE"
	StateOOM         JobState = "OUT_OF_MEMORY"
	StateRequeued    JobState = "REQUEUED"
	StateConfiguring JobState = "CONFIGURING"
	StateStopped     JobState = "STOPPED"
	StateSignaling   JobState = "SIGNALING"
	StateResizing    JobState = "RESIZING"
)

// IsActive reports whether a job in this state is still in the queue.
func (s JobState) IsActive() bool {
	switch s {
	case StatePending, StateRunning, StateSuspended, StateCompleting, StateConfiguring,
		StateStopped, StateSignaling, StateResizing, StateRequeued:
		return true
	}
	return false
}

// IsFailure reports whether a finished job ended badly.
func (s JobState) IsFailure() bool {
	switch s {
	case StateFailed, StateTimeout, StateNodeFail, StateBootFail, StateOOM, StateDeadline, StatePreempted:
		return true
	}
	return false
}

// JobID is a parsed Slurm job identifier.
type JobID struct {
	Raw        string `json:"raw"`            // as printed, e.g. "812_[3-9%2]"
	ArrayJobID uint64 `json:"array_job_id"`   // base ID (equals the job ID for non-array jobs)
	TaskSpec   string `json:"task,omitempty"` // "", "4", "[3-9%2]"
	HetOffset  int    `json:"het_offset"`     // -1 unless heterogeneous
	Step       string `json:"step,omitempty"` // "", "batch", "extern", "0"
}

// String returns the ID as printed by Slurm.
func (id JobID) String() string { return id.Raw }

// IsArray reports whether the ID belongs to an array job.
func (id JobID) IsArray() bool { return id.TaskSpec != "" }

// Job is one of the user's jobs from squeue.
type Job struct {
	ID           JobID
	Name         string
	User         string
	Account      string
	Partition    string
	QOS          string
	State        JobState
	Reason       string // raw reason; "None" when running
	Dependency   string
	TimeUsed     time.Duration
	TimeLimit    *time.Duration
	TimeLeft     *time.Duration
	SubmitTime   time.Time
	StartTime    time.Time // actual, or scheduler estimate while pending
	EndTime      time.Time
	Nodes        int
	NodeList     []string // expanded
	CPUs         int
	MemPerNodeMB int64 // 0 = whole node
	GPUs         int   // allocated when running (joined from cluster data), else requested
	GPUType      string
	Priority     int64
	QueueRank    int // 0 = unknown or not pending
	QueueTotal   int
}

// JobDetail is the full scontrol view of one job.
type JobDetail struct {
	Job
	WorkDir   string
	StdOut    string
	StdErr    string
	Command   string
	ExitCode  string
	AllocTRES map[string]string
	ReqTRES   map[string]string
	Raw       map[string]string // every scontrol key, for the "all fields" view
	RawOrder  []string          // keys in the order scontrol printed them
}

// JobStat is live usage from sstat, for running jobs only.
type JobStat struct {
	NTasks   int
	MaxRSSMB int64
	AveRSSMB int64
	AveCPU   time.Duration
	// TotalCPU is the CPU time so far: per-step average times tasks, summed.
	TotalCPU time.Duration
	// GPUUtil is GPU utilisation (0..1) when the site records gres/gpuutil (see HasGPUUtil).
	GPUUtil    float64
	HasGPUUtil bool
	At         time.Time
}

// RunningJob is a running job of any user, from the cluster-wide query.
type RunningJob struct {
	ID        JobID
	User      string
	Partition string
	NodeList  []string
	EndTime   time.Time
	GPUs      int   // total across nodes, whole GPUs and MIG slices
	MIGSlices int   // how many of GPUs are MIG slices
	CPUs      int   // total across nodes
	MemMB     int64 // total across nodes; 0 when not tracked
}

// Node is one compute node.
type Node struct {
	Name       string
	State      string   // base state, e.g. "MIXED"
	Flags      []string // DRAIN, NOT_RESPONDING, ...
	Reason     string
	Partitions []string
	CPUTotal   int
	CPUAlloc   int
	CPULoad    float64
	MemTotalMB int64
	MemAllocMB int64
	MemFreeMB  int64 // -1 when unknown
	GPUTotal   int   // whole GPUs; MIG slices are counted in MIGTotal
	GPUAlloc   int
	GPUType    string // whole-GPU types as Slurm names them, comma-separated
	MIGTotal   int    // MIG slices
	MIGAlloc   int
	GPUs       []GPUGroup // per type, whole GPUs first
	Features   []string
	BootTime   time.Time
}

// GPUGroup is the GPUs of one type on a node.
type GPUGroup struct {
	Type  string // as Slurm names it, e.g. "nvidia_h200_nvl" or "1g.33gb"; "" when untyped
	Total int
	Alloc int
	MIG   bool // a MIG slice profile rather than a whole GPU
}

// HasGPUs reports whether the node has whole GPUs or MIG slices.
func (n Node) HasGPUs() bool { return n.GPUTotal > 0 || n.MIGTotal > 0 }

// MemTracked reports whether Slurm tracks the node's memory (running jobs with none allocated means untracked).
func (n Node) MemTracked() bool { return n.MemAllocMB != 0 || n.CPUAlloc == 0 }

// HasFlag reports whether the node carries flag (e.g. "DRAIN").
func (n Node) HasFlag(flag string) bool {
	for _, f := range n.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// Partition is one Slurm partition.
type Partition struct {
	Name      string
	State     string
	Default   bool
	MaxTime   *time.Duration
	Nodes     []string
	TotalCPUs int
	TotalGPUs int
}

// Reservation is one Slurm reservation.
type Reservation struct {
	Name     string
	Start    time.Time
	End      time.Time
	Nodes    []string
	Flags    []string // MAINT, ...
	Users    []string
	Accounts []string
	State    string
}

// IsMaintenance reports whether the reservation has the MAINT flag.
func (r Reservation) IsMaintenance() bool {
	for _, f := range r.Flags {
		if f == "MAINT" {
			return true
		}
	}
	return false
}

// HistoryJob is a job from sacct, with its steps folded in.
type HistoryJob struct {
	ID             JobID
	Name           string
	Partition      string
	State          JobState
	CancelledByUID string
	ExitCode       int
	Signal         int
	Submit         time.Time
	Start          time.Time
	End            time.Time
	Elapsed        time.Duration
	TimeLimit      *time.Duration
	AllocCPUs      int
	TotalCPU       time.Duration
	AllocMemMB     int64
	PeakMemMB      int64 // estimated, from steps
	GPUs           int
	GPUType        string // raw GPU type(s) from the allocation, "" when untyped
	Nodes          int
	NodeList       []string
	WorkDir        string
	Eff            Efficiency
}

// Efficiency describes how well a finished job used what it asked for.
type Efficiency struct {
	CPU      float64 // 0..1; -1 = unknown
	Mem      float64 // 0..1; -1 = unknown
	Time     float64 // 0..1; -1 = unknown
	GPUHours float64
	GPUUtil  float64 // -1 when not accounted
}

// Share is one fairshare association.
type Share struct {
	Account        string
	User           string
	RawShares      int64
	NormShares     float64
	RawUsage       int64
	EffectiveUsage float64
	FairShare      float64 // 0..1
}

// PriorityFactors is one row of sprio.
type PriorityFactors struct {
	JobID     string
	Priority  int64
	Age       int64
	FairShare int64
	JobSize   int64
	Partition int64
	QOS       int64
}

// Limit is one association or QOS limit; Max and Used are in Unit, Used is -1 if unreported.
type Limit struct {
	Name string  // "GrpTRES cpu", "MaxJobs", "MaxWall"
	Unit string  // "" (a count), "min" or "MB"
	Max  float64 // the limit
	Used float64 // current usage, -1 when unknown
}

// LimitScope is the set limits of one association or QOS.
type LimitScope struct {
	Kind   string // "user", "account" or "qos"
	Name   string // "research", "normal"
	Limits []Limit
}

// Quota is one storage location's usage.
type Quota struct {
	Label             string
	Path              string
	FSType            string
	Backend           string
	UsedBytes         int64 // 0 = unknown or none
	SoftBytes         int64
	HardBytes         int64
	UsedFiles         int64
	SoftFiles         int64
	HardFiles         int64
	Grace             string
	Note              string
	Raw               string // backend output, for the detail view
	AvailableBytes    int64
	FilesystemBytes   int64
	AvailabilityKnown bool
	IsFilesystemTotal bool   // statfs fallback, not a personal quota
	Filesystem        *Quota `json:"-"`
	Err               string // last error, if the value is stale
	At                time.Time
}

// QuotaUsage is how full a location is, in whole percent rounded down; computed once so all views agree.
type QuotaUsage struct {
	BlocksPct int    // space used against the soft limit (else hard); -1 without a limit
	FilesPct  int    // files used against the soft limit (else hard); -1 without a limit
	Pct       int    // the fuller of the two; -1 when neither has a limit
	What      string // "space" or "files": which one Pct is
}

// Usage returns how full q is.
func (q Quota) Usage() QuotaUsage {
	pct := func(used, soft, hard int64) int {
		limit := soft
		if limit <= 0 {
			limit = hard
		}
		if limit <= 0 {
			return -1
		}
		return int(math.Floor(float64(used) * 100 / float64(limit)))
	}
	u := QuotaUsage{BlocksPct: pct(q.UsedBytes, q.SoftBytes, q.HardBytes), FilesPct: pct(q.UsedFiles, q.SoftFiles, q.HardFiles), What: "space"}
	u.Pct = u.BlocksPct
	if u.FilesPct > u.Pct {
		u.Pct, u.What = u.FilesPct, "files"
	}
	return u
}

// DiskUsage is the result of the disk-usage analyser for one directory.
type DiskUsage struct {
	Path    string
	Total   int64 // bytes
	Entries []DiskEntry
	At      time.Time
	Took    time.Duration
	Partial bool   // du reported unreadable directories
	Err     string // the scan failed or was cancelled
	Running bool
}

// DiskEntry is one subdirectory's size.
type DiskEntry struct {
	Path  string
	Bytes int64
}

// Level is an alert severity.
type Level int

// Alert levels.
const (
	Info Level = iota
	Warn
	Crit
)

func (l Level) String() string {
	switch l {
	case Crit:
		return "crit"
	case Warn:
		return "warn"
	}
	return "info"
}

// Alert is something the Overview should point out.
type Alert struct {
	Level   Level
	Kind    string // "storage", "job-failed", "unsatisfiable", "timelimit", "maintenance", "node", "low-eff"
	Key     string // stable identity, for dismissing
	Message string
	JobID   string // optional; enables "jump to job"
	Tab     string // optional tab to jump to
}

// Capabilities describe what the installed Slurm supports.
type Capabilities struct {
	Version    string `json:"version"`
	Major      int    `json:"major"`
	Minor      int    `json:"minor"`
	Patch      int    `json:"patch"`
	HasMe      bool   `json:"has_me"`
	HasOverlap bool   `json:"has_overlap"`
	HasSstat   bool   `json:"has_sstat"`
	HasSshare  bool   `json:"has_sshare"`
	HasSprio   bool   `json:"has_sprio"`
	HasSacct   bool   `json:"has_sacct"`
}

// AtLeast reports whether the Slurm version is at least major.minor.
func (c Capabilities) AtLeast(major, minor int) bool {
	return c.Major > major || (c.Major == major && c.Minor >= minor)
}

// ParseWarning describes input a parser skipped.
type ParseWarning struct {
	Source string `json:"source"`
	Line   int    `json:"line"` // 1-based; 0 when not line-based
	Raw    string `json:"raw"`
	Msg    string `json:"msg"`
}

// ComputeEfficiency fills Eff: CPU = TotalCPU/(Elapsed×AllocCPUs), memory = PeakMem/AllocMem,
// time = Elapsed/TimeLimit. gpuUtil is 0..1, or -1 when not accounted.
func (h *HistoryJob) ComputeEfficiency(gpuUtil float64) {
	h.Eff = Efficiency{CPU: -1, Mem: -1, Time: -1, GPUUtil: gpuUtil}
	elapsed := h.Elapsed.Seconds()
	if elapsed > 0 && h.AllocCPUs > 0 {
		h.Eff.CPU = h.TotalCPU.Seconds() / (elapsed * float64(h.AllocCPUs))
	}
	if h.AllocMemMB > 0 && h.PeakMemMB > 0 {
		h.Eff.Mem = float64(h.PeakMemMB) / float64(h.AllocMemMB)
	}
	if h.TimeLimit != nil && *h.TimeLimit > 0 {
		h.Eff.Time = elapsed / h.TimeLimit.Seconds()
	}
	h.Eff.GPUHours = float64(h.GPUs) * elapsed / 3600
}

// OwnedBy fails closed when either identity is unknown.
func (j Job) OwnedBy(user string) bool { return user != "" && j.User != "" && j.User == user }
