package demo

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// submitDelay is how long a job submitted in demo mode pends.
const submitDelay = 20 * time.Second

// Sim is the simulated cluster.
type Sim struct {
	sc    Scenario
	clock state.Clock
	t0    time.Time

	mu        sync.Mutex
	cycle     int64
	mods      map[string]*mod // by job ID ("812"), task ("815_3") or pending tasks ("815_p")
	submitted []Job           // jobs from sbatch, this loop only
	nextID    int
}

// mod is what the user changed about a job through actions.
type mod struct {
	cancelled time.Time
	held      *bool
	requeued  time.Time
}

// row is one squeue line.
type row struct {
	job      *Job
	raw      string // "812", "815_3", "815_[4-9]"
	jobID    int    // the job's own ID (a task's ID differs from the array's)
	task     string // "", "3", "4-9"
	state    string // RUNNING | PENDING
	reason   string
	start    time.Time
	submit   time.Time
	est      time.Time
	nodes    []string
	priority int
	limit    string
}

// done is a job that left the queue during this loop.
type done struct {
	job         *Job
	raw         string
	jobID       int
	submit      time.Time
	start, end  time.Time
	state, exit string
	cancelledBy int
	limit       string
}

// New starts a simulation at the clock's current time.
func New(sc Scenario, clock state.Clock) *Sim {
	return &Sim{sc: sc, clock: clock, t0: clock.Now(), mods: map[string]*mod{}, nextID: 1000}
}

// Scenario returns the scenario.
func (s *Sim) Scenario() Scenario { return s.sc }

// loopPos returns the offset into the loop and the loop's start time.
func (s *Sim) loopPos(now time.Time) (time.Duration, time.Time) {
	loop := s.sc.Loop.D()
	el := max(now.Sub(s.t0), 0)
	cycle := int64(el / loop)
	if cycle != s.cycle {
		s.cycle = cycle
		s.mods = map[string]*mod{}
		s.submitted = nil
	}
	e := el % loop
	return e, now.Add(-e)
}

func (s *Sim) mod(key string) *mod {
	m := s.mods[key]
	if m == nil {
		m = &mod{}
		s.mods[key] = m
	}
	return m
}

// modFor merges the modifications that apply to a row: the job's, then
// the task's.
func (s *Sim) modFor(keys ...string) mod {
	var out mod
	for _, k := range keys {
		m, ok := s.mods[k]
		if !ok {
			continue
		}
		if !m.cancelled.IsZero() && (out.cancelled.IsZero() || m.cancelled.Before(out.cancelled)) {
			out.cancelled = m.cancelled
		}
		if m.held != nil {
			out.held = m.held
		}
		if !m.requeued.IsZero() {
			out.requeued = m.requeued
		}
	}
	return out
}

// jobs returns scenario jobs plus submitted ones.
func (s *Sim) jobs() []*Job {
	out := make([]*Job, 0, len(s.sc.Jobs)+len(s.submitted))
	for i := range s.sc.Jobs {
		out = append(out, &s.sc.Jobs[i])
	}
	for i := range s.submitted {
		out = append(out, &s.submitted[i])
	}
	return out
}

