package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/notify"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// How often wait asks Slurm: every waitEvery, every waitSlow after the
// first hour. Tests shorten them.
var (
	waitEvery = 30 * time.Second
	waitSlow  = 2 * time.Minute
)

// waitResult is how one job ended.
type waitResult struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	ExitCode string `json:"exit_code,omitempty"`
	ElapsedS int64  `json:"elapsed_s,omitempty"`
}

func newWaitCmd(a *app) *cobra.Command {
	var asJSON, hook bool
	cmd := &cobra.Command{
		Use:               "wait <jobid>...",
		ValidArgsFunction: a.completeJobs,
		Short:             "Wait until your jobs end, then print how they ended",
		Long: `Wait until every named job has left the queue, then print its final
state. Exits 0 only when every job completed, so it fits in scripts:

  sdash wait 812 && echo "done"

An array ID waits for all of its tasks. sdash wait runs in the foreground
(Ctrl-C stops it) and asks for your own jobs every 30 seconds, every 2
minutes after the first hour. --hook also runs notify_command for each job.`,
		Example: `  sdash wait 812
  sdash wait 812 815 --hook
  sdash wait 812 --json`,
		Args: usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, id := range args {
				if err := actions.ValidateJobID(id); err != nil {
					return usageError{err}
				}
			}
			ctx := cmd.Context()
			rt, err := a.slurmRuntime(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			start := a.clock()
			waiting := slices.Clone(args)
			for len(waiting) > 0 {
				jobs, err := rt.sources.MyJobs(ctx)
				switch {
				case err == nil:
					waiting = slices.DeleteFunc(waiting, func(id string) bool { return !queued(jobs, id) })
				case state.KindOf(err) == state.ErrMissing:
					return fmt.Errorf("%w: %v", errSlurmMissing, err)
				}
				if len(waiting) == 0 {
					break
				}
				every := waitEvery
				if a.clock().Sub(start) > time.Hour {
					every = waitSlow
				}
				select {
				case <-ctx.Done():
					// Interrupted: the jobs have not ended, so never exit 0.
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "sdash wait: stopped while waiting for %s\n", strings.Join(waiting, " "))
					return errSilent
				case <-time.After(every):
				}
			}
			results := a.finalResults(ctx, rt, args)
			if hook && rt.cfg.NotifyCommand != "" && a.sim == nil {
				r := execx.NewReal(execx.Options{Logger: a.log, MaxConcurrent: 1, DefaultTimeout: notify.HookTimeout})
				for _, res := range results {
					e := notify.Event{JobID: res.ID, State: res.State, ExitCode: res.ExitCode, Elapsed: time.Duration(res.ElapsedS) * time.Second}
					if err := notify.RunHook(ctx, r, rt.cfg.NotifyCommand, e); err != nil {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "notify_command for %s: %v\n", res.ID, err)
					}
				}
			}
			if asJSON {
				err = writeJSON(out, struct {
					Schema int          `json:"schema"`
					Jobs   []waitResult `json:"jobs"`
				}{jsonSchema, results})
			} else {
				err = renderWait(out, results)
			}
			if err != nil {
				return err
			}
			for _, r := range results {
				if r.State != string(model.StateCompleted) {
					return errSilent
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&hook, "hook", false, "run notify_command for each job when it ends")
	return cmd
}

// queued reports whether id (a job, an array task or a whole array) is
// still among the user's queued jobs, a task also inside a pending range.
func queued(jobs []model.Job, id string) bool {
	arr, task, isTask := strings.Cut(id, "_")
	t, err := strconv.Atoi(task)
	for _, j := range jobs {
		if j.ID.Raw == id || strconv.FormatUint(j.ID.ArrayJobID, 10) == id {
			return true
		}
		if isTask && err == nil && strconv.FormatUint(j.ID.ArrayJobID, 10) == arr && j.ID.HasTask(t) {
			return true
		}
	}
	return false
}

// finalResults asks accounting how each job ended; without accounting the
// state is ENDED.
func (a *app) finalResults(ctx context.Context, rt *slurmRuntime, ids []string) []waitResult {
	var finals []parse.FinalState
	if rt.store.Caps.HasSacct && !rt.store.Site.AccountingOff() {
		finals, _ = rt.sources.FinalStates(ctx, ids)
	}
	out := make([]waitResult, 0, len(ids))
	for _, id := range ids {
		r := waitResult{ID: id, State: notify.Ended}
		worst := ""
		for _, f := range finals {
			if f.ID != id && !strings.HasPrefix(f.ID, id+"_") {
				continue
			}
			// An array is as good as its worst task.
			if worst == "" || (f.State != model.StateCompleted && worst == string(model.StateCompleted)) {
				worst = string(f.State)
				r.ExitCode = strconv.Itoa(f.ExitCode) + ":" + strconv.Itoa(f.Signal)
			}
			r.ElapsedS = max(r.ElapsedS, int64(f.Elapsed/time.Second))
		}
		if worst != "" {
			r.State = worst
		}
		out = append(out, r)
	}
	return out
}

func renderWait(w io.Writer, results []waitResult) error {
	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "%s %s", r.ID, r.State)
		if r.ExitCode != "" && r.ExitCode != "0:0" {
			fmt.Fprintf(&b, " (exit %s)", r.ExitCode)
		}
		if r.ElapsedS > 0 {
			fmt.Fprintf(&b, " after %s", units.FormatShort(time.Duration(r.ElapsedS)*time.Second))
		}
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}
