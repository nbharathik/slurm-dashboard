package parse

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// sstatRow is one step of "sstat -a -n -P -o JobID,NTasks,MaxRSS,AveRSS,AveCPU,TRESUsageInTot".
type sstatRow struct {
	job     string // the job part of "14.batch"
	tasks   int
	maxRSS  int64
	aveRSS  int64
	aveCPU  time.Duration
	gpuUtil float64
	hasUtil bool
}

func parseSstatRow(w *warnings, n int, line string) (sstatRow, bool) {
	f := cleanFields(strings.Split(line, "|"))
	if len(f) != 6 {
		w.add(n, line, "expected 6 fields, got %d", len(f))
		return sstatRow{}, false
	}
	var fe fieldErr
	r := sstatRow{tasks: fe.atoi(f[1])}
	r.job, _, _ = strings.Cut(f[0], ".")
	tres := units.ParseTRES(f[5])
	var err error
	r.maxRSS, err = units.MemMB(f[2], 0)
	fe.set(err)
	if r.maxRSS == 0 {
		r.maxRSS, err = units.MemMB(tres["mem"], 0)
		fe.set(err)
	}
	r.aveRSS, err = units.MemMB(f[3], 0)
	fe.set(err)
	r.aveCPU, _, err = units.ParseDuration(f[4])
	fe.set(err)
	if u, ok := tres["gres/gpuutil"]; ok {
		if v, err := strconv.ParseFloat(u, 64); err == nil && v >= 0 {
			r.gpuUtil, r.hasUtil = math.Min(v/100, 1), true
		}
	}
	if fe.err != nil {
		w.add(n, line, "%v", fe.err)
		return sstatRow{}, false
	}
	return r, true
}

// fold adds one step to a job's usage: tasks are summed, memory and CPU
// take the largest step.
func (s *sstatRow) foldInto(stat *model.JobStat) {
	stat.NTasks += s.tasks
	stat.MaxRSSMB = max(stat.MaxRSSMB, s.maxRSS)
	stat.AveRSSMB = max(stat.AveRSSMB, s.aveRSS)
	stat.AveCPU = max(stat.AveCPU, s.aveCPU)
	stat.TotalCPU += s.aveCPU * time.Duration(max(s.tasks, 1))
	if s.hasUtil {
		stat.GPUUtil, stat.HasGPUUtil = max(stat.GPUUtil, s.gpuUtil), true
	}
}

// Sstat parses "sstat -a -n -P -j <id> -o JobID,NTasks,MaxRSS,AveRSS,AveCPU,TRESUsageInTot"
// and folds all steps into one JobStat: tasks are summed, memory and CPU
// take the largest step. It returns nil when there are no steps.
func Sstat(raw []byte) (stat *model.JobStat, warns []model.ParseWarning) {
	w := &warnings{source: "jobstat"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		r, ok := parseSstatRow(w, n, line)
		if !ok {
			return
		}
		if stat == nil {
			stat = &model.JobStat{}
		}
		r.foldInto(stat)
	})
	return stat, w.list
}

// SstatJobs parses the same output for several jobs at once ("sstat -j
// 1,2,3") and folds each job's steps into one JobStat, keyed by job ID.
// Jobs without steps are absent.
func SstatJobs(raw []byte) (stats map[string]*model.JobStat, warns []model.ParseWarning) {
	w := &warnings{source: "jobstat"}
	defer func() { warns = w.list }()
	defer w.recover()

	stats = map[string]*model.JobStat{}
	lines(raw, w, func(n int, line string) {
		r, ok := parseSstatRow(w, n, line)
		if !ok || r.job == "" {
			return
		}
		stat := stats[r.job]
		if stat == nil {
			stat = &model.JobStat{}
			stats[r.job] = stat
		}
		r.foldInto(stat)
	})
	return stats, w.list
}
