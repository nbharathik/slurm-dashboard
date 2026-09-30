package parse

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// HistoryFields is the number of fields in the history sacct format.
const HistoryFields = 20

// History parses the history query:
//
//	sacct -n -P --delimiter=<Sep> -u $USER -S ... -E now -o JobID,JobIDRaw,JobName,
//	  Partition,State,ExitCode,Submit,Start,End,ElapsedRaw,TimelimitRaw,AllocCPUS,
//	  TotalCPU,ReqMem,MaxRSS,TRESUsageInTot,AllocTRES,NNodes,NodeList,WorkDir
//
// Step lines (123.batch, 123.0) are folded into their job: peak memory is
// the largest step's TRESUsageInTot mem (else MaxRSS), and TotalCPU falls
// back to the sum of the steps. Efficiency is computed for every job.
func History(raw []byte) (jobs []model.HistoryJob, warns []model.ParseWarning) {
	w := &warnings{source: "history"}
	defer func() { warns = w.list }()
	defer w.recover()

	index := map[string]int{}
	gpuUtil := map[string]float64{}
	stepCPU := map[string]time.Duration{}

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.Split(line, Sep))
		if len(f) != HistoryFields {
			w.add(n, line, "expected %d fields, got %d", HistoryFields, len(f))
			return
		}
		id, err := units.ParseJobID(f[0])
		if err != nil {
			w.add(n, line, "%v", err)
			return
		}
		var fe fieldErr

		if id.Step != "" {
			parent := strings.SplitN(f[0], ".", 2)[0]
			i, ok := index[parent]
			if !ok {
				return // step without its job (outside the time window)
			}
			usage := units.ParseTRES(f[15])
			peak, err := units.MemMB(usage["mem"], 0)
			fe.set(err)
			if peak == 0 {
				peak, err = units.MemMB(f[14], 0)
				fe.set(err)
			}
			if peak > jobs[i].PeakMemMB {
				jobs[i].PeakMemMB = peak
			}
			if u, ok := usage["gres/gpuutil"]; ok {
				if v, err := strconv.ParseFloat(u, 64); err == nil {
					gpuUtil[parent] = math.Max(gpuUtil[parent], v/100)
				}
			}
			cpu, _, err := units.ParseDuration(f[12])
			fe.set(err)
			stepCPU[parent] += cpu
			if fe.err != nil {
				w.add(n, line, "%v", fe.err)
			}
			return
		}

		state, by := units.ParseJobState(f[4])
		code, sig, err := units.ParseExitCode(f[5])
		fe.set(err)
		elapsed, err := units.ParseSeconds(f[9])
		fe.set(err)
		limit, err := units.ParseMinutesLimit(f[10])
		fe.set(err)
		totalCPU, _, err := units.ParseDuration(f[12])
		fe.set(err)
		alloc := units.ParseTRES(f[16])
		allocCPUs := fe.atoi(f[11])
		allocMem, err := units.MemMB(alloc["mem"], allocCPUs)
		fe.set(err)
		if allocMem == 0 {
			allocMem, err = units.MemMB(f[13], allocCPUs)
			fe.set(err)
		}
		gpus, gpuType := units.GPUsFromTRES(alloc)
		nodes, err := units.ExpandHostlist(f[18])
		fe.set(err)

		h := model.HistoryJob{
			ID:             id,
			Name:           f[2],
			Partition:      f[3],
			State:          state,
			CancelledByUID: by,
			ExitCode:       code,
			Signal:         sig,
			Submit:         fe.time(f[6], units.ParseTimestamp),
			Start:          fe.time(f[7], units.ParseTimestamp),
			End:            fe.time(f[8], units.ParseTimestamp),
			Elapsed:        elapsed,
			TimeLimit:      limit,
			AllocCPUs:      allocCPUs,
			TotalCPU:       totalCPU,
			AllocMemMB:     allocMem,
			GPUs:           gpus,
			GPUType:        gpuType,
			Nodes:          fe.atoi(f[17]),
			NodeList:       nodes,
			WorkDir:        f[19],
		}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		index[f[0]] = len(jobs)
		jobs = append(jobs, h)
	})

	for key, i := range index {
		if jobs[i].TotalCPU == 0 {
			jobs[i].TotalCPU = stepCPU[key]
		}
		util, ok := gpuUtil[key]
		if !ok {
			util = -1
		}
		jobs[i].ComputeEfficiency(util)
	}
	return jobs, w.list
}

// FinalState is a job's outcome after it left the queue.
type FinalState struct {
	ID          string
	State       model.JobState
	CancelledBy string
	ExitCode    int
	Signal      int
	Elapsed     time.Duration
}

// FinalStates parses "sacct -n -P -X -j <ids> -o JobID,State,ExitCode,ElapsedRaw".
func FinalStates(raw []byte) (out []FinalState, warns []model.ParseWarning) {
	w := &warnings{source: "final"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.Split(line, "|"))
		if len(f) != 4 {
			w.add(n, line, "expected 4 fields, got %d", len(f))
			return
		}
		var fe fieldErr
		state, by := units.ParseJobState(f[1])
		code, sig, err := units.ParseExitCode(f[2])
		fe.set(err)
		el, err := units.ParseSeconds(f[3])
		fe.set(err)
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		out = append(out, FinalState{ID: f[0], State: state, CancelledBy: by, ExitCode: code, Signal: sig, Elapsed: el})
	})
	return out, w.list
}
