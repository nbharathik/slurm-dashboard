package insights

import (
	"cmp"
	"slices"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// WorstJobs is how many of the least efficient jobs Summarise lists.
const WorstJobs = 5

// Summary is what a set of finished jobs used.
type Summary struct {
	Days     int // the window the caller asked for; 0 when not known
	Finished int // jobs that left the queue
	Outcomes []Outcome
	Failed   int     // jobs that ended badly (see model.JobState.IsFailure)
	FailRate float64 // Failed / Finished, 0 when nothing finished
	// TopFailure is the most common way jobs failed, "" when none did.
	TopFailure Failure

	CPUHours    float64
	GPUHours    float64
	ByPartition []PartitionHours
	ByGPUType   []TypeHours

	Waste Waste
	// Worst are the completed jobs that wasted the most, biggest first.
	Worst []JobWaste
}

// Outcome counts jobs that ended in one state.
type Outcome struct {
	State model.JobState
	Count int
}

// Failure is the most common failure state, with the explanation of its
// latest job.
type Failure struct {
	State model.JobState
	Count int
	Job   string // the latest job in that state
	Hint  string // one line: what usually causes it
}

// PartitionHours is the time spent in one partition.
type PartitionHours struct {
	Partition string
	Jobs      int
	CPUHours  float64
	GPUHours  float64
}

// TypeHours is GPU time by GPU type. Type is empty for jobs whose
// allocation does not name a type.
type TypeHours struct {
	Type  string
	Hours float64
}

// Waste is what completed jobs asked for and did not use, over jobs with known usage.
type Waste struct {
	CPUJobs      int
	CPUHeld      float64 // CPU-hours held by those jobs
	IdleCPUHours float64 // allocated CPU time minus CPU time used
	MemJobs      int
	IdleMemGBh   float64 // (allocated - peak) memory times elapsed hours; the peak is estimated
	GPUJobs      int     // jobs with a GPU utilisation figure; 0 when the site does not record it
	GPUHeld      float64 // GPU-hours held by those jobs
	IdleGPUHours float64 // GPU-hours times (1 - utilisation)
}

// JobWaste is one completed job and what it wasted.
type JobWaste struct {
	Job          model.HistoryJob
	IdleCPUHours float64
	IdleGPUHours float64
	// Suggestions are the right-sizing lines for this job.
	Suggestions []Suggestion
}

// Hours is the wasted resource-hours of a job: idle CPU-hours plus idle
// GPU-hours, each counting one. Jobs are ranked by it.
func (w JobWaste) Hours() float64 { return w.IdleCPUHours + w.IdleGPUHours }

// Summarise adds up the finished jobs in jobs. Jobs still queued or
// running are left out. uid is the user's numeric ID, for the failure hint.
func Summarise(jobs []model.HistoryJob, days int, uid string) Summary {
	s := Summary{Days: days}
	counts := map[model.JobState]int{}
	byPart := map[string]*PartitionHours{}
	byType := map[string]float64{}
	failures := map[model.JobState]*Failure{}
	latest := map[model.JobState]model.HistoryJob{}

	for _, j := range jobs {
		if j.State.IsActive() || j.State == model.StatePending {
			continue
		}
		s.Finished++
		counts[j.State]++
		if j.State.IsFailure() {
			s.Failed++
			f := failures[j.State]
			if f == nil {
				f = &Failure{State: j.State}
				failures[j.State] = f
			}
			f.Count++
			if prev, ok := latest[j.State]; !ok || j.End.After(prev.End) {
				latest[j.State] = j
			}
		}

		hours := j.Elapsed.Hours()
		cpuH := float64(j.AllocCPUs) * hours
		gpuH := float64(j.GPUs) * hours
		s.CPUHours += cpuH
		s.GPUHours += gpuH
		p := byPart[j.Partition]
		if p == nil {
			p = &PartitionHours{Partition: j.Partition}
			byPart[j.Partition] = p
		}
		p.Jobs++
		p.CPUHours += cpuH
		p.GPUHours += gpuH
		if gpuH > 0 {
			byType[j.GPUType] += gpuH
		}

		if j.State == model.StateCompleted {
			s.addWaste(j)
		}
	}

	for st, n := range counts {
		s.Outcomes = append(s.Outcomes, Outcome{State: st, Count: n})
	}
	slices.SortFunc(s.Outcomes, func(a, b Outcome) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(string(a.State), string(b.State)))
	})
	if s.Finished > 0 {
		s.FailRate = float64(s.Failed) / float64(s.Finished)
	}
	for st, f := range failures {
		j := latest[st]
		f.Job = j.ID.Raw
		f.Hint = ExplainFailure(j, uid).Text
		if s.TopFailure.Count < f.Count || (s.TopFailure.Count == f.Count && string(f.State) < string(s.TopFailure.State)) {
			s.TopFailure = *f
		}
	}
	for _, p := range byPart {
		s.ByPartition = append(s.ByPartition, *p)
	}
	slices.SortFunc(s.ByPartition, func(a, b PartitionHours) int {
		return cmp.Or(cmp.Compare(b.CPUHours+b.GPUHours, a.CPUHours+a.GPUHours), strings.Compare(a.Partition, b.Partition))
	})
	for t, h := range byType {
		s.ByGPUType = append(s.ByGPUType, TypeHours{Type: t, Hours: h})
	}
	slices.SortFunc(s.ByGPUType, func(a, b TypeHours) int {
		return cmp.Or(cmp.Compare(b.Hours, a.Hours), strings.Compare(a.Type, b.Type))
	})
	slices.SortStableFunc(s.Worst, func(a, b JobWaste) int {
		return cmp.Or(cmp.Compare(b.Hours(), a.Hours()), strings.Compare(a.Job.ID.Raw, b.Job.ID.Raw))
	})
	if len(s.Worst) > WorstJobs {
		s.Worst = s.Worst[:WorstJobs]
	}
	return s
}

