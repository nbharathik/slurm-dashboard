package demo

import (
	"fmt"
	"strings"
	"time"
)

// assocMgr renders "scontrol show assoc_mgr" for the demo user in the
// format of Slurm 23.11 (testdata/fixtures/23.11/assocmgr.txt).
func (s *Sim) assocMgr(now time.Time) (string, error) {
	if s.sc.NoAccounting {
		return "", fmt.Errorf("scontrol: error: slurm_load_assoc_mgr_info: Unable to contact slurm controller (connect failure)")
	}
	l := s.sc.Limits
	if l == nil {
		l = &Limits{}
	}
	var run, held, cpus, gpus int
	rows, _ := s.snapshot(now)
	for _, r := range rows {
		if r.job.User != s.sc.User {
			continue
		}
		held++
		if r.state == "RUNNING" {
			run++
			cpus += r.job.CPUs
			gpus += r.job.GPUs
		}
	}
	count := func(limit, used int) string {
		if limit == 0 {
			return "N(0)"
		}
		return fmt.Sprintf("%d(%d)", limit, used)
	}
	tres := func(cpu, gpu string) string {
		return fmt.Sprintf("cpu=%s,mem=N(0),energy=N(0),node=N(0),billing=N(0),fs/disk=N(0),vmem=N(0),pages=N(0),gres/gpu=%s,gres/gpumem=N(0),gres/gpuutil=N(0)", cpu, gpu)
	}
	blank := func(v int) string {
		if v == 0 {
			return ""
		}
		return fmt.Sprint(v)
	}
	var b strings.Builder
	b.WriteString("Current Association Manager state\n\nAssociation Records\n\n")
	fmt.Fprintf(&b, "ClusterName=%s Account=%s UserName= Partition= Priority=0 ID=3\n", s.sc.Cluster, s.sc.Account)
	b.WriteString("    ParentAccount=root(1) Lineage=/" + s.sc.Account + "/ DefAssoc=Yes\n")
	b.WriteString("    GrpJobs=N(0) GrpJobsAccrue=N(0)\n    GrpSubmitJobs=N(0) GrpWall=N(0.00)\n")
	fmt.Fprintf(&b, "    GrpTRES=%s\n", tres(count(l.AccountCPUs, cpus), "N(0)"))
	b.WriteString("    MaxJobs= MaxJobsAccrue= MaxSubmitJobs= MaxWallPJ=\n    MaxTRESPJ=\n")
	fmt.Fprintf(&b, "ClusterName=%s Account=%s UserName=%s(1000) Partition= Priority=0 ID=7\n", s.sc.Cluster, s.sc.Account, s.sc.User)
	b.WriteString("    ParentAccount= Lineage=/" + s.sc.Account + "/0-" + s.sc.User + "/ DefAssoc=Yes\n")
	b.WriteString("    GrpJobs=N(0) GrpJobsAccrue=N(0)\n    GrpSubmitJobs=N(0) GrpWall=N(0.00)\n")
	gpu := "N(0)"
	if l.UserGPUs > 0 {
		gpu = count(l.UserGPUs, gpus)
	}
	fmt.Fprintf(&b, "    GrpTRES=%s\n", tres(count(l.UserCPUs, cpus), gpu))
	fmt.Fprintf(&b, "    MaxJobs=%s MaxJobsAccrue= MaxSubmitJobs=%s MaxWallPJ=%s\n    MaxTRESPJ=\n",
		orEmpty(l.MaxJobs, run), orEmpty(l.MaxSubmit, held), blank(l.MaxWallMin))
	b.WriteString("\nQOS Records\n\nQOS=normal(1)\n    GrpJobs=N(0) GrpJobsAccrue=N(0) GrpSubmitJobs=N(0) GrpWall=N(0.00)\n")
	fmt.Fprintf(&b, "    GrpTRES=%s\n    MaxWallPJ=%s\n    MaxTRESPJ=\n    Account Limits\n        No Accounts\n    User Limits\n        No Users\n",
		tres("N(0)", "N(0)"), blank(l.QOSWallMin))
	return b.String(), nil
}

// orEmpty renders "max(used)", or nothing when the limit is unset.
func orEmpty(limit, used int) string {
	if limit == 0 {
		return ""
	}
	return fmt.Sprintf("%d(%d)", limit, used)
}
