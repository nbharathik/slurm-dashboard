package insights

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// Input is everything the alert rules look at. Missing data (nil slices)
// simply produces no alerts of that kind.
type Input struct {
	Now          time.Time
	UID          string // numeric user ID, for "cancelled by you"
	MyJobs       []model.Job
	Nodes        []model.Node
	Partitions   []model.Partition
	Reservations []model.Reservation
	History      []model.HistoryJob // recent finished jobs (any range)
	Ended        []model.HistoryJob // jobs seen ending in this session
	Storage      []model.Quota
	// Stats is the latest sstat sample of running jobs by job ID, taken at
	// StatsAt; Waste says the user wants the idle-job warning.
	Stats     map[string]model.JobStat
	StatsAt   time.Time
	Waste     bool
	Dismissed map[string]time.Time // key → dismissed until
	// StorageWarn and StorageCrit are the [alerts] thresholds in percent;
	// zero means the defaults.
	StorageWarn, StorageCrit int
	// TimeLeftWarn is how close to its time limit a running job is warned
	// about: zero means TimeLeftWarn's default, negative means never.
	TimeLeft time.Duration
}

// Default alert thresholds.
const (
	DefaultStorageWarn = 90
	DefaultStorageCrit = 97
	TimeLeftWarn       = 10 * time.Minute
	IdleAfter          = 30 * time.Minute // a job is judged idle only after this long
	IdleCPU            = 0.20             // under this share of its CPUs busy
	IdleMem            = 0.10             // under this share of its memory used (peak)
	IdleGPU            = 0.20             // under this GPU utilisation
	StatFresh          = 15 * time.Minute // an sstat sample older than this is ignored
	FailureWindow      = 24 * time.Hour
	MaintWindow        = 7 * 24 * time.Hour
	MaintWarn          = 24 * time.Hour
	LowEffJobs         = 5
	LowEffMedian       = 0.25
	DismissFor         = 24 * time.Hour
	maxFailedShown     = 5
)

// Compute returns the active alerts, most severe first.
func Compute(in Input) []model.Alert {
	var out []model.Alert
	add := func(a model.Alert) {
		if until, ok := in.Dismissed[a.Key]; ok && in.Now.Before(until) {
			return
		}
		out = append(out, a)
	}
	for _, a := range storage(in) {
		add(a)
	}
	for _, a := range failures(in) {
		add(a)
	}
	for _, a := range pending(in) {
		add(a)
	}
	for _, a := range timeLeft(in) {
		add(a)
	}
	for _, a := range idleJobs(in) {
		add(a)
	}
	for _, a := range nodes(in) {
		add(a)
	}
	for _, a := range maintenance(in) {
		add(a)
	}
	if a, ok := lowEfficiency(in); ok {
		add(a)
	}
	slices.SortStableFunc(out, func(a, b model.Alert) int { return cmp.Compare(b.Level, a.Level) })
	return out
}

// Thresholds returns the storage warning and critical percentages, with
// defaults for unset values.
func Thresholds(warn, crit int) (int, int) {
	if warn <= 0 {
		warn = DefaultStorageWarn
	}
	if crit <= 0 {
		crit = DefaultStorageCrit
	}
	return warn, crit
}

// StorageLevel is the alert level for a usage percentage.
func StorageLevel(alertPercent, warn, crit int) (model.Level, bool) {
	warn, crit = Thresholds(warn, crit)
	switch {
	case alertPercent >= crit:
		return model.Crit, true
	case alertPercent >= warn:
		return model.Warn, true
	}
	return model.Info, false
}

func storage(in Input) []model.Alert {
	var out []model.Alert
	for _, q := range in.Storage {
		u := q.Usage()
		level, ok := StorageLevel(u.Pct, in.StorageWarn, in.StorageCrit)
		if !ok {
			continue
		}
		msg := fmt.Sprintf("%s is at %d%% of its %s quota", q.Label, u.Pct, u.What)
		if q.IsFilesystemTotal {
			// No personal quota: the whole shared filesystem is filling up,
			// which the user can rarely fix alone.
			level = model.Info
			msg = fmt.Sprintf("%s: the shared filesystem is %d%% full (no personal quota)", q.Label, u.Pct)
			if u.What == "files" {
				msg = fmt.Sprintf("%s: the shared filesystem has used %d%% of its files (no personal quota)", q.Label, u.Pct)
			}
		}
		if q.Grace != "" && q.Grace != "none" && q.Grace != "-" {
			msg += " (grace " + q.Grace + ")"
		}
		out = append(out, model.Alert{Level: level, Kind: "storage", Key: "storage:" + q.Label, Message: msg, Tab: model.TabStorage})
	}
	return out
}

