package state

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// Per-source timeouts.
const (
	TimeoutMyJobs     = 15 * time.Second
	TimeoutCluster    = 20 * time.Second
	TimeoutQueueRank  = 20 * time.Second
	TimeoutNodes      = 20 * time.Second
	TimeoutPartitions = 15 * time.Second
	TimeoutDetail     = 15 * time.Second
	TimeoutHistory    = 60 * time.Second
	TimeoutFairshare  = 15 * time.Second
	TimeoutLimits     = 15 * time.Second
	TimeoutStats      = 30 * time.Second
)

// LargeCluster is the running-job count above which the cluster query
// slows to at least a minute.
const LargeCluster = 5000

// ErrKind classifies a failed Slurm call.
type ErrKind int

// Kinds of Slurm failure.
const (
	ErrOther ErrKind = iota
	ErrControllerDown
	ErrAccountingDown
	ErrMissing
	ErrTimeout
)

// SlurmError is a failed Slurm call with a user-facing summary.
type SlurmError struct {
	Kind ErrKind
	Msg  string
	Err  error
}

func (e *SlurmError) Error() string { return e.Msg }
func (e *SlurmError) Unwrap() error { return e.Err }

// Classify wraps a runner error with its kind.
func Classify(err error, stderr []byte) error {
	if err == nil {
		return nil
	}
	msg := execx.FirstLine(stderr)
	if msg == "" {
		msg = err.Error()
	}
	low := strings.ToLower(msg)
	kind := ErrOther
	switch {
	case errors.Is(err, execx.ErrNotFound):
		kind = ErrMissing
	case errors.Is(err, execx.ErrTimeout):
		kind = ErrTimeout
	case strings.Contains(low, "accounting storage"),
		strings.Contains(low, "slurmdbd"),
		strings.Contains(low, "problem talking to the database"):
		// Checked first: database errors often end in "connection refused".
		kind = ErrAccountingDown
	case strings.Contains(low, "unable to contact slurm controller"),
		strings.Contains(low, "socket timed out"),
		strings.Contains(low, "slurm_receive_msg"),
		strings.Contains(low, "connection refused"):
		kind = ErrControllerDown
	}
	return &SlurmError{Kind: kind, Msg: msg, Err: err}
}

// KindOf returns the ErrKind of err (ErrOther when unknown).
func KindOf(err error) ErrKind {
	var se *SlurmError
	if errors.As(err, &se) {
		return se.Kind
	}
	return ErrOther
}

// ClusterData is the result of the cluster-wide running-jobs query.
type ClusterData struct {
	Jobs []model.RunningJob
}

// HistoryData is the result of the history query.
type HistoryData struct {
	Days int
	Jobs []model.HistoryJob
}

// Detail is the result of the job detail query for one job.
type Detail struct {
	ID     string
	Job    *model.JobDetail
	Stat   *model.JobStat
	Script string
}

// Sources runs and parses every Slurm data source.
type Sources struct {
	Runner execx.Runner
	Cmd    slurm.Commands
	Log    *slog.Logger

	mu       sync.Mutex
	warnLog  map[string][]time.Time
	warnings map[string]int
}

// run executes argv with a label and returns stdout or a classified error.
func (s *Sources) run(ctx context.Context, label string, argv []string) ([]byte, error) {
	res, err := s.Runner.Run(execx.WithLabel(ctx, label), argv...)
	if err != nil {
		return res.Stdout, Classify(err, res.Stderr)
	}
	return res.Stdout, nil
}

// noteWarnings logs parse warnings (at most 20 per source per hour) and
// counts them for the debug overlay.
func (s *Sources) noteWarnings(source string, warns []model.ParseWarning) {
	if len(warns) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warnLog == nil {
		s.warnLog, s.warnings = map[string][]time.Time{}, map[string]int{}
	}
	s.warnings[source] += len(warns)
	now := time.Now()
	recent := s.warnLog[source][:0]
	for _, t := range s.warnLog[source] {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	for _, w := range warns {
		if len(recent) >= 20 {
			break
		}
		recent = append(recent, now)
		if s.Log != nil {
			s.Log.Warn("unparseable Slurm output", "source", w.Source, "line", w.Line, "msg", w.Msg, "raw", w.Raw)
		}
	}
	s.warnLog[source] = recent
}

// WarningCounts returns how many parse warnings each source produced.
func (s *Sources) WarningCounts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.warnings))
	for k, v := range s.warnings {
		out[k] = v
	}
	return out
}

