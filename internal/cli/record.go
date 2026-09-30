package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func newRecordCmd(a *app) *cobra.Command {
	var out string
	var days int
	var anonymize bool
	cmd := &cobra.Command{
		Use:    "record --out DIR",
		Hidden: true,
		Short:  "Run every data source once and save the raw output as test fixtures",
		Long: `Run every read-only data source once and save each command's raw output as
DIR/<slurm-version>/<source>.txt plus <source>.meta.json (argv, exit code,
duration, stderr). The version directory must not already exist.
Only read-only commands run.

With --anonymize, user names, accounts, job names, the cluster name and your
home directory are replaced consistently (user1, acct1, job1, cluster1,
/home/user1). The mapping is written beside the fixtures as
anonymize.mapping.json; never commit or share that file. Review the fixtures
before sharing them: free-text fields such as working directories can still
contain project names.`,
		Example: `  sdash record --out ./fixtures --anonymize`,
		Args:    usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out == "" {
				return usageError{fmt.Errorf("--out is required")}
			}
			return a.record(cmd.Context(), cmd.OutOrStdout(), out, anonymize, days)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output `directory`")
	cmd.Flags().IntVar(&days, "days", 7, "days of accounting history to record (1-30)")
	cmd.Flags().BoolVar(&anonymize, "anonymize", false, "replace user, account, job and cluster names (review the files before sharing)")
	return cmd
}

func (a *app) record(ctx context.Context, w io.Writer, out string, anonymize bool, days int) error {
	rt, err := a.slurmRuntime(ctx)
	if err != nil {
		return err
	}
	caps := rt.store.Caps
	dir := filepath.Join(out, fmt.Sprintf("%d.%d", caps.Major, caps.Minor))
	if err := os.MkdirAll(out, 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("create recording directory: %w; choose a fresh --out directory", err)
	}
	rec := &execx.RecordingRunner{Inner: rt.runner, Dir: dir}
	src := &state.Sources{Runner: rec, Cmd: rt.sources.Cmd, Log: a.log}
	var failed []string
	note := func(what string, err error) {
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", what, err))
		}
	}

	_, err = rec.Run(execx.WithLabel(ctx, "version"), slurm.Version()...)
	note("version", err)
	_, err = rec.Run(execx.WithLabel(ctx, "clustername"), slurm.Config()...)
	note("clustername", err)
	note("clustername", keepLines(filepath.Join(dir, "clustername.txt"), "ClusterName", "PrivateData", "SLURM_VERSION", "AccountingStorageType", "AccountingStoreFlags", "PriorityType", "JobAcctGatherType"))

	mine, err := src.MyJobs(ctx)
	note("myjobs", err)
	all, err := src.AllJobs(ctx)
	note("alljobs", err)
	cluster, err := src.Cluster(ctx, nil)
	note("cluster", err)
	if parts := state.PartitionsOfPending(mine); len(parts) > 0 {
		_, err = src.QueueRank(ctx, parts)
		note("queuerank", err)
	}
	nodes, err := src.Nodes(ctx)
	note("nodes", err)
	parts, err := src.Partitions(ctx)
	note("partitions", err)
	_, err = src.Reservations(ctx)
	note("reservations", err)
	hist := state.HistoryData{}
	if caps.HasSacct {
		hist, err = src.History(ctx, days)
		note("history", err)
	}
	var shares []model.Share
	if caps.HasSshare {
		shares, err = src.Fairshare(ctx)
		note("fairshare", err)
	}
	if caps.HasSprio {
		_, err = src.Priorities(ctx)
		note("sprio", err)
	}

	var details []*model.JobDetail
	for _, want := range []model.JobState{model.StateRunning, model.StatePending} {
		for _, j := range mine {
			if j.State != want {
				continue
			}
			label := "jobdetail-" + strings.ToLower(string(want))
			res, err := rec.Run(execx.WithLabel(ctx, label), rt.sources.Cmd.JobDetail(j.ID.Raw)...)
			note(label, err)
			if err == nil {
				if d, _ := (&state.Sources{Runner: fixedRunner(res), Cmd: rt.sources.Cmd}).JobDetail(ctx, j.ID.Raw); d != nil {
					details = append(details, d)
				}
			}
			if want == model.StateRunning && caps.HasSstat {
				_, err = rec.Run(execx.WithLabel(ctx, "jobstat-running"), rt.sources.Cmd.Sstat(j.ID.Raw)...)
				note("jobstat-running", err)
			}
			break
		}
	}
	var finished []string
	for _, h := range hist.Jobs {
		if !h.State.IsActive() && len(finished) < 5 {
			finished = append(finished, h.ID.Raw)
		}
	}
	if len(finished) > 0 {
		_, err = src.FinalStates(ctx, finished)
		note("final", err)
	}

	msg := fmt.Sprintf("Recorded Slurm %s fixtures in %s\n", caps.Version, dir)
	if anonymize {
		an := newAnonymizer(rt.sources.Cmd.User, a.env.Getenv("HOME"))
		an.cluster = rt.store.ClusterName
		for _, p := range parts {
			an.reserve(p.Name)
		}
		for _, n := range nodes {
			an.reserve(n.Name)
		}
		for _, j := range append(mine, all...) {
			an.addUser(j.User)
			an.addAccount(j.Account)
			an.addJob(j.Name)
			an.reserve(j.QOS, j.Partition)
		}
		for _, r := range cluster.Jobs {
			an.addUser(r.User)
		}
		for _, h := range hist.Jobs {
			an.addJob(h.Name)
		}
		for _, s := range shares {
			an.addUser(s.User)
			an.addAccount(s.Account)
		}
		for _, d := range details {
			an.addUser(d.User)
			an.addAccount(d.Account)
			an.addJob(d.Name)
		}
		mapPath, err := an.apply(dir)
		if err != nil {
			return err
		}
		msg += fmt.Sprintf("Anonymized. The mapping is in %s; do not commit or share it.\n", mapPath)
	} else {
		msg += "Not anonymized: the files contain real user, account and job names.\n"
	}
	sort.Strings(failed)
	for _, f := range failed {
		msg += "! " + f + "\n"
	}
	msg += "Review the files before sharing them.\n"
	_, err = io.WriteString(w, msg)
	return err
}

// keepLines rewrites a recorded file keeping only lines that start with
// one of the prefixes (after spaces), so the full site configuration is
// never stored.
func keepLines(path string, prefixes ...string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var kept []string
	for _, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(l)
		for _, p := range prefixes {
			if strings.HasPrefix(t, p) {
				kept = append(kept, l)
				break
			}
		}
	}
	return os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600) //nolint:gosec // a fixture sdash wrote under --out
}

// fixedRunner answers every command with one recorded result; record uses
// it to parse output it already captured without running it again.
type fixedRunner execx.Result

func (f fixedRunner) Run(context.Context, ...string) (execx.Result, error) {
	return execx.Result(f), nil
}
