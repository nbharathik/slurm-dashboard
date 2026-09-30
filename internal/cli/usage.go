package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
)

func newUsageCmd(a *app) *cobra.Command {
	var (
		asJSON bool
		days   int
	)
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "What your jobs used and wasted: outcomes, CPU and GPU hours, idle resources",
		Long: `Summarise your finished jobs over the last --days days: how they ended, how
many CPU- and GPU-hours they used (by partition and GPU type), how much of
what they asked for sat idle, and the jobs that wasted the most, each with
the #SBATCH lines that would fit it better. It uses the same accounting data
as 'sdash history' and needs Slurm accounting.`,
		Example: `  sdash usage
  sdash usage --days 30
  sdash usage --json | jq '.waste'`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if days < 1 || days > slurm.MaxHistoryDays {
				return usageError{fmt.Errorf("--days must be between 1 and %d", slurm.MaxHistoryDays)}
			}
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			st := rt.store
			if !st.Caps.HasSacct {
				return fmt.Errorf("job accounting (sacct) is not available on this cluster, so there is no usage to summarise")
			}
			st.History.Data, err = rt.sources.History(ctx, days)
			st.History.Has = rt.fail("history", err)
			sum := insights.Summarise(st.History.Data.Jobs, days, st.UID)
			acct := insights.BuildAccount(readAccount(ctx, rt))
			doc := report.Usage(report.NewHeader(st, a.clock()), sum, acct, rt.cfg.GPUNames)
			if asJSON {
				err = writeJSON(cmd.OutOrStdout(), doc)
			} else {
				err = renderUsage(cmd.OutOrStdout(), doc)
			}
			if err != nil {
				return err
			}
			if !st.History.Has {
				return rt.hardFail()
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&asJSON, "json", false, "print JSON")
	fl.IntVar(&days, "days", 7, "how many days back (1-30)")
	return cmd
}

// readAccount reads the limits and the fairshare, each best effort: a site
// can hide them, and the summary then says why instead of failing.
func readAccount(ctx context.Context, rt *slurmRuntime) insights.AccountInput {
	st := rt.store
	in := insights.AccountInput{User: st.User, NoAccounting: st.Site.AccountingOff(), BasicPriority: st.Site.BasicPriority()}
	if !in.NoAccounting {
		scopes, err := rt.sources.Limits(ctx)
		in.Scopes, in.LimitsRead, in.LimitsErr = scopes, err == nil, errText(err)
	}
	if !in.NoAccounting && !in.BasicPriority && st.Caps.HasSshare {
		shares, err := rt.sources.Fairshare(ctx)
		in.Shares, in.PriorityRead, in.PriorityErr = shares, err == nil, errText(err)
		if err == nil && st.Caps.HasSprio {
			in.Priorities, _ = rt.sources.Priorities(ctx)
		}
	}
	return in
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return execx.FirstLine([]byte(err.Error()))
}

