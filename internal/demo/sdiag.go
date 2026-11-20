package demo

import (
	"fmt"
	"strings"
	"time"
)

// sdiag renders scheduler statistics consistent with the simulated queue.
func (s *Sim) sdiag(now time.Time) string {
	rows, ended := s.snapshot(now)
	pending, running := 0, 0
	for _, r := range rows {
		if r.state == "RUNNING" {
			running++
		} else {
			pending++
		}
	}
	since := now.Truncate(24 * time.Hour)
	ts := func(t time.Time) string {
		return fmt.Sprintf("%s (%d)", t.Format("Mon Jan 02 15:04:05 2006"), t.Unix())
	}
	minutes := int64(now.Sub(since).Minutes()) + 1
	var b strings.Builder
	fmt.Fprintf(&b, "*******************************************************\n")
	fmt.Fprintf(&b, "sdiag output at %s\nData since      %s\n", ts(now), ts(since))
	fmt.Fprintf(&b, "*******************************************************\n")
	fmt.Fprintf(&b, "Server thread count:  3\nAgent queue size:     0\nDBD Agent queue size: 0\n\n")
	fmt.Fprintf(&b, "Jobs submitted: %d\nJobs started:   %d\nJobs completed: %d\nJobs canceled:  %d\nJobs failed:    %d\n\n",
		40+minutes/3, 30+minutes/3, 20+len(ended), 2, 1)
	fmt.Fprintf(&b, "Jobs pending:   %d\nJobs running:   %d\n\n", pending, running)
	fmt.Fprintf(&b, "Main schedule statistics (microseconds):\n\tLast cycle:   %d\n\tMax cycle:    412330\n\tMean cycle:   9120\n\tMean depth cycle:  %d\n\tCycles per minute: 2\n\tLast queue length: %d\n\n",
		6000+(now.Unix()%7)*450, pending, pending)
	last := now.Add(-time.Duration(now.Unix()%30) * time.Second)
	fmt.Fprintf(&b, "Backfilling stats\n\tTotal backfilled jobs (since last slurm start): %d\n\tTotal backfilled jobs (since last stats cycle start): %d\n\tTotal cycles: %d\n",
		900+minutes/2, minutes/4, minutes*2)
	fmt.Fprintf(&b, "\tLast cycle when: %s\n\tLast cycle: %d\n\tMax cycle:  8120332\n\tMean cycle: 640210\n\tLast depth cycle: %d\n\tLast depth cycle (try sched): %d\n\tDepth Mean: %d\n\tLast queue length: %d\n\n",
		ts(last), 520000+(now.Unix()%11)*9000, pending, max(pending-1, 0), pending, pending)
	fmt.Fprintf(&b, "Remote Procedure Call statistics by message type\n")
	fmt.Fprintf(&b, "\tREQUEST_JOB_INFO                        ( 2003) count:%-6d ave_time:1210   total_time:%d\n", minutes*40, minutes*40*1210)
	fmt.Fprintf(&b, "\tREQUEST_NODE_INFO                       ( 2007) count:%-6d ave_time:530    total_time:%d\n", minutes*20, minutes*20*530)
	fmt.Fprintf(&b, "\tREQUEST_PARTITION_INFO                  ( 2009) count:%-6d ave_time:190    total_time:%d\n\n", minutes*10, minutes*10*190)
	fmt.Fprintf(&b, "Remote Procedure Call statistics by user\n")
	fmt.Fprintf(&b, "\troot            (       0) count:%-6d ave_time:300    total_time:%d\n", minutes*50, minutes*50*300)
	fmt.Fprintf(&b, "\t%-15s (%8d) count:%-6d ave_time:210    total_time:%d\n", s.sc.User, s.sc.UID, minutes*8, minutes*8*210)
	return b.String()
}
