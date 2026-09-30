package slurm

import (
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// FeatureStatus says how much of a feature this cluster offers.
type FeatureStatus int

// Feature statuses.
const (
	Available FeatureStatus = iota
	Limited
	Unavailable
)

// Feature is one thing sdash can show, and whether this cluster allows it.
// Note explains a feature that is Limited or Unavailable in one plain line.
type Feature struct {
	Name   string
	Status FeatureStatus
	Note   string
}

// Features lists what this cluster offers, from the probed capabilities and
// the site configuration. It hides a feature only when one of them
// positively says it is missing.
func Features(caps model.Capabilities, info parse.ClusterInfo) []Feature {
	var out []Feature
	add := func(name string, st FeatureStatus, note string) { out = append(out, Feature{name, st, note}) }

	switch {
	case caps.HasSacct:
		add("history and efficiency", Available, "")
	case info.AccountingOff():
		add("history and efficiency", Unavailable, "Slurm accounting is off (AccountingStorageType=accounting_storage/none), so there is no job history")
	default:
		add("history and efficiency", Unavailable, "sacct did not answer here, so the Usage tab stays empty")
	}

	switch {
	case !caps.HasSacct:
		add("rerun a finished job", Unavailable, "it starts from the Usage tab, which needs accounting")
	case info.NoJobScripts():
		add("rerun a finished job", Limited, "Slurm does not store job scripts (AccountingStoreFlags lacks job_script); rerun works only for jobs the controller still remembers")
	case !caps.AtLeast(23, 2):
		add("rerun a finished job", Limited, "Slurm before 23.02 does not record the submit line; rerun starts from the script's #SBATCH lines")
	default:
		add("rerun a finished job", Available, "")
	}

	switch {
	case info.BasicPriority():
		add("fairshare and priority", Unavailable, "PriorityType=priority/basic has no fairshare or priority factors")
	case !caps.HasSshare && !caps.HasSprio:
		add("fairshare and priority", Unavailable, "sshare and sprio did not answer here")
	case !caps.HasSshare || !caps.HasSprio:
		add("fairshare and priority", Limited, "only one of sshare and sprio answered here")
	default:
		add("fairshare and priority", Available, "")
	}

	switch {
	case info.NoUsageGather():
		add("live job usage", Unavailable, "JobAcctGatherType=jobacct_gather/none: CPU and memory use are not recorded")
	case !caps.HasSstat:
		add("live job usage", Unavailable, "sstat is not installed")
	default:
		add("live job usage", Available, "")
	}

	if info.AccountingOff() {
		add("account and QOS limits", Unavailable, "there is no accounting database, so Slurm sets no account or QOS limits")
	} else {
		add("account and QOS limits", Available, "")
	}

	if info.HidesJobs() {
		add("everyone's jobs (Queue)", Limited, "PrivateData=jobs: the site hides other users' jobs")
	} else {
		add("everyone's jobs (Queue)", Available, "")
	}
	return out
}
