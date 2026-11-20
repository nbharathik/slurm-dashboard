// Package state collects, schedules and caches dashboard data with freshness tracking.
package state

import (
	"slices"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// Source is the latest state of one data source.
type Source[T any] struct {
	Data  T
	Has   bool          // at least one successful update
	At    time.Time     // last success
	Tried time.Time     // last attempt
	Err   error         // error of the last attempt, nil on success
	Took  time.Duration // duration of the last attempt
}

func (s *Source[T]) apply(u Update) bool {
	s.Tried, s.Took, s.Err = u.At, u.Took, u.Err
	if u.Err != nil {
		return false
	}
	v, ok := u.Data.(T)
	if !ok {
		return false
	}
	s.Data, s.Has, s.At = v, true, u.At
	return true
}

// Freshness is how current a source is.
type Freshness int

// Freshness levels for the header badge.
const (
	Unknown Freshness = iota // never loaded
	Fresh                    // updated within 2× its interval
	Stale                    // older than that
	Failing                  // the last attempt failed
)

// Freshness rates a source given its interval.
func (s *Source[T]) Freshness(now time.Time, interval time.Duration) Freshness {
	switch {
	case s.Err != nil:
		return Failing
	case !s.Has:
		return Unknown
	case now.Sub(s.At) > 2*interval:
		return Stale
	}
	return Fresh
}

// Store is the latest data from every source.
type Store struct {
	derived       bool
	nodeUsage     []NodeUsage
	partSummaries []PartSummary
	User          string
	UID           string // numeric user ID
	ClusterName   string
	Caps          model.Capabilities
	// PrivateJobs: the site sets PrivateData=jobs, so other users' jobs
	// are not visible.
	PrivateJobs bool
	// Site is what "scontrol show config" says the cluster can do
	// (empty when it could not be read: then nothing is hidden).
	Site parse.ClusterInfo

	// StorageWarn and StorageCrit are the [alerts] thresholds in percent
	// (zero means the defaults).
	StorageWarn, StorageCrit int
	// TimeLeftWarn is warn_time_left as a duration (zero: the default,
	// negative: off); WarnWaste is warn_waste.
	TimeLeftWarn time.Duration
	WarnWaste    bool

	// Trends is how full each storage location has been (may be nil).
	// The storage collector adds to it; views only read.
	Trends *insights.Log

	// DiskUsage holds disk-usage analyses by path.
	DiskUsage map[string]model.DiskUsage

	// Ended are the user's jobs seen leaving the queue in this session,
	// with their final state from sacct (newest last, at most MaxEnded).
	Ended []model.HistoryJob

	MyJobs       Source[[]model.Job]
	AllJobs      Source[[]model.Job]
	Cluster      Source[ClusterData]
	QueueRank    Source[[]parse.PendingJob]
	Nodes        Source[[]model.Node]
	Partitions   Source[[]model.Partition]
	Reservations Source[[]model.Reservation]
	Detail       Source[Detail]
	History      Source[HistoryData]
	Fairshare    Source[[]model.Share]
	Priorities   Source[[]model.PriorityFactors]
	Limits       Source[[]model.LimitScope]
	MyStats      Source[map[string]model.JobStat]
	Storage      Source[[]model.Quota]
	Groups       Source[[]string]
	Sched        Source[model.SchedStats]

	prevJobs map[string]model.Job
	havePrev bool
}

// Apply records an update and returns the job transitions it revealed
// (only for "myjobs").
func (s *Store) Apply(u Update) []Transition {
	e, ok := lookupSource(u.Source)
	if !ok {
		return nil
	}
	if e.derives {
		s.derived = false
	}
	if e.apply(s, u) && u.Source == "myjobs" {
		return s.transitions(s.MyJobs.Data)
	}
	return nil
}

// Identity gathers what partition access checks know about the user. Groups
// and accounts stay unknown until their sources have loaded.
func (s *Store) Identity() Identity {
	id := Identity{Root: s.UID == "0"}
	if s.Groups.Has && len(s.Groups.Data) > 0 {
		id.Groups, id.GroupsKnown = s.Groups.Data, true
	}
	if s.Fairshare.Has && s.User != "" {
		for _, sh := range s.Fairshare.Data {
			if sh.User == s.User && sh.Account != "" && !slices.Contains(id.Accounts, sh.Account) {
				id.Accounts = append(id.Accounts, sh.Account)
			}
		}
		id.AccountsKnown = len(id.Accounts) > 0
	}
	return id
}

// RateLimited reports whether the controller is throttling this user: a
// recent latest attempt of any source failed with the rate limit error. A
// source that stopped running (its tab is hidden) does not keep it on.
func (s *Store) RateLimited(now time.Time) bool {
	for _, e := range sources {
		st := e.status(s)
		if st.Err != nil && KindOf(st.Err) == ErrRateLimited && now.Sub(st.Tried) < 2*RateLimitWait {
			return true
		}
	}
	return false
}

// DropAllJobs forgets everyone's jobs (the Queue tab loads them again), the
// largest data on a big cluster.
func (s *Store) DropAllJobs() {
	s.AllJobs = Source[[]model.Job]{}
	s.derived = false
}

// MaxEnded bounds Store.Ended.
const MaxEnded = 200

// AddEnded records the final state of jobs that left the queue.
func (s *Store) AddEnded(jobs ...model.HistoryJob) {
	s.Ended = append(s.Ended, jobs...)
	if n := len(s.Ended) - MaxEnded; n > 0 {
		s.Ended = append([]model.HistoryJob(nil), s.Ended[n:]...)
	}
}

// UsageInput gathers what the usage view needs to explain limits and
// priority, including why a part is missing.
func (s *Store) UsageInput() insights.AccountInput {
	in := insights.AccountInput{
		User: s.User, NoAccounting: s.Site.AccountingOff(), BasicPriority: s.Site.BasicPriority(), FairTree: s.Site.FairTree(),
		Scopes: s.Limits.Data, LimitsRead: s.Limits.Has, LimitsErr: firstLine(s.Limits.Err),
		Shares: s.Fairshare.Data, Priorities: s.Priorities.Data, PriorityRead: s.Fairshare.Has, PriorityErr: firstLine(s.Fairshare.Err),
	}
	if !s.Caps.HasSshare && s.Caps.Version != "" {
		in.PriorityErr = "sshare is not available here"
	}
	return in
}

func firstLine(err error) string {
	if err == nil {
		return ""
	}
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

// Alerts computes the Overview alerts from the latest data.
func (s *Store) Alerts(now time.Time, dismissed map[string]time.Time) []model.Alert {
	return insights.Compute(insights.Input{
		Now:          now,
		UID:          s.UID,
		MyJobs:       JoinJobs(s.MyJobs.Data, s.Cluster.Data.Jobs, nil), // allocated GPUs and memory
		Nodes:        s.Nodes.Data,
		Partitions:   s.Partitions.Data,
		Reservations: s.Reservations.Data,
		History:      s.History.Data.Jobs,
		Ended:        s.Ended,
		Storage:      s.Storage.Data,
		Dismissed:    dismissed,
		StorageWarn:  s.StorageWarn,
		StorageCrit:  s.StorageCrit,
		TimeLeft:     s.TimeLeftWarn,
		Stats:        s.MyStats.Data,
		StatsAt:      s.MyStats.At,
		Waste:        s.WarnWaste,
	})
}

// TransitionKind says what happened to a job between two snapshots.
type TransitionKind int

// Transition kinds.
const (
	Started TransitionKind = iota // PENDING -> RUNNING
	Ended                         // left the queue
	Changed                       // any other state change
)

// Transition is a change in one of the user's jobs.
type Transition struct {
	Kind TransitionKind
	Job  model.Job
	To   model.JobState // empty for Ended
}

func (s *Store) transitions(jobs []model.Job) []Transition {
	cur := make(map[string]model.Job, len(jobs))
	for _, j := range jobs {
		cur[j.ID.Raw] = j
	}
	var out []Transition
	if s.havePrev {
		out = Diff(s.prevJobs, cur)
	}
	s.prevJobs, s.havePrev = cur, true
	return out
}

// Diff compares two snapshots of the user's jobs, keyed by raw job ID.
func Diff(prev, cur map[string]model.Job) []Transition {
	// A pending task range ("123_[4-9]") changes its ID as tasks start; it
	// has not ended while any task of its array is still listed.
	arrays := map[uint64]bool{}
	for _, j := range cur {
		if j.ID.IsArray() {
			arrays[j.ID.ArrayJobID] = true
		}
	}
	var out []Transition
	for id, old := range prev {
		now, ok := cur[id]
		switch {
		case !ok && strings.Contains(old.ID.TaskSpec, "[") && arrays[old.ID.ArrayJobID]:
		case !ok:
			out = append(out, Transition{Kind: Ended, Job: old})
		case old.State == model.StatePending && now.State == model.StateRunning:
			out = append(out, Transition{Kind: Started, Job: now, To: now.State})
		case old.State != now.State:
			out = append(out, Transition{Kind: Changed, Job: now, To: now.State})
		}
	}
	sortTransitions(out)
	return out
}

// Capacity reuses the same derived snapshot across capacity views.
func (s *Store) Capacity() ([]NodeUsage, []PartSummary) {
	if !s.derived {
		s.nodeUsage = NodeGPUUsage(s.Nodes.Data, s.Cluster.Data.Jobs, s.User)
		pending, known := PendingByPartition(s)
		s.partSummaries = PartitionSummaries(s.Partitions.Data, s.nodeUsage, pending, known)
		ApplyAccess(s.partSummaries, s.Identity())
		s.derived = true
	}
	return s.nodeUsage, s.partSummaries
}
