package parse

import "testing"

// D1: a --mem-per-cpu job's memory comes from the TRES totals, not from
// MinMemoryCPU times the minimum CPUs per node.
func TestJobDetailMemPerCPU(t *testing.T) {
	line := "JobId=900 JobName=mpc UserId=you(1000) Account=a QOS=normal JobState=PENDING Reason=Priority " +
		"Partition=cpu NumNodes=2 NumCPUs=16 MinCPUsNode=1 MinMemoryCPU=4G " +
		"ReqTRES=cpu=16,mem=64G,node=2,billing=16 AllocTRES=(null) RunTime=00:00:00 TimeLimit=01:00:00 " +
		"SubmitTime=2026-09-28T12:00:00 StartTime=Unknown EndTime=Unknown Priority=100 NodeList=(null) WorkDir=/home/you"
	ds, warns := JobDetails([]byte(line + "\n"))
	if len(warns) > 0 || len(ds) != 1 {
		t.Fatalf("details %+v, warnings %+v", ds, warns)
	}
	if got := ds[0].MemPerNodeMB; got != 32*1024 {
		t.Fatalf("memory per node = %d MB, want 32768 (64G over 2 nodes)", got)
	}
}
