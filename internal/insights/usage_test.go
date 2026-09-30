package insights

import (
	"math"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

var usageEpoch = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// job builds a finished job: elapsed hours, CPUs, CPU efficiency, memory
// (allocated and peak MB), GPUs with type and utilisation (-1 = not recorded).
func job(id string, st model.JobState, part string, hours float64, cpus int, cpuEff float64, allocMB, peakMB int64, gpus int, gpuType string, util float64) model.HistoryJob {
	el := time.Duration(hours * float64(time.Hour))
	limit := 2 * el
	h := model.HistoryJob{
		ID: model.JobID{Raw: id}, Name: "job" + id, Partition: part, State: st, Elapsed: el, TimeLimit: &limit,
		AllocCPUs: cpus, AllocMemMB: allocMB, PeakMemMB: peakMB, GPUs: gpus, GPUType: gpuType,
		End: usageEpoch.Add(time.Duration(len(id)) * time.Minute),
	}
	h.TotalCPU = time.Duration(cpuEff * float64(el) * float64(cpus))
	h.ComputeEfficiency(util)
	return h
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestSummarise(t *testing.T) {
	jobs := []model.HistoryJob{
		job("1", model.StateCompleted, "cpu", 2, 8, 0.5, 16384, 4096, 0, "", -1),
		job("22", model.StateCompleted, "gpu", 4, 16, 0.25, 65536, 16384, 2, "h200", 0.5),
		job("333", model.StateCompleted, "gpu", 1, 4, 1.0, 8192, 8192, 1, "h200", -1),
		job("4444", model.StateOOM, "cpu", 1, 4, 0.9, 8192, 8192, 0, "", -1),
		job("5", model.StateOOM, "cpu", 1, 4, 0.9, 8192, 8192, 0, "", -1),
		job("66", model.StateTimeout, "gpu", 3, 8, 0.1, 8192, 1024, 1, "a100", 0.1),
		job("7", model.StateCancelled, "cpu", 0.5, 2, 0.0, 4096, 0, 0, "", -1),
		{ID: model.JobID{Raw: "8"}, State: model.StateRunning, Partition: "cpu", Elapsed: time.Hour, AllocCPUs: 100},
	}
	s := Summarise(jobs, 7, "1000")

	if s.Days != 7 || s.Finished != 7 || s.Failed != 3 || !near(s.FailRate, 3.0/7) {
		t.Fatalf("counts: %+v", s)
	}
	if s.Outcomes[0].State != model.StateCompleted || s.Outcomes[0].Count != 3 || s.Outcomes[1].State != model.StateOOM || s.Outcomes[1].Count != 2 {
		t.Errorf("outcomes = %+v", s.Outcomes)
	}
	if s.TopFailure.State != model.StateOOM || s.TopFailure.Count != 2 || s.TopFailure.Job != "4444" || s.TopFailure.Hint == "" {
		t.Errorf("top failure = %+v", s.TopFailure)
	}

	// CPU-hours: 16 + 64 + 4 + 4 + 4 + 24 + 1 = 117 (the running job is left out).
	if !near(s.CPUHours, 117) {
		t.Errorf("CPU-hours = %v, want 117", s.CPUHours)
	}
	// GPU-hours: 8 + 1 + 3 = 12.
	if !near(s.GPUHours, 12) {
		t.Errorf("GPU-hours = %v, want 12", s.GPUHours)
	}
	if len(s.ByPartition) != 2 || s.ByPartition[0].Partition != "gpu" || s.ByPartition[0].Jobs != 3 || !near(s.ByPartition[0].CPUHours, 92) || !near(s.ByPartition[0].GPUHours, 12) {
		t.Errorf("by partition = %+v", s.ByPartition)
	}
	if len(s.ByGPUType) != 2 || s.ByGPUType[0].Type != "h200" || !near(s.ByGPUType[0].Hours, 9) || s.ByGPUType[1].Type != "a100" {
		t.Errorf("by GPU type = %+v", s.ByGPUType)
	}

	// Waste counts completed jobs only.
	w := s.Waste
	if !near(w.CPUHeld, 16+64+4) || !near(w.GPUHeld, 8) {
		t.Errorf("held = %v CPU-h, %v GPU-h", w.CPUHeld, w.GPUHeld)
	}
	if w.CPUJobs != 3 || !near(w.IdleCPUHours, 8+48+0) {
		t.Errorf("idle CPU = %d jobs, %v h; want 3 jobs, 56 h", w.CPUJobs, w.IdleCPUHours)
	}
	if w.MemJobs != 3 || !near(w.IdleMemGBh, (12288.0/1024)*2+(49152.0/1024)*4) {
		t.Errorf("idle memory = %d jobs, %v GB-h", w.MemJobs, w.IdleMemGBh)
	}
	if w.GPUJobs != 1 || !near(w.IdleGPUHours, 4) {
		t.Errorf("idle GPU = %d jobs, %v h; want 1 job, 4 h", w.GPUJobs, w.IdleGPUHours)
	}

	// The worst job is the 4-hour GPU job (48 idle CPU-hours + 4 idle GPU-hours).
	if len(s.Worst) != 2 || s.Worst[0].Job.ID.Raw != "22" || !near(s.Worst[0].Hours(), 52) || len(s.Worst[0].Suggestions) == 0 {
		t.Errorf("worst = %+v", s.Worst)
	}
}

func TestSummariseEmptyAndCapped(t *testing.T) {
	s := Summarise(nil, 7, "1000")
	if s.Finished != 0 || s.FailRate != 0 || s.TopFailure.Count != 0 || len(s.Worst) != 0 {
		t.Errorf("empty = %+v", s)
	}
	var many []model.HistoryJob
	for i := range 12 {
		many = append(many, job(string(rune('a'+i)), model.StateCompleted, "cpu", 1, 4, 0.1, 8192, 1024, 0, "", -1))
	}
	if got := len(Summarise(many, 30, "1000").Worst); got != WorstJobs {
		t.Errorf("worst list has %d jobs, want %d", got, WorstJobs)
	}
}

// A site that does not record GPU utilisation has no idle GPU figure.
func TestSummariseWithoutGPUUtilisation(t *testing.T) {
	s := Summarise([]model.HistoryJob{job("1", model.StateCompleted, "gpu", 2, 4, 0.9, 8192, 4096, 1, "", -1)}, 7, "1000")
	if s.Waste.GPUJobs != 0 || s.Waste.IdleGPUHours != 0 || s.GPUHours != 2 {
		t.Errorf("waste = %+v, GPU-hours %v", s.Waste, s.GPUHours)
	}
	if len(s.ByGPUType) != 1 || s.ByGPUType[0].Type != "" {
		t.Errorf("an untyped GPU is one group with an empty type: %+v", s.ByGPUType)
	}
}
