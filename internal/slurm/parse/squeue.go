package parse

import (
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// MyJobsFields is the number of fields in the jobs squeue format.
const MyJobsFields = jobFieldCount

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
		id, err := units.ParseJobID(f[fJobID])
		if err != nil {
			w.add(n, line, "%v", err)
			return
		}
		var fe fieldErr
		state, _ := units.ParseJobState(f[fState])
		// Repeated values are interned and the rest copied, so a large queue
		// keeps one copy of each user or partition and none of the raw lines.
		j := model.Job{
			ID:         ownID(id),
			Partition:  intern(f[fPartition]),
			QOS:        intern(f[fQOS]),
			Account:    intern(f[fAccount]),
			State:      model.JobState(intern(string(state))),
			Nodes:      fe.atoi(f[fNodes]),
			CPUs:       fe.atoi(f[fCPUs]),
			Reason:     intern(f[fReason]),
			SubmitTime: fe.time(f[fSubmit], units.ParseTimestamp),
			StartTime:  fe.time(f[fStart], units.ParseTimestamp),
			EndTime:    fe.time(f[fEnd], units.ParseTimestamp),
			Priority:   fe.atoi64(f[fPriority]),
			Dependency: own(nullToEmpty(f[fDependency])),
			User:       intern(f[fUser]),
			Name:       own(f[fName]),
		}
		used, _, err := units.ParseDuration(f[fTimeUsed])
		fe.set(err)
		j.TimeUsed = used
		j.TimeLimit, err = units.ParseLimit(f[fTimeLimit])
		fe.set(err)
		j.TimeLeft, err = units.ParseLimit(f[fTimeLeft])
		fe.set(err)
		mem, err := units.MemMB(f[fMem], 0)
		fe.set(err)
		j.MemPerNodeMB = mem
		gpus, typ := units.ParseGRES(f[fGRES])
		j.GPUs, j.GPUType = gpus*max(j.Nodes, 1), intern(typ)
		j.NodeList, err = units.ExpandHostlist(f[fNodeList])
		j.NodeList = internAll(j.NodeList)
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
			ID:        ownID(id),
			User:      intern(f[1]),
			Partition: intern(f[2]),
			NodeList:  internAll(nodes),
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
		p := PendingJob{ID: own(f[0]), Partition: intern(f[1]), Priority: fe.atoi64(f[2])}
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
		if len(f) < 4 {
			w.add(n, line, "expected 4 fields, got %d", len(f))
			return
		}
		var fe fieldErr
		// The reason is last and may hold spaces ("ReqNodeNotAvail, UnavailableNodes:...").
		reason := strings.Join(f[3:], " ")
		e := StartEstimate{ID: f[0], Start: fe.time(f[1], units.ParseTimestamp), Nodes: nullToEmpty(f[2]), Reason: nullToEmpty(reason)}
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
