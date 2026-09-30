package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/storage"
)

// storageSetup finds the storage locations and builds their checker.
func (a *app) storageSetup(cfg config.Config, user string) ([]storage.Location, *storage.Checker) {
	mountData, _ := os.ReadFile("/proc/self/mounts")
	env := storage.Env{
		Getenv: a.env.Getenv, User: user, Mounts: storage.ParseMounts(mountData),
		EvalSymlinks: filepath.EvalSymlinks,
		IsDir: func(p string) bool {
			fi, err := os.Stat(p)
			return err == nil && fi.IsDir()
		},
	}
	locs := storage.Locate(cfg.Storage, env)
	c := &storage.Checker{User: user, StatFS: storage.SysStatFS, LookPath: execx.LookPath, Now: a.clock}
	if a.runner != nil { // tests
		c.Runner = a.runner
		return locs, c
	}
	r := storage.NewRunner(locs, execx.Options{Logger: a.log})
	c.Runner, c.Shell = r, storage.ShellFor(r)
	return locs, c
}

// quotas returns the storage check for user (the scenario's in demo mode).
func (a *app) quotas(cfg config.Config, user string) func(context.Context) []model.Quota {
	if a.sim != nil {
		return func(context.Context) []model.Quota { return a.sim.Quotas() }
	}
	locs, c := a.storageSetup(cfg, user)
	return func(ctx context.Context) []model.Quota { return c.Check(ctx, locs) }
}

// loadStorage fills the store's storage source.
func (rt *slurmRuntime) loadStorage(ctx context.Context, a *app) {
	rt.store.Storage.Data = a.quotas(rt.cfg, rt.sources.Cmd.User)(ctx)
	rt.store.Storage.Has = true
}

func newQuotaCmd(a *app) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "quota",
		Short: "Storage usage and quotas for your locations",
		Long: `Show usage and quota for $HOME and your scratch, work and project spaces
(or the [[storage]] entries in the config). The filesystem type decides the
tool: lfs quota (Lustre), mmlsquota (GPFS), beegfs-ctl, quota, or statfs.`,
		Example: `  sdash quota
  sdash quota --json`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := a.loadConfig()
			r := a.runner
			if r == nil {
				r = execx.NewReal(execx.Options{Logger: a.log})
			}
			user, err := slurm.User(cmd.Context(), a.env.Getenv, r)
			if err != nil {
				return err
			}
			qs := a.quotas(cfg, user)(cmd.Context())
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), report.Quotas(report.Header{Schema: report.Schema, User: user, GeneratedAt: a.clock().UTC()}, qs))
			}
			return renderQuotas(cmd.OutOrStdout(), qs)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func renderQuotas(w io.Writer, qs []model.Quota) error {
	if len(qs) == 0 {
		_, err := fmt.Fprintln(w, "No storage locations found. Add [[storage]] entries to the config.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "LOCATION\tPATH\tFS\tBACKEND\tUSAGE\tFILES")
	for _, q := range qs {
		rq := report.FromQuota(q)
		files := "-"
		if q.UsedFiles > 0 {
			files = fmt.Sprint(q.UsedFiles)
			if lim := max(q.SoftFiles, 0); lim > 0 {
				files += fmt.Sprintf(" of %d", lim)
			} else if q.HardFiles > 0 {
				files += fmt.Sprintf(" of %d", q.HardFiles)
			}
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", q.Label, q.Path, orDash(q.FSType), q.Backend, quotaLine(rq), files)
		if q.Grace != "" {
			_, _ = fmt.Fprintf(tw, "\t\t\t\tgrace %s\t\n", q.Grace)
		}
		if q.Err != "" && q.UsedBytes > 0 {
			_, _ = fmt.Fprintf(tw, "\t\t\t\tstale: %s\t\n", q.Err)
		}
		if q.Note != "" {
			_, _ = fmt.Fprintf(tw, "\t\t\t\t%s\t\n", strings.TrimSpace(q.Note))
		}
	}
	return tw.Flush()
}

// doctorStorage lists each location with its filesystem, backend and one
// sample result.
func (a *app) doctorStorage(ctx context.Context, add func(name, status, detail, hint string), cfg config.Config, user string) {
	locs, c := a.storageSetup(cfg, user)
	if len(locs) == 0 {
		add("storage", checkWarn, "no locations found", "Add [[storage]] entries to the config file.")
		return
	}
	for i, q := range c.Check(ctx, locs) {
		l := locs[i]
		where := fmt.Sprintf("%s on %s (%s), %s", l.Path, orDash(l.Mount.Point), orDash(l.Mount.FSType), q.Backend)
		switch {
		case q.Err != "":
			add("storage "+l.Label, checkWarn, where+": "+q.Err, "Set backend (or a site command) for this location in [[storage]].")
		case q.IsFilesystemTotal:
			add("storage "+l.Label, checkInfo, where+": "+quotaLine(report.FromQuota(q)), "No personal quota found; sdash shows the filesystem's size.")
		default:
			add("storage "+l.Label, checkOK, where+": "+quotaLine(report.FromQuota(q)), "")
		}
	}
}
