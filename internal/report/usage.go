package report

import (
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// UsageDoc is "sdash usage --json".
type UsageDoc struct {
	Header
	Days        int              `json:"days"`
	Finished    int              `json:"finished_jobs"`
	Outcomes    []UsageOutcome   `json:"outcomes"`
	Failed      int              `json:"failed_jobs"`
	FailureRate float64          `json:"failure_rate"`
	TopFailure  *UsageFailure    `json:"top_failure,omitempty"`
	CPUHours    float64          `json:"cpu_hours"`
	GPUHours    float64          `json:"gpu_hours"`
	Partitions  []UsagePartition `json:"partitions"`
	GPUTypes    []UsageGPUType   `json:"gpu_types"`
	Waste       UsageWaste       `json:"waste"`
	Worst       []UsageWorst     `json:"least_efficient"`

	Limits       []UsageLimit    `json:"limits"`
	LimitsNote   string          `json:"limits_note,omitempty"`
	Fairshare    []UsageStanding `json:"fairshare"`
	Pending      []UsagePending  `json:"pending_priority"`
	PriorityNote string          `json:"priority_note,omitempty"`
}

// UsageLimit is one account or QOS limit that applies to you. Used is null
// when Slurm reports no usage for it; Unit is "", "min" or "MB".
type UsageLimit struct {
	Scope string   `json:"scope"`
	Name  string   `json:"name"` // Slurm's name: "GrpTRES cpu"
	What  string   `json:"what"`
	Max   float64  `json:"max"`
	Used  *float64 `json:"used"`
	Unit  string   `json:"unit"`
	Near  bool     `json:"near_limit"`
}

// UsageStanding is your fairshare in one account.
type UsageStanding struct {
	Account   string  `json:"account"`
	FairShare float64 `json:"fairshare"`
	UsedPct   float64 `json:"usage_percent"`
	SharesPct float64 `json:"shares_percent"`
	Verdict   string  `json:"verdict"`
}

// UsagePending is what drives a pending job's priority.
type UsagePending struct {
	Job      string `json:"job"`
	Priority int64  `json:"priority"`
	Biggest  string `json:"biggest_factor,omitempty"`
	Detail   string `json:"detail"`
}

// UsageOutcome counts jobs that ended in one state.
type UsageOutcome struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// UsageFailure is the most common way jobs failed.
type UsageFailure struct {
	State string `json:"state"`
	Count int    `json:"count"`
	Job   string `json:"latest_job"`
	Hint  string `json:"hint,omitempty"`
}

// UsagePartition is time spent in one partition.
type UsagePartition struct {
	Partition string  `json:"partition"`
	Jobs      int     `json:"jobs"`
	CPUHours  float64 `json:"cpu_hours"`
	GPUHours  float64 `json:"gpu_hours"`
}

// UsageGPUType is GPU time by type; gpu_type is empty when the
// allocation names no type.
type UsageGPUType struct {
	GPUType string  `json:"gpu_type"`
	Display string  `json:"gpu_type_display"`
	Hours   float64 `json:"gpu_hours"`
}

// UsageWaste is what completed jobs asked for and did not use. The *_jobs
// fields say how many jobs each figure covers; idle GPU time is null when
// the site records no GPU utilisation.
type UsageWaste struct {
	CPUJobs      int      `json:"cpu_jobs"`
	CPUHeldHours float64  `json:"cpu_hours_held"` // by those jobs
	IdleCPUHours float64  `json:"idle_cpu_hours"`
	MemJobs      int      `json:"memory_jobs"`
	IdleMemGBh   float64  `json:"idle_memory_gb_hours"` // the peak is estimated
	GPUJobs      int      `json:"gpu_jobs"`
	GPUHeldHours float64  `json:"gpu_hours_held"` // by those jobs
	IdleGPUHours *float64 `json:"idle_gpu_hours"`
}

// UsageWorst is one of the jobs that wasted the most.
type UsageWorst struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	IdleCPUHours float64      `json:"idle_cpu_hours"`
	IdleGPUHours float64      `json:"idle_gpu_hours"`
	Suggestions  []Suggestion `json:"suggestions,omitempty"`
}

// Usage builds the usage document.
func Usage(h Header, s insights.Summary, acct insights.Account, aliases map[string]string) UsageDoc {
	doc := UsageDoc{
		Header: h, Days: s.Days, Finished: s.Finished, Failed: s.Failed, FailureRate: s.FailRate,
		CPUHours: s.CPUHours, GPUHours: s.GPUHours,
		Outcomes: []UsageOutcome{}, Partitions: []UsagePartition{}, GPUTypes: []UsageGPUType{}, Worst: []UsageWorst{},
		Limits: []UsageLimit{}, LimitsNote: acct.LimitsNote, Fairshare: []UsageStanding{}, Pending: []UsagePending{}, PriorityNote: acct.PriorityNote,
		Waste: UsageWaste{
			CPUJobs: s.Waste.CPUJobs, CPUHeldHours: s.Waste.CPUHeld, IdleCPUHours: s.Waste.IdleCPUHours,
			MemJobs: s.Waste.MemJobs, IdleMemGBh: s.Waste.IdleMemGBh, GPUJobs: s.Waste.GPUJobs, GPUHeldHours: s.Waste.GPUHeld,
		},
	}
	if s.Waste.GPUJobs > 0 {
		v := s.Waste.IdleGPUHours
		doc.Waste.IdleGPUHours = &v
	}
	for _, o := range s.Outcomes {
		doc.Outcomes = append(doc.Outcomes, UsageOutcome{State: string(o.State), Count: o.Count})
	}
	if f := s.TopFailure; f.Count > 0 {
		doc.TopFailure = &UsageFailure{State: string(f.State), Count: f.Count, Job: f.Job, Hint: f.Hint}
	}
	for _, p := range s.ByPartition {
		doc.Partitions = append(doc.Partitions, UsagePartition(p))
	}
	for _, t := range s.ByGPUType {
		doc.GPUTypes = append(doc.GPUTypes, UsageGPUType{GPUType: t.Type, Display: units.GPUDisplayNames(t.Type, aliases), Hours: t.Hours})
	}
	for _, w := range s.Worst {
		u := UsageWorst{ID: w.Job.ID.Raw, Name: w.Job.Name, IdleCPUHours: w.IdleCPUHours, IdleGPUHours: w.IdleGPUHours}
		for _, x := range w.Suggestions {
			u.Suggestions = append(u.Suggestions, Suggestion{What: x.What, Reason: x.Reason, Line: x.Line})
		}
		doc.Worst = append(doc.Worst, u)
	}
	for _, l := range acct.Limits {
		u := UsageLimit{Scope: l.Scope, Name: l.Limit.Name, What: l.What, Max: l.Limit.Max, Unit: l.Limit.Unit, Near: l.Near()}
		if l.Limit.Used >= 0 {
			v := l.Limit.Used
			u.Used = &v
		}
		doc.Limits = append(doc.Limits, u)
	}
	for _, st := range acct.Standing {
		doc.Fairshare = append(doc.Fairshare, UsageStanding(st))
	}
	for _, p := range acct.Pending {
		doc.Pending = append(doc.Pending, UsagePending{Job: p.JobID, Priority: p.Priority, Biggest: p.Biggest, Detail: p.Line})
	}
	return doc
}
