package insights

import "github.com/nbharathik/slurm-dashboard/internal/model"

// LiveUse is the fraction of its request a running job has used (memory is peak); -1 when unknown.
type LiveUse struct {
	CPU, Mem, GPU float64
}

// LiveEfficiency reads a running job's sstat sample against its request.
func LiveEfficiency(j model.Job, s model.JobStat) LiveUse {
	u := LiveUse{CPU: -1, Mem: -1, GPU: -1}
	if j.TimeUsed > 0 && j.CPUs > 0 {
		u.CPU = min(s.TotalCPU.Seconds()/j.TimeUsed.Seconds()/float64(j.CPUs), 1)
	}
	if req := j.MemPerNodeMB * int64(max(j.Nodes, 1)); req > 0 {
		u.Mem = min(float64(s.MaxRSSMB)/float64(req), 1)
	}
	if j.GPUs > 0 && s.HasGPUUtil {
		u.GPU = s.GPUUtil
	}
	return u
}
