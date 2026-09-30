package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/notify"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/storage"
	"github.com/nbharathik/slurm-dashboard/internal/ui"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// viewState is what collectors need to know about the UI. The UI updates
// it through OnViewState; collectors read it from their goroutines.
type viewState struct {
	mu           sync.Mutex
	detailID     string
	historyDays  int
	pendingParts []string
	// running holds the IDs of the user's plain running jobs old enough to
	// judge (see statAge), for the mystats collector; warnWaste is the
	// warn_waste setting.
	running   []string
	warnWaste bool
	// fromAll holds the pending jobs of the last everyone's-jobs query,
	// so queue ranks need no query of their own while it is fresh.
	fromAll   []parse.PendingJob
	fromAllAt time.Time
}

func (v *viewState) set(fn func(v *viewState)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	fn(v)
}

func (v *viewState) get() viewState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return viewState{
		detailID: v.detailID, historyDays: v.historyDays, pendingParts: slices.Clone(v.pendingParts), fromAll: v.fromAll, fromAllAt: v.fromAllAt,
		running: slices.Clone(v.running), warnWaste: v.warnWaste,
	}
}

// onViewState handles the UI's view-state messages.
func (v *viewState) onViewState(msg tea.Msg) {
	switch m := msg.(type) {
	case views.DetailMsg:
		v.set(func(v *viewState) { v.detailID = m.ID })
	case views.HistoryRangeMsg:
		v.set(func(v *viewState) { v.historyDays = m.Days })
	}
}

// intervalMap names each source's interval for a refresh speed.
func intervalMap(iv config.Intervals) map[string]time.Duration {
	return map[string]time.Duration{
		"myjobs": iv.MyJobs, "alljobs": iv.AllJobs, "cluster": iv.Cluster, "queuerank": iv.QueueRank,
		"nodes": iv.Nodes, "partitions": iv.Partitions, "reservations": iv.Resv,
		"jobdetail": 10 * time.Second, "history": iv.History, "fairshare": iv.Fairshare, "sprio": iv.Fairshare,
		"storage": iv.Storage, "limits": 10 * time.Minute, "mystats": iv.Stats,
	}
}

