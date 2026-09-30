package parse

import (
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// MyJobsFields is the number of fields in the jobs squeue format.
const MyJobsFields = 23

// MyJobs parses "squeue -h -o" with 23 Sep-separated fields; Name is last so it may contain anything.
func MyJobs(raw []byte) (jobs []model.Job, warns []model.ParseWarning) {
	w := &warnings{source: "myjobs"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.SplitN(line, Sep, MyJobsFields))
		if len(f) != MyJobsFields {
			w.add(n, line, "expected %d fields, got %d", MyJobsFields, len(f))
			return
		}
		id, err := units.ParseJobID(f[0])
		if err != nil {
			w.add(n, line, "%v", err)
			return
		}
		var fe fieldErr
		state, _ := units.ParseJobState(f[6])
		j := model.Job{
			ID:         id,
			Partition:  f[3],
			QOS:        f[4],
			Account:    f[5],
			State:      state,
			Nodes:      fe.atoi(f[10]),
			CPUs:       fe.atoi(f[11]),
			Reason:     f[14],
			SubmitTime: fe.time(f[18], units.ParseTimestamp),
			StartTime:  fe.time(f[16], units.ParseTimestamp),
			EndTime:    fe.time(f[17], units.ParseTimestamp),
			Priority:   fe.atoi64(f[19]),
			Dependency: nullToEmpty(f[20]),
			User:       f[21],
			Name:       f[22],
		}
		used, _, err := units.ParseDuration(f[7])
		fe.set(err)
		j.TimeUsed = used
		j.TimeLimit, err = units.ParseLimit(f[8])
		fe.set(err)
		j.TimeLeft, err = units.ParseLimit(f[9])
		fe.set(err)
		mem, err := units.MemMB(f[12], 0)
		fe.set(err)
		j.MemPerNodeMB = mem
		gpus, typ := units.ParseGRES(f[13])
		j.GPUs, j.GPUType = gpus*max(j.Nodes, 1), typ
		j.NodeList, err = units.ExpandHostlist(f[15])
		fe.set(err)
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		jobs = append(jobs, j)
	})
	return jobs, w.list
}

// Running parses the cluster-wide "squeue -t RUNNING -O" query; fields are whitespace-free.
func Running(raw []byte) (jobs []model.RunningJob, warns []model.ParseWarning) {
	w := &warnings{source: "cluster"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.Fields(line))
		if len(f) == 6 { // empty tres-alloc column
			f = append(f, "")
		}
		if len(f) != 7 {
			w.add(n, line, "expected 7 fields, got %d", len(f))
			return
		}
		id, err := units.ParseJobID(f[0])
		if err != nil {
			w.add(n, line, "%v", err)
			return
		}
		var fe fieldErr
		nodes, err := units.ExpandHostlist(f[4])
		fe.set(err)
		tres := units.ParseTRES(f[6])
		gpus, _ := units.GPUsFromTRES(tres)
		typed, _ := units.GPUsByTypeTRES(tres)
		mig := 0
		for _, c := range typed {
			if units.IsMIG(c.Type) {
				mig += c.N
			}
		}
		mem, _ := units.MemMB(tres["mem"], 0)
		rj := model.RunningJob{
			ID:        id,
			User:      f[1],
			Partition: f[2],
			NodeList:  nodes,
			EndTime:   fe.time(f[5], units.ParseTimestamp),
			GPUs:      max(gpus, mig),
			MIGSlices: mig,
			CPUs:      fe.atoi(tres["cpu"]),
			MemMB:     mem,
		}
		_ = fe.atoi(f[3])
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		jobs = append(jobs, rj)
	})
	return jobs, w.list
}

// PendingJob is one row of the queue-rank query.
type PendingJob struct {
	ID        string
	Partition string
	Priority  int64
}

// QueueRank parses
//
//	squeue -h -t PENDING -p <parts> -O "JobID:48,Partition:64,PriorityLong:24"
func QueueRank(raw []byte) (jobs []PendingJob, warns []model.ParseWarning) {
	w := &warnings{source: "queuerank"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.Fields(line))
		if len(f) != 3 {
			w.add(n, line, "expected 3 fields, got %d", len(f))
			return
		}
		var fe fieldErr
		p := PendingJob{ID: f[0], Partition: f[1], Priority: fe.atoi64(f[2])}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		jobs = append(jobs, p)
	})
	return jobs, w.list
}

// StartEstimate is the scheduler's expected start of a pending job.
type StartEstimate struct {
	ID     string
	Start  time.Time // zero when Slurm has no estimate yet
	Nodes  string    // the nodes it expects to use; "" when unknown
	Reason string
}

// StartEstimates parses
//
//	squeue --start -h -j <id> -O "JobID:48,StartTime:24,SchedNodes:256,Reason:64"
func StartEstimates(raw []byte) (out []StartEstimate, warns []model.ParseWarning) {
	w := &warnings{source: "start"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.Fields(line))
		if len(f) != 4 {
			w.add(n, line, "expected 4 fields, got %d", len(f))
			return
		}
		var fe fieldErr
		e := StartEstimate{ID: f[0], Start: fe.time(f[1], units.ParseTimestamp), Nodes: nullToEmpty(f[2]), Reason: nullToEmpty(f[3])}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		out = append(out, e)
	})
	return out, w.list
}

func nullToEmpty(s string) string {
	switch strings.TrimSpace(s) {
	case "(null)", "N/A", "None":
		return ""
	}
	return strings.TrimSpace(s)
}