// MyJobs returns the user's jobs.
func (s *Sources) MyJobs(ctx context.Context) ([]model.Job, error) {
	out, err := s.run(ctx, "myjobs", s.Cmd.MyJobs())
	if err != nil {
		return nil, err
	}
	jobs, warns := parse.MyJobs(out)
	s.noteWarnings("myjobs", warns)
	for i := range jobs {
		if jobs[i].User == "" {
			jobs[i].User = s.Cmd.User
		}
	}
	return jobs, nil
}

// AllJobs returns every user's jobs.
func (s *Sources) AllJobs(ctx context.Context) ([]model.Job, error) {
	out, err := s.run(ctx, "alljobs", s.Cmd.AllJobs())
	if err != nil {
		return nil, err
	}
	jobs, warns := parse.MyJobs(out)
	s.noteWarnings("alljobs", warns)
	return jobs, nil
}

// Cluster returns all running jobs, optionally in some partitions.
func (s *Sources) Cluster(ctx context.Context, partitions []string) (ClusterData, error) {
	out, err := s.run(ctx, "cluster", s.Cmd.Cluster(partitions))
	if err != nil {
		return ClusterData{}, err
	}
	jobs, warns := parse.Running(out)
	s.noteWarnings("cluster", warns)
	return ClusterData{Jobs: jobs}, nil
}

// QueueRank returns pending jobs in the partitions.
func (s *Sources) QueueRank(ctx context.Context, partitions []string) ([]parse.PendingJob, error) {
	if len(partitions) == 0 {
		return nil, nil
	}
	out, err := s.run(ctx, "queuerank", s.Cmd.QueueRank(partitions))
	if err != nil {
		return nil, err
	}
	jobs, warns := parse.QueueRank(out)
	s.noteWarnings("queuerank", warns)
	return jobs, nil
}

// StartEstimate asks when a pending job is expected to start. ok is false
// when the job is not pending (or not visible).
func (s *Sources) StartEstimate(ctx context.Context, id string) (est parse.StartEstimate, ok bool, err error) {
	out, err := s.run(ctx, "start", s.Cmd.StartEstimate(id))
	if err != nil {
		return est, false, err
	}
	ests, warns := parse.StartEstimates(out)
	s.noteWarnings("start", warns)
	if len(ests) == 0 {
		return est, false, nil
	}
	return ests[0], true, nil
}

// Nodes returns all nodes.
func (s *Sources) Nodes(ctx context.Context) ([]model.Node, error) {
	out, err := s.run(ctx, "nodes", s.Cmd.Nodes())
	if err != nil {
		return nil, err
	}
	nodes, warns := parse.Nodes(out)
	s.noteWarnings("nodes", warns)
	return nodes, nil
}

// Partitions returns all partitions.
func (s *Sources) Partitions(ctx context.Context) ([]model.Partition, error) {
	out, err := s.run(ctx, "partitions", s.Cmd.Partitions())
	if err != nil {
		return nil, err
	}
	parts, warns := parse.Partitions(out)
	s.noteWarnings("partitions", warns)
	return parts, nil
}

// Reservations returns all reservations.
func (s *Sources) Reservations(ctx context.Context) ([]model.Reservation, error) {
	out, err := s.run(ctx, "reservations", s.Cmd.Reservations())
	if err != nil {
		return nil, err
	}
	res, warns := parse.Reservations(out)
	s.noteWarnings("reservations", warns)
	return res, nil
}

// JobDetail returns scontrol's view of one job, or nil if Slurm no longer
// knows it.
func (s *Sources) JobDetail(ctx context.Context, id string) (*model.JobDetail, error) {
	out, err := s.run(ctx, "jobdetail", s.Cmd.JobDetail(id))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "invalid job id") {
			return nil, nil
		}
		return nil, err
	}
	jobs, warns := parse.JobDetails(out)
	s.noteWarnings("jobdetail", warns)
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

// JobStat returns live usage of a running job, or nil without steps.
func (s *Sources) JobStat(ctx context.Context, id string) (*model.JobStat, error) {
	out, err := s.run(ctx, "jobstat", s.Cmd.Sstat(id))
	if err != nil {
		return nil, err
	}
	stat, warns := parse.Sstat(out)
	s.noteWarnings("jobstat", warns)
	if stat != nil {
		stat.At = time.Now()
	}
	return stat, nil
}

// StatBatch is how many jobs one MyStats call samples.
const StatBatch = 20

var jobNumber = regexp.MustCompile(`^[0-9]{1,12}$`)