// JobIdle returns a completed job's idle CPU- and GPU-hours, and whether either is known.
func JobIdle(j model.HistoryJob) (cpu, gpu float64, ok bool) {
	if j.State != model.StateCompleted {
		return 0, 0, false
	}
	hours := j.Elapsed.Hours()
	if j.Eff.CPU >= 0 {
		cpu, ok = float64(j.AllocCPUs)*hours*(1-min(j.Eff.CPU, 1)), true
	}
	if j.GPUs > 0 && j.Eff.GPUUtil >= 0 {
		gpu, ok = j.Eff.GPUHours*(1-min(j.Eff.GPUUtil, 1)), true
	}
	return cpu, gpu, ok
}

// addWaste counts one completed job's idle resources.
func (s *Summary) addWaste(j model.HistoryJob) {
	hours := j.Elapsed.Hours()
	jw := JobWaste{Job: j}
	jw.IdleCPUHours, jw.IdleGPUHours, _ = JobIdle(j)
	if j.Eff.CPU >= 0 {
		s.Waste.CPUJobs++
		s.Waste.CPUHeld += float64(j.AllocCPUs) * hours
		s.Waste.IdleCPUHours += jw.IdleCPUHours
	}
	if j.Eff.Mem >= 0 && j.AllocMemMB > 0 {
		s.Waste.MemJobs++
		s.Waste.IdleMemGBh += float64(max(j.AllocMemMB-j.PeakMemMB, 0)) / 1024 * hours
	}
	if j.GPUs > 0 && j.Eff.GPUUtil >= 0 {
		s.Waste.GPUJobs++
		s.Waste.GPUHeld += j.Eff.GPUHours
		s.Waste.IdleGPUHours += jw.IdleGPUHours
	}
	if jw.Hours() > 0 {
		jw.Suggestions = RightSize(j)
		s.Worst = append(s.Worst, jw)
	}
}
