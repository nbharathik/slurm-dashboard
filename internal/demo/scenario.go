// Package demo simulates a Slurm cluster for "sdash --demo". Its runner
// answers the same commands sdash runs on a real cluster with real
// Slurm-format text, so demo mode exercises the real parsers, collectors
// and safety checks. Time moves jobs through a fixed script that loops.
package demo

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	demodata "github.com/nbharathik/slurm-dashboard/demo"
)

// Scenario is a scripted cluster (demo/*.json).
type Scenario struct {
	Cluster      string `json:"cluster"`
	Version      string `json:"version"`
	User         string `json:"user"`
	UID          int    `json:"uid"`
	Account      string `json:"account"`
	Loop         Dur    `json:"loop"`
	Home         string `json:"home"`
	MemUntracked bool   `json:"mem_untracked"` // nodes report AllocMem=0, as without CR_*_Memory
	PrivateData  string `json:"private_data"`  // e.g. "jobs": other users' jobs are hidden
	NoAccounting bool   `json:"no_accounting"` // sacct, sshare and sprio fail as without slurmdbd
	// A site can lack these one by one; each shows up in "scontrol show
	// config" and fails the matching command.
	Priority      string        `json:"priority"`        // "basic": priority/basic, no sshare or sprio
	NoUsageGather bool          `json:"no_usage_gather"` // JobAcctGatherType=none: sstat has no data
	NoJobScripts  bool          `json:"no_job_scripts"`  // AccountingStoreFlags lacks job_script
	Nodes         []Node        `json:"nodes"`
	Partitions    []Partition   `json:"partitions"`
	Reservations  []Reservation `json:"reservations"`
	Jobs          []Job         `json:"jobs"`
	History       []Past        `json:"history"`
	Storage       []Quota       `json:"storage"`
	Fairshare     Fairshare     `json:"fairshare"`
	Limits        *Limits       `json:"limits"` // account, user and QOS limits; nil = none set
}

// Limits are the association and QOS limits of the demo user; zero means
// unset. Usage is computed from the simulated queue.
type Limits struct {
	AccountCPUs int `json:"account_cpus"` // GrpTRES cpu of the account
	UserCPUs    int `json:"user_cpus"`    // GrpTRES cpu of the user
	UserGPUs    int `json:"user_gpus"`    // GrpTRES gres/gpu of the user
	MaxJobs     int `json:"max_jobs"`
	MaxSubmit   int `json:"max_submit"`
	MaxWallMin  int `json:"max_wall_min"` // MaxWallPJ of the user
	QOSWallMin  int `json:"qos_wall_min"` // MaxWallPJ of QOS "normal"
}

// Node is a compute node.
type Node struct {
	Name       string   `json:"name"`
	CPUs       int      `json:"cpus"`
	MemGB      int      `json:"mem_gb"`
	GPUs       int      `json:"gpus"`
	GPUType    string   `json:"gpu_type"`
	MIG        []Gres   `json:"mig"` // MIG slice profiles, e.g. {"type": "1g.33gb", "count": 4}
	Partitions []string `json:"partitions"`
	Features   []string `json:"features"`
	Drain      string   `json:"drain"`       // drain reason; empty = healthy
	LoadFactor float64  `json:"load_factor"` // CPU load per allocated CPU (default 0.83)
}

// Gres is a number of GPUs of one type.
type Gres struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// Partition is a partition.
type Partition struct {
	Name    string `json:"name"`
	Nodes   string `json:"nodes"` // hostlist
	MaxTime string `json:"max_time"`
	Default bool   `json:"default"`
}

// Reservation starts StartIn after the scenario starts.
type Reservation struct {
	Name     string   `json:"name"`
	StartIn  Dur      `json:"start_in"`
	Duration Dur      `json:"duration"`
	Nodes    string   `json:"nodes"`
	Flags    []string `json:"flags"`
}

// Job is a queued job. Times are offsets into the scenario loop.
type Job struct {
	ID         int      `json:"id"`
	User       string   `json:"user"`
	Name       string   `json:"name"`
	Partition  string   `json:"partition"`
	Account    string   `json:"account"`
	QOS        string   `json:"qos"`
	CPUs       int      `json:"cpus"`
	MemGB      float64  `json:"mem_gb"`
	GPUs       int      `json:"gpus"`
	GPUType    string   `json:"gpu_type"` // GPU or MIG profile; empty = the node's type
	Nodes      []string `json:"nodes"`
	Limit      string   `json:"limit"`
	RunBefore  Dur      `json:"run_before"`  // running this long when the loop starts
	EndAt      Dur      `json:"end_at"`      // leaves the queue at this offset
	Final      string   `json:"final"`       // final state
	Exit       string   `json:"exit"`        // final exit code:signal
	Reason     string   `json:"reason"`      // pending reason
	Priority   int      `json:"priority"`    // pending priority
	SubmitAgo  Dur      `json:"submit_ago"`  // before the loop starts
	EstStart   Dur      `json:"est_start"`   // scheduler's start estimate, from the loop start
	Array      string   `json:"array"`       // task range, e.g. "3-9"
	ArrayStart Dur      `json:"array_start"` // the first task starts here
	WorkDir    string   `json:"workdir"`
	CPUEff     float64  `json:"cpu_eff"`
	MemFrac    float64  `json:"mem_frac"`
	PeakFrac   float64  `json:"peak_frac"`
	GPUUtil    float64  `json:"gpu_util"`
	Log        string   `json:"log"` // log script name

	// SubmittedAt is set for jobs submitted through sbatch in demo mode.
	SubmittedAt time.Time `json:"-"`
}