// addCollectors registers data sources and their refresh intervals.
func addCollectors(s *state.Scheduler, src *state.Sources, site parse.ClusterInfo, iv *state.Intervals, vs *viewState,
	storageFn func(context.Context) ([]model.Quota, error),
) {
	fixed := func(name string) func() time.Duration { return func() time.Duration { return iv.Get(name) } }
	add := func(name string, opt state.Options, fn func(ctx context.Context) (any, error)) {
		s.Add(state.Func{N: name, I: fixed(name), Fn: fn}, opt)
	}
	caps := src.Cmd.Caps

	add("myjobs", state.Options{Timeout: state.TimeoutMyJobs}, func(ctx context.Context) (any, error) {
		jobs, err := src.MyJobs(ctx)
		if err == nil {
			parts := state.PartitionsOfPending(jobs)
			prev := vs.get().pendingParts
			running := runningToSample(jobs)
			vs.set(func(v *viewState) { v.pendingParts, v.running = parts, running })
			// Sources gated on pending jobs start as soon as there are some.
			if len(parts) > 0 && !slices.Equal(prev, parts) {
				s.Refresh("queuerank", "sprio")
			}
		}
		return jobs, err
	})
	// Everyone's jobs: only while the Queue tab is open, and every minute
	// at most on large clusters.
	var allLarge bool
	var allMu sync.Mutex
	s.Add(state.Func{N: "alljobs", I: func() time.Duration {
		allMu.Lock()
		defer allMu.Unlock()
		if allLarge {
			return max(iv.Get("alljobs"), time.Minute)
		}
		return iv.Get("alljobs")
	}, Fn: func(ctx context.Context) (any, error) {
		jobs, err := src.AllJobs(ctx)
		if err == nil {
			pending := state.PendingJobs(jobs)
			vs.set(func(v *viewState) { v.fromAll, v.fromAllAt = pending, time.Now() })
		}
		allMu.Lock()
		allLarge = len(jobs) > state.LargeCluster
		allMu.Unlock()
		return jobs, err
	}}, state.Options{Timeout: state.TimeoutCluster, Tabs: []string{model.TabQueue}, OnlyVisible: true})
	var large bool
	var mu sync.Mutex
	s.Add(state.Func{N: "cluster", I: func() time.Duration {
		mu.Lock()
		defer mu.Unlock()
		if large {
			return max(iv.Get("cluster"), time.Minute)
		}
		return iv.Get("cluster")
	}, Fn: func(ctx context.Context) (any, error) {
		d, err := src.Cluster(ctx, nil)
		mu.Lock()
		large = len(d.Jobs) > state.LargeCluster
		mu.Unlock()
		return d, err
	}}, state.Options{Timeout: state.TimeoutCluster, Tabs: []string{model.TabOverview, model.TabNodes}, HiddenMin: time.Minute})
	add("queuerank", state.Options{Timeout: state.TimeoutQueueRank, Active: func() bool { return len(vs.get().pendingParts) > 0 }},
		func(ctx context.Context) (any, error) {
			st := vs.get()
			if time.Since(st.fromAllAt) < time.Minute {
				// Everyone's jobs are fresh: rank from them, no query.
				return state.PendingIn(st.fromAll, st.pendingParts), nil
			}
			return src.QueueRank(ctx, st.pendingParts)
		})
	add("nodes", state.Options{Timeout: state.TimeoutNodes}, func(ctx context.Context) (any, error) { return src.Nodes(ctx) })
	add("partitions", state.Options{Timeout: state.TimeoutPartitions}, func(ctx context.Context) (any, error) { return src.Partitions(ctx) })
	add("reservations", state.Options{Timeout: state.TimeoutPartitions}, func(ctx context.Context) (any, error) { return src.Reservations(ctx) })
	add("jobdetail", state.Options{Timeout: state.TimeoutDetail, Active: func() bool { return vs.get().detailID != "" }},
		func(ctx context.Context) (any, error) {
			id := vs.get().detailID
			d := state.Detail{ID: id}
			job, err := src.JobDetail(ctx, id)
			if err != nil {
				return d, err
			}
			d.Job = job
			if job != nil && job.State == model.StateRunning && caps.HasSstat && !site.NoUsageGather() {
				// Usage is best effort: a job without steps has none yet.
				d.Stat, _ = src.JobStat(ctx, id)
			}
			return d, nil
		})
	if caps.HasSacct {
		add("history", state.Options{Timeout: state.TimeoutHistory, Tabs: []string{model.TabUsage}, HiddenMin: 30 * time.Minute},
			func(ctx context.Context) (any, error) {
				days := vs.get().historyDays
				if days == 0 {
					days = 7
				}
				return src.History(ctx, days)
			})
	}
	if caps.HasSacct && !site.AccountingOff() {
		// Limits change rarely: read them only while the Usage tab is open.
		add("limits", state.Options{Timeout: state.TimeoutLimits, Tabs: []string{model.TabUsage}, OnlyVisible: true},
			func(ctx context.Context) (any, error) { return src.Limits(ctx) })
	}
	if caps.HasSstat && !site.NoUsageGather() {
		// Live use of long-running jobs, for the idle-job warning: one
		// sstat call per StatBatch jobs, only when there are such jobs.
		add("mystats", state.Options{
			Timeout: state.TimeoutStats,
			Active:  func() bool { st := vs.get(); return st.warnWaste && len(st.running) > 0 },
		}, func(ctx context.Context) (any, error) { return src.MyStats(ctx, vs.get().running) })
	}
	if caps.HasSshare {
		add("fairshare", state.Options{Timeout: state.TimeoutFairshare}, func(ctx context.Context) (any, error) { return src.Fairshare(ctx) })
	}
	if caps.HasSprio {
		add("sprio", state.Options{
			Timeout: state.TimeoutFairshare, Tabs: []string{model.TabJobs}, HiddenMin: 10 * time.Minute,
			Active: func() bool { return len(vs.get().pendingParts) > 0 },
		},
			func(ctx context.Context) (any, error) { return src.Priorities(ctx) })
	}
	if storageFn != nil {
		add("storage", state.Options{Timeout: 2 * time.Minute}, func(ctx context.Context) (any, error) { return storageFn(ctx) })
	}
}

// asciiLocale reports a non-UTF-8 locale to enable plain symbols.
func asciiLocale(getenv func(string) string) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(k); v != "" {
			v = strings.ToLower(v)
			return !strings.Contains(v, "utf-8") && !strings.Contains(v, "utf8")
		}
	}
	return false
}