func renderUsage(w io.Writer, doc report.UsageDoc) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Your jobs over the last %d days: %d finished\n", doc.Days, doc.Finished)
	if doc.Finished == 0 {
		b.WriteString("\nNothing finished in this range.\n")
		renderAccount(&b, doc)
		_, err := io.WriteString(w, b.String())
		return err
	}
	var parts []string
	for _, o := range doc.Outcomes {
		parts = append(parts, fmt.Sprintf("%s %d", o.State, o.Count))
	}
	fmt.Fprintf(&b, "\nOutcomes  %s\n", strings.Join(parts, " · "))
	fmt.Fprintf(&b, "Failed    %d of %d (%.0f%%)", doc.Failed, doc.Finished, 100*doc.FailureRate)
	if f := doc.TopFailure; f != nil {
		fmt.Fprintf(&b, ", most often %s (%d; latest %s)", f.State, f.Count, f.Job)
	}
	b.WriteString("\n")
	if f := doc.TopFailure; f != nil && f.Hint != "" {
		fmt.Fprintf(&b, "          %s\n", f.Hint)
	}

	fmt.Fprintf(&b, "\nHours     CPU %s", insights.FormatHours(doc.CPUHours))
	if doc.GPUHours > 0 {
		fmt.Fprintf(&b, " · GPU %s", insights.FormatHours(doc.GPUHours))
		if len(doc.GPUTypes) > 1 || (len(doc.GPUTypes) == 1 && doc.GPUTypes[0].GPUType != "") {
			var types []string
			for _, t := range doc.GPUTypes {
				name := t.Display
				if name == "" {
					name = "type not recorded"
				}
				types = append(types, fmt.Sprintf("%s %s", name, insights.FormatHours(t.Hours)))
			}
			fmt.Fprintf(&b, " (%s)", strings.Join(types, ", "))
		}
	}
	b.WriteString("\n")
	for _, p := range doc.Partitions {
		fmt.Fprintf(&b, "          %-12s %3d jobs · CPU %s", p.Partition, p.Jobs, insights.FormatHours(p.CPUHours))
		if p.GPUHours > 0 {
			fmt.Fprintf(&b, " · GPU %s", insights.FormatHours(p.GPUHours))
		}
		b.WriteString("\n")
	}

	wst := doc.Waste
	b.WriteString("\nWasted    (completed jobs only)\n")
	if wst.CPUJobs > 0 {
		fmt.Fprintf(&b, "          CPU idle %s of the %s held (%s, %d jobs)\n", insights.FormatHours(wst.IdleCPUHours), insights.FormatHours(wst.CPUHeldHours), insights.Percent(wst.IdleCPUHours, wst.CPUHeldHours), wst.CPUJobs)
	}
	if wst.MemJobs > 0 {
		fmt.Fprintf(&b, "          memory unused about %.0f GB-hours, from estimated peaks (%d jobs)\n", wst.IdleMemGBh, wst.MemJobs)
	}
	switch {
	case wst.IdleGPUHours != nil:
		fmt.Fprintf(&b, "          GPU idle %s of the %s held (%s, %d jobs)\n", insights.FormatHours(*wst.IdleGPUHours), insights.FormatHours(wst.GPUHeldHours), insights.Percent(*wst.IdleGPUHours, wst.GPUHeldHours), wst.GPUJobs)
	case doc.GPUHours > 0:
		b.WriteString("          GPU use is not recorded by this cluster (no gres/gpuutil)\n")
	}

	if len(doc.Worst) > 0 {
		b.WriteString("\nLeast efficient completed jobs\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  ID\tNAME\tIDLE CPU\tIDLE GPU\tSUGGESTION")
		for _, x := range doc.Worst {
			var sg []string
			for _, s := range x.Suggestions {
				if s.Line != "" {
					sg = append(sg, strings.TrimPrefix(s.Line, "#SBATCH "))
				}
			}
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", x.ID, truncate(x.Name, 24), insights.FormatHours(x.IdleCPUHours), insights.FormatHours(x.IdleGPUHours), orDash(strings.Join(sg, " ")))
		}
		_ = tw.Flush()
	}
	renderAccount(&b, doc)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderAccount prints the limits and the priority, or why they are
// missing.
func renderAccount(b *strings.Builder, doc report.UsageDoc) {
	b.WriteString("\nLimits\n")
	if doc.LimitsNote != "" {
		fmt.Fprintf(b, "          %s\n", doc.LimitsNote)
	}
	scope := ""
	for _, l := range doc.Limits {
		if l.Scope != scope {
			scope = l.Scope
			fmt.Fprintf(b, "          %s\n", scope)
		}
		flag := ""
		if l.Near {
			flag = "  <- close to the limit"
		}
		fmt.Fprintf(b, "            %-28s %s%s\n", l.What, limitText(l), flag)
	}

	b.WriteString("\nPriority\n")
	if doc.PriorityNote != "" {
		fmt.Fprintf(b, "          %s\n", doc.PriorityNote)
	}
	for _, f := range doc.Fairshare {
		fmt.Fprintf(b, "          %s: fairshare %.2f, used %.1f%% of the cluster against %.1f%% shares: %s\n", f.Account, f.FairShare, f.UsedPct, f.SharesPct, f.Verdict)
	}
	for _, p := range doc.Pending {
		fmt.Fprintf(b, "          job %s pending: %s\n", p.Job, p.Detail)
	}
}

// limitText uses the same limit wording as the dashboard.
func limitText(l report.UsageLimit) string {
	for _, ln := range insights.Limits([]model.LimitScope{{Limits: []model.Limit{{Name: l.Name, Unit: l.Unit, Max: l.Max, Used: usedOrNeg(l.Used)}}}}) {
		return ln.Text
	}
	return ""
}

func usedOrNeg(v *float64) float64 {
	if v == nil {
		return -1
	}
	return *v
}