func failures(in Input) []model.Alert {
	seen := map[string]bool{}
	var jobs []model.HistoryJob
	for _, list := range [][]model.HistoryJob{in.Ended, in.History} {
		for _, h := range list {
			if seen[h.ID.Raw] || !isFailure(h.State) {
				continue
			}
			end := h.End
			if end.IsZero() || in.Now.Sub(end) > FailureWindow {
				continue
			}
			seen[h.ID.Raw] = true
			jobs = append(jobs, h)
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].End.After(jobs[j].End) })
	var out []model.Alert
	for i, h := range jobs {
		if i == maxFailedShown {
			out = append(out, model.Alert{
				Level: model.Crit, Kind: "job-failed", Key: "job-failed:more:" + h.ID.Raw,
				Message: fmt.Sprintf("%d more jobs failed in the last 24 h", len(jobs)-maxFailedShown), Tab: model.TabUsage,
			})
			break
		}
		msg := fmt.Sprintf("%s %s: %s", h.ID.Raw, h.Name, h.State)
		switch {
		case h.State == model.StateOOM && h.PeakMemMB > 0 && h.AllocMemMB > 0:
			msg += fmt.Sprintf(" (peak %s of %s)", units.FormatMB(float64(h.PeakMemMB)), units.FormatMB(float64(h.AllocMemMB)))
		case h.ExitCode != 0 || h.Signal != 0:
			if hint := ExplainFailure(h, in.UID); hint.Title != "" {
				msg += " (" + strings.ToLower(hint.Title[:1]) + hint.Title[1:] + ")"
			}
		}
		out = append(out, model.Alert{Level: model.Crit, Kind: "job-failed", Key: "job-failed:" + h.ID.Raw, Message: msg, JobID: h.ID.Raw, Tab: model.TabUsage})
	}
	return out
}

func isFailure(s model.JobState) bool {
	switch s {
	case model.StateFailed, model.StateOOM, model.StateTimeout, model.StateNodeFail:
		return true
	}
	return false
}

func pending(in Input) []model.Alert {
	var out []model.Alert
	for _, j := range in.MyJobs {
		if j.State != model.StatePending {
			continue
		}
		p := Explain(j.Reason)
		if !p.Never {
			continue
		}
		out = append(out, model.Alert{
			Level: model.Crit, Kind: "unsatisfiable", Key: "unsatisfiable:" + j.ID.Raw,
			Message: fmt.Sprintf("%s %s can never start: %s", j.ID.Raw, j.Name, p.Code), JobID: j.ID.Raw, Tab: model.TabJobs,
		})
	}
	return out
}

func timeLeft(in Input) []model.Alert {
	warn := in.TimeLeft
	if warn == 0 {
		warn = TimeLeftWarn
	}
	if warn < 0 {
		return nil
	}
	var out []model.Alert
	for _, j := range in.MyJobs {
		if j.State != model.StateRunning || j.TimeLeft == nil || *j.TimeLeft >= warn {
			continue
		}
		out = append(out, model.Alert{
			Level: model.Warn, Kind: "timelimit", Key: "timelimit:" + j.ID.Raw,
			Message: fmt.Sprintf("%s %s has %s left: checkpoint or save now", j.ID.Raw, j.Name, units.FormatShort(*j.TimeLeft)),
			JobID:   j.ID.Raw, Tab: model.TabJobs,
		})
	}
	return out
}

// idleJobs notes running jobs that, after IdleAfter, leave most of what
// they asked for unused: under IdleCPU of the CPUs (of jobs with at least
// two), under IdleMem of the memory (of requests of a gigabyte or more) or
// under IdleGPU of the GPU time when the site records it. Array tasks and
// heterogeneous jobs are not sampled.
func idleJobs(in Input) []model.Alert {
	if !in.Waste || len(in.Stats) == 0 || in.StatsAt.IsZero() || in.Now.Sub(in.StatsAt) > StatFresh {
		return nil
	}
	var out []model.Alert
	for _, j := range in.MyJobs {
		st, ok := in.Stats[j.ID.Raw]
		if !ok || j.State != model.StateRunning || j.TimeUsed < IdleAfter || !plainJob(j.ID.Raw) {
			continue
		}
		u := LiveEfficiency(j, st)
		var parts []string
		if u.CPU >= 0 && j.CPUs >= 2 && u.CPU < IdleCPU {
			parts = append(parts, fmt.Sprintf("%d%% of its %d CPUs", alertPercent(u.CPU), j.CPUs))
		}
		if req := j.MemPerNodeMB * int64(max(j.Nodes, 1)); u.Mem >= 0 && req >= 1024 && u.Mem < IdleMem {
			parts = append(parts, fmt.Sprintf("%d%% of its %s memory", alertPercent(u.Mem), units.FormatMB(float64(req))))
		}
		if u.GPU >= 0 && u.GPU < IdleGPU {
			parts = append(parts, fmt.Sprintf("its GPU only %d%% of the time", alertPercent(u.GPU)))
		}
		if len(parts) == 0 {
			continue
		}
		out = append(out, model.Alert{
			Level: model.Info, Kind: "waste", Key: "waste:" + j.ID.Raw,
			Message: fmt.Sprintf("%s %s uses %s after %s: ask for less next time", j.ID.Raw, j.Name, strings.Join(parts, ", "), units.FormatShort(j.TimeUsed)),
			JobID:   j.ID.Raw, Tab: model.TabJobs,
		})
	}
	return out
}