// MyStats samples live usage by job ID (StatBatch per call); array and heterogeneous IDs are skipped.
func (s *Sources) MyStats(ctx context.Context, ids []string) (map[string]model.JobStat, error) {
	var ok []string
	for _, id := range ids {
		if jobNumber.MatchString(id) {
			ok = append(ok, id)
		}
	}
	out := map[string]model.JobStat{}
	for len(ok) > 0 {
		n := min(len(ok), StatBatch)
		raw, err := s.run(ctx, "mystats", s.Cmd.SstatJobs(ok[:n]))
		if err != nil {
			return out, err
		}
		stats, warns := parse.SstatJobs(raw)
		s.noteWarnings("mystats", warns)
		now := time.Now()
		for id, st := range stats {
			st.At = now
			out[id] = *st
		}
		ok = ok[n:]
	}
	return out, nil
}

// History returns the user's jobs over the last days.
func (s *Sources) History(ctx context.Context, days int) (HistoryData, error) {
	out, err := s.run(ctx, "history", s.Cmd.History(days))
	if err != nil {
		return HistoryData{Days: days}, err
	}
	jobs, warns := parse.History(out)
	s.noteWarnings("history", warns)
	return HistoryData{Days: days, Jobs: jobs}, nil
}

// HistoryJob returns one job's accounting record, or nil.
func (s *Sources) HistoryJob(ctx context.Context, id string) (*model.HistoryJob, error) {
	out, err := s.run(ctx, "historyjob", s.Cmd.HistoryJob(id))
	if err != nil {
		return nil, err
	}
	jobs, warns := parse.History(out)
	s.noteWarnings("historyjob", warns)
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

// Fairshare returns the user's fairshare associations.
func (s *Sources) Fairshare(ctx context.Context) ([]model.Share, error) {
	out, err := s.run(ctx, "fairshare", s.Cmd.Fairshare())
	if err != nil {
		return nil, err
	}
	shares, warns := parse.Shares(out)
	s.noteWarnings("fairshare", warns)
	return shares, nil
}

// userName is what a Slurm user name looks like; anything else is not
// passed on in "users=NAME".
var userName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Limits returns the account and QOS limits that apply to the user.
func (s *Sources) Limits(ctx context.Context) ([]model.LimitScope, error) {
	if !userName.MatchString(s.Cmd.User) {
		return nil, fmt.Errorf("limits: unusable user name")
	}
	out, err := s.run(ctx, "assoc_mgr", s.Cmd.AssocMgr())
	if err != nil {
		return nil, err
	}
	scopes, warns := parse.AssocMgr(out, s.Cmd.User)
	s.noteWarnings("assocmgr", warns)
	return scopes, nil
}

// Priorities returns the priority factors of the user's pending jobs.
func (s *Sources) Priorities(ctx context.Context) ([]model.PriorityFactors, error) {
	out, err := s.run(ctx, "sprio", s.Cmd.Priorities())
	if err != nil {
		return nil, err
	}
	rows, warns := parse.Priorities(out)
	s.noteWarnings("sprio", warns)
	return rows, nil
}

// FinalStates returns the outcome of jobs that left the queue.
func (s *Sources) FinalStates(ctx context.Context, ids []string) ([]parse.FinalState, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := s.run(ctx, "final", s.Cmd.FinalStates(ids))
	if err != nil {
		return nil, err
	}
	states, warns := parse.FinalStates(out)
	s.noteWarnings("final", warns)
	return states, nil
}

// BatchScript returns a job's batch script: from the controller while it
// knows the job, else from accounting where the site stores scripts.
func (s *Sources) BatchScript(ctx context.Context, id string) (string, error) {
	out, err := s.run(ctx, "script", s.Cmd.BatchScript(id))
	if err == nil && len(out) > 0 {
		return string(out), nil
	}
	out2, err2 := s.run(ctx, "script-sacct", s.Cmd.SacctBatchScript(id))
	if err2 == nil {
		script := stripSacctScriptHeader(string(out2))
		if strings.TrimSpace(script) != "" && !strings.Contains(script, "NONE") {
			return script, nil
		}
	}
	if err == nil {
		err = err2
	}
	return "", fmt.Errorf("script no longer available: %w", err)
}

// SubmitLine returns a job's submit command line and working directory (empty before Slurm 23.02).
func (s *Sources) SubmitLine(ctx context.Context, id string) (line, workDir string, err error) {
	out, err := s.run(ctx, "submitline", s.Cmd.SubmitLine(id))
	if err != nil {
		return "", "", err
	}
	line, workDir = parse.SubmitRecord(out)
	return line, workDir, nil
}

// stripSacctScriptHeader removes sacct's "Batch Script for N" banner.
func stripSacctScriptHeader(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "----") {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return s
}
