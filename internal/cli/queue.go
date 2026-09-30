package cli

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func newQueueCmd(a *app) *cobra.Command {
	var (
		asJSON                           bool
		user, part, stateFilter, groupBy string
	)
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Everyone's jobs, grouped by state, user or partition",
		Example: `  sdash queue
  sdash queue -p gpu --state PD
  sdash queue --group-by user
  sdash queue --json | jq '.jobs | length'`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			if groupBy == "" {
				groupBy = "state"
			}
			if !slices.Contains(state.QueueGroups, groupBy) {
				return usageError{fmt.Errorf("--group-by must be one of %s", strings.Join(state.QueueGroups, ", "))}
			}
			st := rt.store
			st.AllJobs.Data, err = rt.sources.AllJobs(ctx)
			st.AllJobs.Has = rt.fail("alljobs", err)
			var jobs []model.Job
			for _, j := range state.JoinJobs(st.AllJobs.Data, nil, state.PendingJobs(st.AllJobs.Data)) {
				switch {
				case user != "" && j.User != user:
				case part != "" && !slices.Contains(strings.Split(j.Partition, ","), part):
				case stateFilter != "" && !jobStateMatches(j, stateFilter):
				default:
					jobs = append(jobs, j)
				}
			}
			doc := report.Jobs(report.NewHeader(st, a.clock()), "all", jobs)
			if asJSON {
				err = writeJSON(cmd.OutOrStdout(), doc)
			} else {
				err = renderQueue(cmd.OutOrStdout(), doc, groupBy, st.User, st.PrivateJobs)
			}
			if err != nil {
				return err
			}
			if !st.AllJobs.Has {
				return rt.hardFail()
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&asJSON, "json", false, "print JSON")
	fl.StringVarP(&user, "user", "u", "", "only this user's jobs")
	fl.StringVarP(&part, "partition", "p", "", "only jobs in this partition")
	fl.StringVar(&stateFilter, "state", "", "only jobs in this state: R, PD, CG, ... (or running, pending)")
	fl.StringVar(&groupBy, "group-by", "", "group by state, user, partition or none (default [queue] group_by)")
	_ = cmd.RegisterFlagCompletionFunc("partition", a.completePartitions)
	_ = cmd.RegisterFlagCompletionFunc("group-by", fixedCompletions(state.QueueGroups...))
	_ = cmd.RegisterFlagCompletionFunc("state", fixedCompletions("R", "PD", "CG", "running", "pending"))
	return cmd
}

// jobStateMatches accepts squeue's short codes and state names.
func jobStateMatches(j model.Job, want string) bool {
	short := map[string]model.JobState{"R": model.StateRunning, "PD": model.StatePending, "CG": model.StateCompleting, "S": model.StateSuspended}
	want = strings.ToUpper(want)
	if s, ok := short[want]; ok {
		return j.State == s
	}
	return strings.EqualFold(string(j.State), want)
}

// queueGroup names a job's group.
func queueGroup(j report.Job, by string) string {
	switch by {
	case "user":
		return j.User
	case "partition":
		return j.Partition
	case "none":
		return ""
	}
	switch model.JobState(j.State) {
	case model.StateRunning, model.StateCompleting, model.StateConfiguring:
		return "running"
	case model.StatePending:
		if strings.HasPrefix(j.Reason, "JobHeld") {
			return "held"
		}
		return "pending"
	}
	return strings.ToLower(j.State)
}

