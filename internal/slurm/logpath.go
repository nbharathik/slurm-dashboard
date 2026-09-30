package slurm

import (
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// LogPath returns a job's stdout (or stderr) file from its scontrol
// detail, with any remaining %-patterns expanded relative to WorkDir.
func LogPath(d *model.JobDetail, stderr bool) string {
	p := d.StdOut
	if stderr && d.StdErr != "" {
		p = d.StdErr
	}
	if p == "" {
		return ""
	}
	first := ""
	if len(d.NodeList) > 0 {
		first = d.NodeList[0]
	}
	arrayJob, task := d.Raw["ArrayJobId"], d.Raw["ArrayTaskId"]
	jobID := d.Raw["JobId"]
	if jobID == "" {
		jobID = d.ID.Raw
	}
	if arrayJob == "" {
		arrayJob = jobID
	}
	return units.ExpandLogPattern(p, units.LogVars{
		JobID: jobID, Name: d.Name, User: d.User, ArrayJobID: arrayJob, TaskID: task, FirstNode: first, WorkDir: d.WorkDir,
	})
}