// isTerminal reports whether v is a character device (a terminal).
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runDashboard starts the TUI.
func (a *app) runDashboard(ctx context.Context) error {
	if !isTerminal(a.env.Stdin) || !isTerminal(a.env.Stdout) {
		return usageError{fmt.Errorf("the dashboard needs an interactive terminal; for scripts use sdash status, jobs or nodes (with --json)")}
	}
	cfg := a.loadConfig()
	clock := state.RealClock{}
	isDemo := a.sim != nil
	rt, err := a.slurmRuntime(ctx)
	if err != nil {
		return err
	}
	check := a.quotas(rt.cfg, rt.sources.Cmd.User)
	rt.store.Trends = a.openTrends(isDemo)
	storageFn := func(ctx context.Context) ([]model.Quota, error) {
		q := check(ctx)
		a.recordTrends(rt.store.Trends, q)
		return q, nil
	}

	vs := &viewState{historyDays: 7, warnWaste: cfg.WarnWaste}
	sched := state.NewScheduler(clock, uint64(time.Now().UnixNano()))
	live := state.NewIntervals(intervalMap(cfg.Intervals()))
	sched.SetManual(cfg.Intervals().Manual)
	addCollectors(sched, rt.sources, rt.store.Site, live, vs, storageFn)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sched.Start(runCtx)

	var history func() []execx.CallRecord
	if h, ok := rt.runner.(interface{ History() []execx.CallRecord }); ok {
		history = h.History
	}
	opt := ui.Options{
		Config: cfg, Store: rt.store, Scheduler: sched, Runner: rt.runner, Sources: rt.sources,
		History: history, Warnings: rt.sources.WarningCounts,
		ASCII: asciiLocale(a.env.Getenv), NoColor: a.env.Getenv("NO_COLOR") != "", Demo: isDemo,
		StateDir: a.stateDir(isDemo), CacheDir: a.paths.CacheDir, Log: a.log, Now: time.Now,
		Interval: live.Get, OnViewState: vs.onViewState, Getenv: a.env.Getenv, LogFS: a.logFS(),
		OnConfig: func(c config.Config) {
			vs.set(func(v *viewState) { v.warnWaste = c.WarnWaste })
			sched.Nudge()
		},
		ApplyRefresh: func(iv config.Intervals) {
			live.Set(intervalMap(iv))
			sched.SetManual(iv.Manual)
			sched.Nudge()
		},
	}
	if !isDemo {
		opt.ConfigPath, opt.ConfigLayers = a.configPath(), a.configLayers()
	}
	if isDemo {
		opt.DuRunner, opt.HasIonice = rt.runner, true
	} else {
		opt.DuRunner = execx.NewReal(execx.Options{Logger: a.log, MaxConcurrent: 1, DefaultTimeout: storage.DuTimeout})
		_, err := execx.LookPath("ionice")
		opt.HasIonice = err == nil
	}
	if hook := cfg.NotifyCommand; hook != "" && !isDemo {
		hr := execx.NewReal(execx.Options{Logger: a.log, MaxConcurrent: 1, DefaultTimeout: notify.HookTimeout})
		opt.Hook = func(e notify.Event) error { return notify.RunHook(runCtx, hr, hook, e) }
	}
	if !isDemo {
		opt.Doctor = func() string {
			checks, _ := a.doctor(runCtx)
			var b strings.Builder
			_ = renderChecks(&b, checks)
			return strings.TrimRight(b.String(), "\n")
		}
	}
	err = ui.Run(runCtx, opt)
	var crash *ui.CrashError
	if errors.As(err, &crash) {
		_, _ = fmt.Fprintf(a.env.Stderr, "%v\nPlease attach it to a bug report.\n", err)
		return errSilent
	}
	return err
}

// stateDir is where the UI keeps its state; demo mode keeps none.
func (a *app) stateDir(isDemo bool) string {
	if isDemo || a.pathsErr != nil {
		return ""
	}
	return a.paths.StateDir
}

// openTrends loads the storage history: from the state directory, or a made
// up one in demo mode. It is never a reason to fail.
func (a *app) openTrends(isDemo bool) *insights.Log {
	now := a.clock()
	if isDemo && a.sim != nil {
		return insights.New(a.sim.TrendSamples(now), now)
	}
	if dir := a.stateDir(isDemo); dir != "" {
		return insights.Open(filepath.Join(dir, insights.FileName), now)
	}
	return insights.Open("", now)
}

// recordTrends adds a reading of every location to the history and keeps it
// in the state directory.
func (a *app) recordTrends(l *insights.Log, quotas []model.Quota) {
	if err := l.Persist(l.Add(quotas, a.clock())); err != nil {
		a.log.Warn("storage history not saved", "err", err)
	}
}

// statAge is how long a job must have run before it is sampled for the
// idle-job warning (the warning itself waits for insights.IdleAfter, so a
// sample is ready when it is judged).
const statAge = 25 * time.Minute

// maxSampled bounds how many jobs the idle-job warning samples.
const maxSampled = 3 * state.StatBatch

// runningToSample lists the user's plain running jobs old enough to judge,
// longest running first.
func runningToSample(jobs []model.Job) []string {
	var old []model.Job
	for _, j := range jobs {
		if j.State == model.StateRunning && j.TimeUsed >= statAge && j.ID.Raw != "" && strings.Trim(j.ID.Raw, "0123456789") == "" {
			old = append(old, j)
		}
	}
	slices.SortStableFunc(old, func(a, b model.Job) int { return cmp.Compare(b.TimeUsed, a.TimeUsed) })
	ids := make([]string, 0, min(len(old), maxSampled))
	for _, j := range old[:min(len(old), maxSampled)] {
		ids = append(ids, j.ID.Raw)
	}
	return ids
}