func renderQueue(w io.Writer, doc report.JobsDoc, by, me string, private bool) error {
	if private {
		_, _ = fmt.Fprintln(w, "Other users' jobs are hidden by site policy (PrivateData=jobs); only yours are listed.")
	}
	groups := map[string][]report.Job{}
	var order []string
	for _, j := range doc.Jobs {
		g := queueGroup(j, by)
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], j)
	}
	rank := func(g string) int {
		if i := slices.Index([]string{"running", "pending", "held"}, g); by == "state" && i >= 0 {
			return i
		}
		if g == me {
			return -1
		}
		return 3
	}
	slices.SortStableFunc(order, func(a, b string) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(len(groups[b]), len(groups[a])), strings.Compare(a, b))
	})
	for i, g := range order {
		if by != "none" {
			if i > 0 {
				_, _ = fmt.Fprintln(w)
			}
			cpus, gpus := 0, 0
			for _, j := range groups[g] {
				cpus += j.CPUs
				gpus += j.GPUs
			}
			_, _ = fmt.Fprintf(w, "%s: %s, %s, %s\n", g, plural(len(groups[g]), "job"), plural(cpus, "CPU"), plural(gpus, "GPU"))
		}
		if err := renderJobs(w, report.JobsDoc{Jobs: groups[g]}, true); err != nil {
			return err
		}
	}
	if len(order) == 0 {
		_, _ = fmt.Fprintln(w, "(no jobs)")
	}
	return nil
}

func newHistoryCmd(a *app) *cobra.Command {
	var (
		asJSON      bool
		days        int
		stateFilter string
	)
	cmd := &cobra.Command{
		Use:               "history [jobid]",
		ValidArgsFunction: a.completeJobs,
		Short:             "Your finished jobs with CPU, memory and GPU efficiency; with a job ID, one job's card",
		Long: `List your finished jobs with CPU, memory and GPU efficiency. With a job ID,
print a seff-like card for that job: what went wrong if it failed, and
#SBATCH lines that would fit it better.`,
		Example: `  sdash history
  sdash history --days 30 --state failed
  sdash history 809
  sdash history --json | jq '.jobs[] | select(.mem_efficiency < 0.3) | .id'`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
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
				return fmt.Errorf("job accounting (sacct) is not available on this cluster, so there is no history")
			}
			if len(args) == 1 {
				if stateFilter != "" {
					return usageError{fmt.Errorf("--state filters the list; it does not go with a job ID")}
				}
				return a.effOne(ctx, cmd.OutOrStdout(), rt, args[0], asJSON, a.clock())
			}
			st.History.Data, err = rt.sources.History(ctx, days)
			st.History.Has = rt.fail("history", err)
			var jobs []model.HistoryJob
			for _, h := range st.History.Data.Jobs {
				if stateFilter == "" || strings.EqualFold(string(h.State), stateFilter) || (strings.EqualFold(stateFilter, "failed") && h.State.IsFailure()) {
					jobs = append(jobs, h)
				}
			}
			doc := report.History(report.NewHeader(st, a.clock()), days, jobs, st.UID)
			if asJSON {
				err = writeJSON(cmd.OutOrStdout(), doc)
			} else {
				err = renderHistory(cmd.OutOrStdout(), doc)
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
	fl.StringVar(&stateFilter, "state", "", "only this state (COMPLETED, TIMEOUT, ...) or \"failed\" for any failure")
	_ = cmd.RegisterFlagCompletionFunc("state", fixedCompletions("failed", "COMPLETED", "FAILED", "TIMEOUT", "OUT_OF_MEMORY", "CANCELLED", "NODE_FAIL"))
	return cmd
}

func renderHistory(w io.Writer, doc report.HistoryDoc) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tNAME\tSTATE\tELAPSED\tCPU\tMEM\tGPU\tENDED")
	pct := func(f *float64) string {
		if f == nil {
			return "-"
		}
		return fmt.Sprintf("%.0f%%", *f*100)
	}
	for _, j := range doc.Jobs {
		elapsed := units.FormatShort(time.Duration(j.ElapsedS) * time.Second)
		if j.TimeLimitS != nil {
			elapsed += "/" + units.FormatLimit(time.Duration(*j.TimeLimitS)*time.Second)
		}
		ended := "-"
		if j.End != nil {
			ended = j.End.Local().Format("Jan 2 15:04")
		}
		gpu := "-"
		if j.GPUs > 0 {
			gpu = fmt.Sprintf("%d (%s)", j.GPUs, pct(j.GPUUtil))
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", j.ID, truncate(j.Name, 24), j.State, elapsed, pct(j.CPUEff), pct(j.MemEff), gpu, ended)
	}
	if len(doc.Jobs) == 0 {
		_, _ = fmt.Fprintln(tw, "(no finished jobs in this range)")
	}
	return tw.Flush()
}
