package state

import (
	"slices"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// Status is a source's state without its data, for freshness and errors.
type Status struct {
	Has        bool
	At, Tried  time.Time
	Err        error
	Took       time.Duration
	Registered bool // false for an unknown name
}

func (s *Source[T]) status() Status {
	return Status{Has: s.Has, At: s.At, Tried: s.Tried, Err: s.Err, Took: s.Took, Registered: true}
}

// sourceEntry is one data source of the Store: how an update is applied and
// how its status is read. derives marks sources the capacity view is built
// from, so an update drops the cached derivation.
type sourceEntry struct {
	name    string
	derives bool
	apply   func(*Store, Update) bool
	status  func(*Store) Status
}

func register[T any](name string, derives bool, field func(*Store) *Source[T]) sourceEntry {
	return sourceEntry{
		name:    name,
		derives: derives,
		apply:   func(s *Store, u Update) bool { return field(s).apply(u) },
		status:  func(s *Store) Status { return field(s).status() },
	}
}

// sources is every Store source by its collector name: adding a source is
// one line here plus its collector.
var sources = []sourceEntry{
	register("myjobs", true, func(s *Store) *Source[[]model.Job] { return &s.MyJobs }),
	register("alljobs", true, func(s *Store) *Source[[]model.Job] { return &s.AllJobs }),
	register("cluster", true, func(s *Store) *Source[ClusterData] { return &s.Cluster }),
	register("queuerank", true, func(s *Store) *Source[[]parse.PendingJob] { return &s.QueueRank }),
	register("nodes", true, func(s *Store) *Source[[]model.Node] { return &s.Nodes }),
	register("partitions", true, func(s *Store) *Source[[]model.Partition] { return &s.Partitions }),
	register("reservations", false, func(s *Store) *Source[[]model.Reservation] { return &s.Reservations }),
	register("jobdetail", false, func(s *Store) *Source[Detail] { return &s.Detail }),
	register("history", false, func(s *Store) *Source[HistoryData] { return &s.History }),
	register("fairshare", true, func(s *Store) *Source[[]model.Share] { return &s.Fairshare }),
	register("sprio", false, func(s *Store) *Source[[]model.PriorityFactors] { return &s.Priorities }),
	register("limits", false, func(s *Store) *Source[[]model.LimitScope] { return &s.Limits }),
	register("mystats", false, func(s *Store) *Source[map[string]model.JobStat] { return &s.MyStats }),
	register("storage", false, func(s *Store) *Source[[]model.Quota] { return &s.Storage }),
	register("groups", true, func(s *Store) *Source[[]string] { return &s.Groups }),
	register("sdiag", false, func(s *Store) *Source[model.SchedStats] { return &s.Sched }),
}

func lookupSource(name string) (sourceEntry, bool) {
	i := slices.IndexFunc(sources, func(e sourceEntry) bool { return e.name == name })
	if i < 0 {
		return sourceEntry{}, false
	}
	return sources[i], true
}

// SourceNames lists every source the Store keeps.
func SourceNames() []string {
	out := make([]string, len(sources))
	for i, e := range sources {
		out[i] = e.name
	}
	return out
}

// Status reports a source's freshness and last error by name.
func (s *Store) Status(name string) Status {
	if e, ok := lookupSource(name); ok {
		return e.status(s)
	}
	return Status{}
}
