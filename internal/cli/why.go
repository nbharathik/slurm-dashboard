package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// findJob finds a queued job by ID, accepting the array job ID for arrays.
func findJob(jobs []model.Job, id string) (model.Job, bool) {
	for _, j := range jobs {
		if j.ID.Raw == id {
			return j, true
		}
	}
	for _, j := range jobs {
		if fmt.Sprint(j.ID.ArrayJobID) == id && j.State == model.StatePending {
			return j, true
		}
	}
	return model.Job{}, false
}

func newWhyCmd(a *app) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:               "why <jobid>",
		ValidArgsFunction: a.completeJobs,
		Short:             "Explain why a job is pending",
		Example: `  sdash why 815
  sdash why 815 --json`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			rt.loadJobs(ctx, false)
			if !rt.store.MyJobs.Has {
				return rt.hardFail()
			}
			jobs := state.JoinJobs(rt.store.MyJobs.Data, rt.store.Cluster.Data.Jobs, rt.store.QueueRank.Data)
			j, ok := findJob(jobs, args[0])
			if !ok {
				return fmt.Errorf("job %s is not one of your queued jobs (finished jobs: sdash history %s)", args[0], args[0])
			}
			if j.State != model.StatePending {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s is %s, not pending.\n", j.ID.Raw, j.Name, j.State)
				return err
			}
			prio := ""
			if rt.store.Caps.HasSprio {
				if pfs, err := rt.sources.Priorities(ctx); err == nil {
					if pf, ok := insights.PriorityFor(pfs, j); ok {
						prio = insights.PriorityLine(pf)
					}
				}
			}
			now := a.clock()
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), report.Why(report.NewHeader(rt.store, now), j, prio))
			}
			return renderWhy(cmd.OutOrStdout(), j, prio, now)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func renderWhy(w io.Writer, j model.Job, prio string, now time.Time) error {
	est := ""
	if !j.StartTime.IsZero() {
		est = j.StartTime.Local().Format("Mon Jan 2 15:04")
		if d := j.StartTime.Sub(now); d > 0 {
			est += " (in " + units.FormatShort(d) + ")"
		}
	}
	card := insights.WhyCard(j, est)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n%s\n", j.ID.Raw, j.Name, card.Headline)
	for _, l := range card.Lines {
		b.WriteString(l + "\n")
	}
	if prio != "" {
		b.WriteString("Priority " + prio + "\n")
	}
	if !card.Pending.Known {
		b.WriteString("See \"JOB REASON CODES\" in man squeue.\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func newEffCmd(a *app) *cobra.Command {
	var (
		asJSON bool
		days   int
	)
	cmd := &cobra.Command{
		Use:               "eff [jobid]",
		Hidden:            true, // "history JOBID" is the everyday form
		ValidArgsFunction: a.completeJobs,
		Short:             "Efficiency of a finished job, or of your recent jobs",
		Long: `With a job ID, print a seff-like card: CPU, memory and time efficiency,
what went wrong if it failed, and #SBATCH lines that would fit it better.
Without one, list your jobs that finished in the last --days days.`,
		Example: `  sdash eff 809
  sdash eff --days 30
  sdash eff 809 --json`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if days < 1 || days > 30 {
				return usageError{errors.New("--days must be between 1 and 30")}
			}
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			if !rt.store.Caps.HasSacct {
				return errors.New("efficiency needs Slurm accounting (sacct), which is not available here")
			}
			now := a.clock()
			if len(args) == 1 {
				return a.effOne(ctx, cmd.OutOrStdout(), rt, args[0], asJSON, now)
			}
			h, err := rt.sources.History(ctx, days)
			if err != nil {
				return err
			}
			var done []model.HistoryJob
			for _, j := range h.Jobs {
				if !j.State.IsActive() {
					done = append(done, j)
				}
			}
			if asJSON {
				doc := report.EffDoc{Header: report.NewHeader(rt.store, now), Days: days, Jobs: []report.Efficiency{}}
				for _, j := range done {
					doc.Jobs = append(doc.Jobs, report.Eff(j, rt.store.UID))
				}
				return writeJSON(cmd.OutOrStdout(), doc)
			}
			return renderEffTable(cmd.OutOrStdout(), done, days)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&days, "days", 7, "history window in days (1-30)")
	return cmd
}

func (a *app) effOne(ctx context.Context, w io.Writer, rt *slurmRuntime, id string, asJSON bool, now time.Time) error {
	j, err := rt.sources.HistoryJob(ctx, id)
	if err != nil {
		return err
	}
	if j == nil {
		return fmt.Errorf("job %s is not in the accounting database (yet)", id)
	}
	if asJSON {
		doc := report.EffDoc{Header: report.NewHeader(rt.store, now), Jobs: []report.Efficiency{report.Eff(*j, rt.store.UID)}}
		return writeJSON(w, doc)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", j.ID.Raw, j.Name)
	if hint := insights.ExplainFailure(*j, rt.store.UID); hint.Title != "" {
		fmt.Fprintf(&b, "%s: %s\n\n", hint.Title, hint.Text)
	}
	for _, l := range insights.EffCard(*j) {
		b.WriteString(l + "\n")
	}
	if j.State.IsActive() {
		b.WriteString("\nThe job has not finished; numbers are partial.\n")
	}
	if s := insights.RightSize(*j); len(s) > 0 {
		b.WriteString("\nRight-sizing:\n")
		for _, x := range s {
			fmt.Fprintf(&b, "  %s: %s\n", x.What, x.Reason)
			if x.Line != "" {
				fmt.Fprintf(&b, "    %s\n", x.Line)
			}
		}
	}
	_, err = io.WriteString(w, b.String())
	return err
}

func pctOrDash(f float64) string {
	if f < 0 || math.IsNaN(f) {
		return "-"
	}
	return fmt.Sprintf("%d%%", int(math.Round(f*100)))
}

func renderEffTable(w io.Writer, jobs []model.HistoryJob, days int) error {
	if len(jobs) == 0 {
		_, err := fmt.Fprintf(w, "No finished jobs in the last %d days.\n", days)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tNAME\tSTATE\tELAPSED\tCPU%\tMEM%\tGPU-H\tEND")
	var mems []float64
	for _, j := range jobs {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%.1f\t%s\n", j.ID.Raw, truncate(j.Name, 24), j.State,
			units.FormatShort(j.Elapsed), pctOrDash(j.Eff.CPU), pctOrDash(j.Eff.Mem), j.Eff.GPUHours, j.End.Local().Format("Jan 2 15:04"))
		if j.State == model.StateCompleted && j.Eff.Mem >= 0 {
			mems = append(mems, j.Eff.Mem)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(mems) > 0 {
		_, err := fmt.Fprintf(w, "\nMedian memory efficiency of completed jobs: %s. Details: sdash history <jobid>\n", pctOrDash(insights.Median(mems)))
		return err
	}
	return nil
}
