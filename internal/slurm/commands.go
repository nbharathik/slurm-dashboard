// Package slurm builds the exact Slurm command lines and detects version features; it never runs them.
package slurm

import (
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

// jobsFormat is the squeue -o format parse.MyJobs reads.
var jobsFormat = strings.Join([]string{
	"%i", "%F", "%K", "%P", "%q", "%a", "%T", "%M", "%l", "%L", "%D", "%C",
	"%m", "%b", "%r", "%N", "%S", "%e", "%V", "%Q", "%E", "%u", "%j",
}, parse.Sep)

const (
	clusterFormat   = "JobID:48,UserName:48,Partition:64,NumNodes:8,NodeList:1024,EndTime:24,tres-alloc:1024"
	queueRankFormat = "JobID:48,Partition:64,PriorityLong:24"
	startFormat     = "JobID:48,StartTime:24,SchedNodes:256,Reason:64"
	sstatFields     = "JobID,NTasks,MaxRSS,AveRSS,AveCPU,TRESUsageInTot"
	historyFields   = "JobID,JobIDRaw,JobName,Partition,State,ExitCode,Submit,Start,End,ElapsedRaw,TimelimitRaw,AllocCPUS,TotalCPU,ReqMem,MaxRSS,TRESUsageInTot,AllocTRES,NNodes,NodeList,WorkDir"
	shareFields     = "Account,User,RawShares,NormShares,RawUsage,EffectvUsage,FairShare"
	finalFields     = "JobID,State,ExitCode,ElapsedRaw"
)

// MaxHistoryDays caps the sacct window.
const MaxHistoryDays = 30

// Commands builds the argv of every Slurm command sdash runs.
type Commands struct {
	Caps model.Capabilities
	User string
}

// Version is "sinfo --version".
func Version() []string { return []string{"sinfo", "--version"} }

// Config is "scontrol show config" (read for ClusterName).
func Config() []string { return []string{"scontrol", "show", "config"} }

// MyJobs lists the user's jobs; below Slurm 20.11 it uses -u instead of --me.
func (c Commands) MyJobs() []string {
	if c.Caps.HasMe || c.Caps.Major == 0 {
		return []string{"squeue", "--me", "-h", "-o", jobsFormat}
	}
	return []string{"squeue", "-u", c.User, "-h", "-o", jobsFormat}
}

// AllJobs lists every user's jobs (the Queue tab).
func (c Commands) AllJobs() []string {
	return []string{"squeue", "-h", "-o", jobsFormat}
}

// Cluster lists all running jobs, optionally limited to partitions.
func (c Commands) Cluster(partitions []string) []string {
	argv := []string{"squeue", "-h", "-t", "RUNNING", "-O", clusterFormat}
	if len(partitions) > 0 {
		argv = append(argv, "-p", strings.Join(partitions, ","))
	}
	return argv
}

// QueueRank lists pending jobs in the given partitions.
func (c Commands) QueueRank(partitions []string) []string {
	return []string{"squeue", "-h", "-t", "PENDING", "-p", strings.Join(partitions, ","), "-O", queueRankFormat}
}

// StartEstimate asks the scheduler when one pending job should start.
func (c Commands) StartEstimate(id string) []string {
	return []string{"squeue", "--start", "-h", "-j", id, "-O", startFormat}
}

// Nodes is "scontrol show node -o".
func (c Commands) Nodes() []string { return []string{"scontrol", "show", "node", "-o"} }

// Partitions is "scontrol show partition -o".
func (c Commands) Partitions() []string { return []string{"scontrol", "show", "partition", "-o"} }

// Reservations is "scontrol show reservation -o".
func (c Commands) Reservations() []string { return []string{"scontrol", "show", "reservation", "-o"} }

// JobDetail is "scontrol show job -o <id>".
func (c Commands) JobDetail(id string) []string {
	return []string{"scontrol", "show", "job", "-o", id}
}

// Sstat samples a running job's usage.
func (c Commands) Sstat(id string) []string {
	return []string{"sstat", "-a", "-n", "-P", "-j", id, "-o", sstatFields}
}

// SstatJobs samples several running jobs in one call; pass plain digit IDs (sstat cannot split array tasks).
func (c Commands) SstatJobs(ids []string) []string {
	return []string{"sstat", "-a", "-n", "-P", "-j", strings.Join(ids, ","), "-o", sstatFields}
}

// History is the user's jobs over the last days (1..30), steps included.
func (c Commands) History(days int) []string {
	days = min(max(days, 1), MaxHistoryDays)
	return []string{
		"sacct", "-n", "-P", "--delimiter=" + parse.Sep, "-u", c.User,
		"-S", "now-" + strconv.Itoa(days) + "days", "-E", "now", "-o", historyFields,
	}
}

// HistoryJob is one job's accounting record, with steps.
func (c Commands) HistoryJob(id string) []string {
	return []string{"sacct", "-n", "-P", "--delimiter=" + parse.Sep, "-j", id, "-o", historyFields}
}

// Fairshare is the user's fairshare associations.
func (c Commands) Fairshare() []string {
	return []string{"sshare", "-U", "-n", "-P", "-o", shareFields}
}

// AssocMgr prints the user's association and QOS limits (read-only; sites may hide it).
func (c Commands) AssocMgr() []string {
	return []string{"scontrol", "show", "assoc_mgr", "users=" + c.User, "flags=assoc,qos"}
}

// Priorities lists priority factors of the user's pending jobs.
func (c Commands) Priorities() []string {
	f := strings.Join([]string{"%i", "%Y", "%A", "%F", "%J", "%P", "%Q"}, parse.Sep)
	return []string{"sprio", "-h", "-u", c.User, "-o", f}
}

// BatchScript prints a job's batch script to stdout.
func (c Commands) BatchScript(id string) []string {
	return []string{"scontrol", "write", "batch_script", id, "-"}
}

// SacctBatchScript prints a finished job's script where accounting stores scripts.
func (c Commands) SacctBatchScript(id string) []string {
	return []string{"sacct", "-j", id, "--batch-script"}
}

// SubmitLine is a job's submit command line and WorkDir (Slurm 23.02+).
func (c Commands) SubmitLine(id string) []string {
	return []string{"sacct", "-n", "-P", "--delimiter=" + parse.Sep, "-X", "-j", id, "-o", "SubmitLine,WorkDir"}
}

// FinalStates reads the outcome of jobs that left the queue.
func (c Commands) FinalStates(ids []string) []string {
	return []string{"sacct", "-n", "-P", "-X", "-j", strings.Join(ids, ","), "-o", finalFields}
}
