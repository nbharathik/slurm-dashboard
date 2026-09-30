package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newStatusCmd(a *app) *cobra.Command {
	var (
		asJSON bool
		watch  time.Duration
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "One-shot summary: your jobs, free GPUs, alerts and storage",
		Example: `  sdash status
  sdash status --json | jq .jobs
  sdash status --watch          # redraw as data arrives (Ctrl-C stops)
  sdash status --watch=30s --json >> status.ndjson`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("watch") {
				if watch < minWatch {
					return usageError{fmt.Errorf("--watch must be at least %s", minWatch)}
				}
				return a.watchStatus(ctx, cmd.OutOrStdout(), rt, watch, asJSON)
			}
			rt.loadJobs(ctx, false)
			rt.loadNodes(ctx)
			rt.loadExtras(ctx, a)
			doc := buildStatus(rt, a.clock())
			if asJSON {
				if err := writeJSON(cmd.OutOrStdout(), doc); err != nil {
					return err
				}
			} else if err := renderStatus(cmd.OutOrStdout(), doc); err != nil {
				return err
			}
			if !rt.store.MyJobs.Has {
				return rt.hardFail()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON (with --watch: one document per line)")
	cmd.Flags().DurationVar(&watch, "watch", 0, "keep running and redraw at most this often (default 5s)")
	cmd.Flags().Lookup("watch").NoOptDefVal = "5s"
	return cmd
}

func buildStatus(rt *slurmRuntime, now time.Time) report.StatusDoc {
	st := rt.store
	doc := report.StatusDoc{Header: report.NewHeader(st, now), Alerts: []report.Alert{}, Storage: []report.Quota{}}
	c := state.CountJobs(st.MyJobs.Data)
	doc.Jobs.Running, doc.Jobs.Pending, doc.Jobs.Other = c.Running, c.Pending, c.Other
	t := state.SumGPUs(state.GPUNodes(state.NodeGPUUsage(st.Nodes.Data, st.Cluster.Data.Jobs, st.User)))
	doc.GPUs.Total, doc.GPUs.Free, doc.GPUs.NodesWithFree = t.Total, t.Free, t.NodesWithFree
	doc.GPUs.MIGTotal, doc.GPUs.MIGFree = t.MIGTotal, t.MIGFree
	for _, al := range rt.store.Alerts(now, nil) {
		doc.Alerts = append(doc.Alerts, report.FromAlert(al))
	}
	for _, q := range st.Storage.Data {
		doc.Storage = append(doc.Storage, report.FromQuota(q))
	}
	doc.Errors = rt.errStrings()
	return doc
}

func renderStatus(w io.Writer, doc report.StatusDoc) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s · %s · slurm %s\n", meta.AppName, orDash(doc.Cluster), doc.User, orDash(doc.Slurm))
	fmt.Fprintf(&b, "Jobs     %d running · %d pending · %d other\n", doc.Jobs.Running, doc.Jobs.Pending, doc.Jobs.Other)
	if doc.GPUs.Total > 0 {
		fmt.Fprintf(&b, "GPUs     %d of %d free\n", doc.GPUs.Free, doc.GPUs.Total)
	}
	if doc.GPUs.MIGTotal > 0 {
		fmt.Fprintf(&b, "MIG      %d of %d slices free\n", doc.GPUs.MIGFree, doc.GPUs.MIGTotal)
	}
	for _, q := range doc.Storage {
		fmt.Fprintf(&b, "Storage  %-10s %s\n", q.Label, quotaLine(q))
	}
	for _, al := range doc.Alerts {
		fmt.Fprintf(&b, "%s %s\n", alertMark(al.Level), al.Message)
	}
	for _, e := range doc.Errors {
		fmt.Fprintf(&b, "! %s\n", e)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func quotaLine(q report.Quota) string {
	if q.Error != "" && q.UsedBytes == 0 {
		return "unavailable: " + q.Error
	}
	limit := q.HardBytes
	if q.SoftBytes > 0 {
		limit = q.SoftBytes
	}
	s := units.FormatBytes(q.UsedBytes)
	if limit > 0 {
		s += fmt.Sprintf(" of %s (%d%%)", units.FormatBytes(limit), q.UsedPct)
	}
	if q.FilesPct > q.UsedPct {
		s += fmt.Sprintf(", files %d%%", q.FilesPct)
	}
	if q.IsFilesystemTotal {
		s += "  (shared filesystem, no personal quota)"
	}
	return s
}

func alertMark(level string) string {
	switch level {
	case "crit":
		return "✗"
	case "warn":
		return "!"
	}
	return "·"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newJobsCmd(a *app) *cobra.Command {
	var asJSON, all bool
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List your jobs (sdash queue lists everyone's)",
		Example: `  sdash jobs
  sdash jobs --json`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			rt.loadJobs(ctx, all)
			st := rt.store
			jobs := st.MyJobs.Data
			scope := "mine"
			if all {
				jobs, scope = st.AllJobs.Data, "all"
			}
			jobs = state.JoinJobs(jobs, st.Cluster.Data.Jobs, st.QueueRank.Data)
			doc := report.Jobs(report.NewHeader(st, a.clock()), scope, jobs)
			if asJSON {
				err = writeJSON(cmd.OutOrStdout(), doc)
			} else {
				err = renderJobs(cmd.OutOrStdout(), doc, all)
			}
			if err != nil {
				return err
			}
			if (all && !st.AllJobs.Has) || (!all && !st.MyJobs.Has) {
				return rt.hardFail()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&all, "all", false, "show every user's jobs (same as sdash queue --group-by none)")
	return cmd
}

func renderJobs(w io.Writer, doc report.JobsDoc, all bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	head := "ID\tNAME\tSTATE\tTIME\tGPU\tPARTITION\tNODE / REASON"
	if all {
		head = "ID\tUSER\tNAME\tSTATE\tTIME\tGPU\tPARTITION\tNODE / REASON"
	}
	_, _ = fmt.Fprintln(tw, head)
	for _, j := range doc.Jobs {
		where := strings.Join(j.NodeList, ",")
		if j.State == string(model.StatePending) {
			where = j.Reason
			if j.QueueRank > 0 {
				where = fmt.Sprintf("#%d of %d · %s", j.QueueRank, j.QueueTotal, j.Reason)
			}
		}
		t := units.FormatDuration(time.Duration(j.TimeUsedS) * time.Second)
		if j.TimeLimitS != nil {
			t += "/" + units.FormatDuration(time.Duration(*j.TimeLimitS)*time.Second)
		}
		row := []string{j.ID, truncate(j.Name, 24), j.State, t, fmt.Sprint(j.GPUs), j.Partition, where}
		if all {
			row = append([]string{j.ID, j.User}, row[1:]...)
		}
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	if len(doc.Jobs) == 0 {
		_, _ = fmt.Fprintln(tw, "(no jobs)")
	}
	return tw.Flush()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func newGPUsCmd(a *app) *cobra.Command {
	var asJSON, allNodes bool
	cmd := &cobra.Command{
		Use:    "gpus",
		Hidden: true, // "nodes --gpu" is the everyday form
		Short:  "GPU availability per node, with when busy GPUs free up",
		Example: `  sdash gpus
  sdash gpus --all-nodes
  sdash gpus --json | jq '.nodes[] | select(.gpu_free > 0)'`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			rt.loadNodes(ctx)
			st := rt.store
			usage := rt.scopedUsage()
			doc := report.GPUs(report.NewHeader(st, a.clock()), usage, allNodes, rt.cfg.GPUNames)
			if asJSON {
				err = writeJSON(cmd.OutOrStdout(), doc)
			} else {
				err = renderGPUs(cmd.OutOrStdout(), doc, a.clock())
			}
			if err != nil {
				return err
			}
			if !st.Nodes.Has {
				return rt.hardFail()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&allNodes, "all-nodes", false, "include nodes without GPUs")
	return cmd
}

func renderGPUs(w io.Writer, doc report.GPUsDoc, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NODE\tSTATE\tGPUS\tFREE\tTYPE\tMIG\tUSED BY\tNEXT FREE")
	for _, n := range doc.Nodes {
		var users []string
		if n.Mine > 0 {
			users = append(users, fmt.Sprintf("you %d", n.Mine))
		}
		for u, c := range n.Users {
			users = append(users, fmt.Sprintf("%s %d", u, c))
		}
		next := "now"
		switch {
		case !state.Available(model.Node{State: n.State, Flags: n.Flags}):
			next = "-"
		case n.Free > 0 || (n.Total == 0 && n.MIGFree > 0):
		case n.FreeBy != nil:
			next = fmt.Sprintf("by %s (%s)", n.FreeBy.Local().Format("Jan 2 15:04"), units.FormatShort(n.FreeBy.Sub(now)))
		default:
			next = "unknown"
		}
		nodeState := strings.ToLower(n.State)
		if len(n.Flags) > 0 {
			nodeState += "+" + strings.ToLower(strings.Join(n.Flags, "+"))
		}
		var mig []string
		for _, g := range n.Groups {
			if g.MIG {
				mig = append(mig, fmt.Sprintf("%s %d/%d", g.Type, g.Allocated, g.Total))
			}
		}
		gpus, free := fmt.Sprintf("%d/%d", n.Allocated, n.Total), strconv.Itoa(n.Free)
		if n.Total == 0 { // MIG slices only, or no GPUs (--all-nodes)
			gpus, free = "-", "-"
		}
		if n.Total == 0 && n.MIGTotal == 0 {
			next = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", n.Name, nodeState, gpus, free,
			orDash(n.GPUDisplay), orDash(strings.Join(mig, " · ")), orDash(strings.Join(slices.Sorted(slices.Values(users)), " · ")), next)
	}
	if len(doc.Nodes) == 0 {
		_, _ = fmt.Fprintln(tw, "(no GPU nodes; sdash nodes lists every node)")
	}
	return tw.Flush()
}

// loadExtras loads partitions, reservations, a week of history and fairshare; missing commands are skipped.
func (rt *slurmRuntime) loadExtras(ctx context.Context, a *app) {
	st := rt.store
	var err error
	st.Partitions.Data, err = rt.sources.Partitions(ctx)
	st.Partitions.Has = rt.fail("partitions", err)
	st.Reservations.Data, err = rt.sources.Reservations(ctx)
	st.Reservations.Has = rt.fail("reservations", err)
	if st.Caps.HasSacct {
		st.History.Data, err = rt.sources.History(ctx, 7)
		st.History.Has = rt.fail("history", err)
	}
	if st.Caps.HasSshare {
		st.Fairshare.Data, err = rt.sources.Fairshare(ctx)
		st.Fairshare.Has = rt.fail("fairshare", err)
	}
	rt.loadStorage(ctx, a)
}