// snapshot computes the queue and the jobs that ended in this loop.
func (s *Sim) snapshot(now time.Time) (rows []row, ended []done) {
	e, start := s.loopPos(now)
	at := func(d Dur) time.Time { return start.Add(d.D()) }
	for _, j := range s.jobs() {
		id := strconv.Itoa(j.ID)
		submit := start.Add(-j.SubmitAgo.D())
		if j.SubmitAgo == 0 {
			submit = start.Add(-j.RunBefore.D() - 3*time.Minute)
		}
		if j.Array != "" {
			r, d := s.arrayRows(j, now, e, start, submit)
			rows, ended = append(rows, r...), append(ended, d...)
			continue
		}
		m := s.modFor(id)
		r := row{job: j, raw: id, jobID: j.ID, submit: submit, limit: j.Limit, priority: j.Priority, reason: j.Reason, nodes: j.Nodes}
		running := j.RunBefore > 0
		if !j.SubmittedAt.IsZero() {
			r.submit = j.SubmittedAt
			running = now.Sub(j.SubmittedAt) >= submitDelay
		}
		switch {
		case running && !j.SubmittedAt.IsZero():
			r.state, r.reason, r.start = "RUNNING", "None", j.SubmittedAt.Add(submitDelay)
		case running:
			r.state, r.reason, r.start = "RUNNING", "None", start.Add(-j.RunBefore.D())
		case !j.SubmittedAt.IsZero():
			r.state, r.nodes, r.est = "PENDING", nil, j.SubmittedAt.Add(submitDelay)
		default:
			r.state, r.nodes = "PENDING", nil
			if j.EstStart > 0 {
				r.est = at(j.EstStart)
			}
		}
		// Natural end.
		if j.EndAt > 0 && e >= j.EndAt.D() && running && (m.cancelled.IsZero() || m.cancelled.After(at(j.EndAt))) {
			ended = append(ended, done{job: j, raw: id, jobID: j.ID, submit: submit, start: r.start, end: at(j.EndAt), state: j.Final, exit: j.Exit, limit: j.Limit})
			continue
		}
		if !m.cancelled.IsZero() {
			ended = append(ended, done{job: j, raw: id, jobID: j.ID, submit: submit, start: r.start, end: m.cancelled, state: "CANCELLED", exit: "0:0", cancelledBy: s.sc.UID, limit: j.Limit})
			continue
		}
		if !m.requeued.IsZero() && running {
			r.state, r.reason, r.start, r.nodes, r.submit = "PENDING", "BeginTime", time.Time{}, nil, m.requeued
			r.est = m.requeued.Add(2 * time.Minute)
		}
		s.applyHold(&r, m)
		rows = append(rows, r)
	}
	return rows, ended
}

func (s *Sim) applyHold(r *row, m mod) {
	if r.state != "PENDING" {
		return
	}
	held := r.job.Reason == "JobHeldUser"
	if m.held != nil {
		held = *m.held
	}
	switch {
	case held:
		r.reason, r.priority, r.est = "JobHeldUser", 0, time.Time{}
	case r.reason == "JobHeldUser":
		r.reason, r.priority = "Priority", 4500
	}
}

// arrayRows expands an array: started tasks run as their own rows, the
// rest pend as one row.
func (s *Sim) arrayRows(j *Job, now time.Time, e time.Duration, start, submit time.Time) (rows []row, ended []done) {
	tasks := expandRange(j.Array)
	id := strconv.Itoa(j.ID)
	var pending []int
	for k, t := range tasks {
		tid := id + "_" + strconv.Itoa(t)
		m := s.modFor(id, tid)
		startAt := time.Duration(-1)
		if k == 0 && j.ArrayStart > 0 {
			startAt = j.ArrayStart.D()
		}
		running := startAt >= 0 && e >= startAt
		if running {
			pm := s.modFor(id + "_p")
			if !pm.cancelled.IsZero() && pm.cancelled.Before(start.Add(startAt)) {
				m.cancelled = pm.cancelled
				running = false
			}
			if held := s.modFor(id, id+"_p").held; held != nil && *held {
				running = false
			}
		}
		if !running {
			if !m.cancelled.IsZero() {
				continue // cancelled while pending: shown in the array's final state
			}
			pending = append(pending, t)
			continue
		}
		r := row{
			job: j, raw: tid, jobID: j.ID + 100 + k, task: strconv.Itoa(t), state: "RUNNING", reason: "None",
			start: start.Add(startAt), submit: submit, nodes: j.Nodes, limit: j.Limit,
		}
		if !m.cancelled.IsZero() {
			ended = append(ended, done{job: j, raw: tid, jobID: r.jobID, submit: submit, start: r.start, end: m.cancelled, state: "CANCELLED", exit: "0:0", cancelledBy: s.sc.UID, limit: r.limit})
			continue
		}
		rows = append(rows, r)
	}
	if len(pending) > 0 {
		spec := compactRange(pending)
		raw := id + "_[" + spec + "]"
		if len(pending) == 1 {
			raw = id + "_" + spec
		}
		m := s.modFor(id, id+"_p")
		r := row{job: j, raw: raw, jobID: j.ID, task: spec, state: "PENDING", reason: j.Reason, submit: submit, priority: j.Priority, limit: j.Limit}
		if j.EstStart > 0 {
			est := start.Add(j.EstStart.D())
			if !est.After(now) {
				est = now.Add(38 * time.Minute)
			}
			r.est = est
		}
		s.applyHold(&r, m)
		rows = append(rows, r)
	}
	return rows, ended
}