func alertPercent(f float64) int { return int(f*100 + 0.5) }

// plainJob reports whether id is a bare job number: not an array task
// ("8_3") or a heterogeneous component ("8+1"), which sstat cannot tell
// apart.
func plainJob(id string) bool {
	return id != "" && strings.Trim(id, "0123456789") == ""
}

func nodes(in Input) []model.Alert {
	byName := make(map[string]model.Node, len(in.Nodes))
	for _, n := range in.Nodes {
		byName[n.Name] = n
	}
	var out []model.Alert
	for _, j := range in.MyJobs {
		if j.State != model.StateRunning {
			continue
		}
		for _, name := range j.NodeList {
			n, ok := byName[name]
			if !ok {
				continue
			}
			bad := ""
			switch {
			case n.State == "DOWN" || n.HasFlag("NOT_RESPONDING"):
				bad = "is down"
			case strings.HasPrefix(n.State, "DRAIN") || n.HasFlag("DRAIN"):
				bad = "is draining"
			default:
				continue
			}
			msg := fmt.Sprintf("Node %s running job %s %s", name, j.ID.Raw, bad)
			if n.Reason != "" {
				msg += ": " + n.Reason
			}
			out = append(out, model.Alert{Level: model.Warn, Kind: "node", Key: "node:" + name + ":" + j.ID.Raw, Message: msg, JobID: j.ID.Raw, Tab: model.TabNodes})
		}
	}
	return out
}

// myPartitions are the partitions of the user's queued and recent jobs.
func myPartitions(in Input) map[string]bool {
	parts := map[string]bool{}
	for _, j := range in.MyJobs {
		for _, p := range strings.Split(j.Partition, ",") {
			parts[p] = p != ""
		}
	}
	for _, h := range in.History {
		parts[h.Partition] = h.Partition != ""
	}
	return parts
}

func maintenance(in Input) []model.Alert {
	parts := myPartitions(in)
	if len(parts) == 0 {
		return nil
	}
	myNodes := map[string]bool{}
	for _, p := range in.Partitions {
		if parts[p.Name] {
			for _, n := range p.Nodes {
				myNodes[n] = true
			}
		}
	}
	var out []model.Alert
	for _, r := range in.Reservations {
		if !r.IsMaintenance() || r.Start.IsZero() {
			continue
		}
		until := r.Start.Sub(in.Now)
		if until > MaintWindow || (until < 0 && in.Now.After(r.End)) {
			continue
		}
		if len(myNodes) > 0 && len(r.Nodes) > 0 && !overlaps(r.Nodes, myNodes) {
			continue
		}
		level := model.Info
		if until < MaintWarn {
			level = model.Warn
		}
		var msg string
		if until <= 0 {
			msg = fmt.Sprintf("Maintenance %s is under way until %s", r.Name, r.End.Local().Format("Jan 2 15:04"))
		} else {
			msg = fmt.Sprintf("Maintenance starts in %s; jobs longer than %s will wait", units.FormatShort(until), hoursOf(until))
		}
		out = append(out, model.Alert{Level: level, Kind: "maintenance", Key: "maintenance:" + r.Name, Message: msg, Tab: model.TabNodes})
	}
	return out
}

func hoursOf(d time.Duration) string {
	h := int(d.Hours())
	if h < 1 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", h)
}

func overlaps(nodes []string, set map[string]bool) bool {
	for _, n := range nodes {
		if set[n] {
			return true
		}
	}
	return false
}

// lowEfficiency fires once a day when the median memory efficiency of the
// last five completed jobs is under 25%.
func lowEfficiency(in Input) (model.Alert, bool) {
	var done []model.HistoryJob
	for _, h := range in.History {
		if h.State == model.StateCompleted && h.Eff.Mem >= 0 && !h.End.IsZero() {
			done = append(done, h)
		}
	}
	if len(done) < LowEffJobs {
		return model.Alert{}, false
	}
	sort.SliceStable(done, func(i, j int) bool { return done[i].End.After(done[j].End) })
	effs := make([]float64, LowEffJobs)
	for i := range effs {
		effs[i] = done[i].Eff.Mem
	}
	median := Median(effs)
	if median >= LowEffMedian {
		return model.Alert{}, false
	}
	return model.Alert{
		Level: model.Info, Kind: "low-eff", Key: "low-eff:" + in.Now.Local().Format("2006-01-02"),
		Message: fmt.Sprintf("Your last %d jobs used a median %d%% of the memory they requested; see Usage for --mem suggestions", LowEffJobs, int(median*100+0.5)),
		Tab:     model.TabUsage,
	}, true
}