// Past is a finished job in accounting.
type Past struct {
	ID          int      `json:"id"`
	Name        string   `json:"name"`
	Partition   string   `json:"partition"`
	State       string   `json:"state"`
	Exit        string   `json:"exit"`
	EndAgo      Dur      `json:"end_ago"`
	Elapsed     Dur      `json:"elapsed"`
	Limit       string   `json:"limit"`
	CPUs        int      `json:"cpus"`
	MemGB       float64  `json:"mem_gb"`
	GPUs        int      `json:"gpus"`
	GPUType     string   `json:"gpu_type"` // recorded in the allocation, e.g. "nvidia_h200_nvl"
	PeakFrac    float64  `json:"peak_frac"`
	CPUEff      float64  `json:"cpu_eff"`
	GPUUtil     float64  `json:"gpu_util"`
	Nodes       []string `json:"nodes"`
	WorkDir     string   `json:"workdir"`
	CancelledBy int      `json:"cancelled_by"`
}

// Quota is one storage location.
type Quota struct {
	Label     string `json:"label"`
	Path      string `json:"path"`
	FS        string `json:"fs"`
	Backend   string `json:"backend"`
	UsedGB    int64  `json:"used_gb"`
	SoftGB    int64  `json:"soft_gb"`
	HardGB    int64  `json:"hard_gb"`
	Files     int64  `json:"files"`
	FilesSoft int64  `json:"files_soft"`
	FilesHard int64  `json:"files_hard"`
	Note      string `json:"note"`
	// SharedFS: no personal quota; the sizes are the whole filesystem's.
	SharedFS bool `json:"shared_fs"`
	// GrowthGB is how many GB the location has been growing per day; the
	// demo makes up a 30-day history that ends at UsedGB.
	GrowthGB float64 `json:"growth_gb_day"`
}

// Fairshare is the user's sshare row.
type Fairshare struct {
	RawShares      int64   `json:"raw_shares"`
	NormShares     float64 `json:"norm_shares"`
	RawUsage       int64   `json:"raw_usage"`
	EffectiveUsage float64 `json:"effective_usage"`
	FairShare      float64 `json:"fairshare"`
}

// Dur is a JSON duration: Go syntax plus a "d" (day) prefix, e.g. "2d3h".
type Dur time.Duration

// UnmarshalJSON implements json.Unmarshaler.
func (d *Dur) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := parseDur(s)
	*d = Dur(v)
	return err
}

func parseDur(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	var days time.Duration
	if i := strings.IndexByte(s, 'd'); i > 0 {
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("duration %q: %w", s, err)
		}
		days, s = time.Duration(n)*24*time.Hour, s[i+1:]
	}
	if s == "" {
		return days, nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("duration %q: %w", s, err)
	}
	return days + v, nil
}

// D returns the duration.
func (d Dur) D() time.Duration { return time.Duration(d) }

// Load parses an embedded scenario by name ("default" or "hetero").
func Load(name string) (Scenario, error) {
	if name == "" {
		name = "default"
	}
	b, err := demodata.Files.ReadFile(name + ".json")
	if err != nil {
		return Scenario{}, fmt.Errorf("no demo scenario %q (have: %s)", name, strings.Join(Names(), ", "))
	}
	return Parse(b)
}

// Names lists the embedded scenarios.
func Names() []string {
	entries, _ := demodata.Files.ReadDir(".")
	var out []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok {
			out = append(out, n)
		}
	}
	return out
}

// Parse parses a scenario and checks it for mistakes.
func Parse(b []byte) (Scenario, error) {
	var sc Scenario
	if err := json.Unmarshal(b, &sc); err != nil {
		return sc, fmt.Errorf("demo scenario: %w", err)
	}
	if sc.User == "" || sc.Loop <= 0 || len(sc.Nodes) == 0 {
		return sc, fmt.Errorf("demo scenario: user, loop and nodes are required")
	}
	nodes := map[string]bool{}
	for _, n := range sc.Nodes {
		nodes[n.Name] = true
	}
	for i := range sc.Jobs {
		j := &sc.Jobs[i]
		if j.Account == "" {
			j.Account = sc.Account
		}
		if j.QOS == "" {
			j.QOS = "normal"
		}
		for _, n := range j.Nodes {
			if !nodes[n] {
				return sc, fmt.Errorf("demo scenario: job %d uses unknown node %s", j.ID, n)
			}
		}
	}
	return sc, nil
}