func expandRange(spec string) []int {
	var out []int
	for _, part := range strings.Split(spec, ",") {
		lo, hi, ok := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			continue
		}
		b := a
		if ok {
			if b, err = strconv.Atoi(hi); err != nil {
				continue
			}
		}
		for t := a; t <= b; t++ {
			out = append(out, t)
		}
	}
	return out
}

func compactRange(ts []int) string {
	slices.Sort(ts)
	var parts []string
	for i := 0; i < len(ts); {
		j := i
		for j+1 < len(ts) && ts[j+1] == ts[j]+1 {
			j++
		}
		if i == j {
			parts = append(parts, strconv.Itoa(ts[i]))
		} else {
			parts = append(parts, strconv.Itoa(ts[i])+"-"+strconv.Itoa(ts[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}

// nodeUse is the allocation on one node.
type nodeUse struct {
	cpus, gpus int            // gpus: whole GPUs
	mig        map[string]int // MIG slices in use by profile
	memMB      int64
	users      map[string]bool
}

func (s *Sim) usage(rows []row) map[string]*nodeUse {
	out := map[string]*nodeUse{}
	for _, r := range rows {
		if r.state != "RUNNING" {
			continue
		}
		n := max(len(r.nodes), 1)
		for _, name := range r.nodes {
			u := out[name]
			if u == nil {
				u = &nodeUse{users: map[string]bool{}}
				out[name] = u
			}
			u.cpus += r.job.CPUs / n
			if units.IsMIG(r.job.GPUType) {
				if u.mig == nil {
					u.mig = map[string]int{}
				}
				u.mig[r.job.GPUType] += r.job.GPUs / n
			} else {
				u.gpus += r.job.GPUs / n
			}
			u.memMB += int64(r.job.MemGB*1024) / int64(n)
			u.users[r.job.User] = true
		}
	}
	return out
}

// limitOf parses a limit ("2:00:00"); zero means unlimited.
func limitOf(s string) time.Duration {
	d, err := units.ParseLimit(s)
	if err != nil || d == nil {
		return 0
	}
	return *d
}

// findRows resolves a job reference from an action to matching rows and
// the modification keys to set.
func (s *Sim) resolve(ref string, rows []row) (keys []string, matched []row) {
	base, task, hasTask := strings.Cut(ref, "_")
	for _, r := range rows {
		if strconv.Itoa(r.job.ID) != base && strconv.Itoa(r.jobID) != ref {
			continue
		}
		switch {
		case !hasTask:
			matched = append(matched, r)
		case strings.HasPrefix(task, "["):
			if r.state == "PENDING" && r.task != "" {
				matched = append(matched, r)
			}
		case r.task == task || (r.state == "PENDING" && slices.Contains(expandRange(r.task), atoi(task))):
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	switch {
	case !hasTask:
		keys = []string{base}
	case strings.HasPrefix(task, "[") || matched[0].state == "PENDING":
		keys = []string{base + "_p"}
	default:
		keys = []string{ref}
	}
	return keys, matched
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// sortRows orders squeue output like Slurm: by partition priority, then
// job ID.
func sortRows(rows []row) {
	slices.SortStableFunc(rows, func(a, b row) int {
		return cmp.Or(cmp.Compare(a.state, b.state)*-1, cmp.Compare(a.job.ID, b.job.ID), cmp.Compare(a.raw, b.raw))
	})
}

func (r row) String() string { return fmt.Sprintf("%s %s %s", r.raw, r.state, r.reason) }
