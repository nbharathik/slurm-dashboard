package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

// Shell completion asks Slurm for job IDs, node and partition names. Each
// lookup has a short timeout and is cached, so pressing tab repeatedly
// does not load the controller.
const (
	completeTimeout = 3 * time.Second
	completeTTL     = 30 * time.Second
)

// completionSetup finishes the setup for shell completion: cobra runs the
// persistent pre-run for its own __complete command, before the target
// command's flags (--demo, --config) are parsed.
func (a *app) completionSetup() {
	if a.log == nil {
		_ = a.setup(nil)
	}
	if a.demoName != "" && a.sim == nil {
		_ = a.startDemo()
	}
}

func fixedCompletions(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeLookup runs a read-only Slurm query for completion, cached in
// the cache directory for completeTTL.
func (a *app) completeLookup(ctx context.Context, name string, argv ...string) []string {
	a.completionSetup()
	path := ""
	if a.pathsErr == nil && a.paths.CacheDir != "" && a.sim == nil {
		path = filepath.Join(a.paths.CacheDir, "complete-"+name+".txt")
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < completeTTL {
			if b, err := os.ReadFile(path); err == nil {
				return strings.Fields(string(b))
			}
		}
	}
	r := a.runner
	if r == nil {
		if _, err := execx.LookPath(argv[0]); err != nil {
			return nil
		}
		r = execx.NewReal(execx.Options{Logger: a.log})
	}
	ctx, cancel := context.WithTimeout(ctx, completeTimeout)
	defer cancel()
	res, err := r.Run(execx.WithLabel(ctx, "complete-"+name), argv...)
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range strings.Fields(string(res.Stdout)) {
		if f = strings.TrimSuffix(f, "*"); f != "" && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	if path != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		_ = os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o600)
	}
	return out
}

func (a *app) completeJobs(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.completeLookup(cmd.Context(), "jobs", "squeue", "--me", "-h", "-o", "%i"), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeNodes(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.completeLookup(cmd.Context(), "nodes", "sinfo", "-h", "-N", "-o", "%N"), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completePartitions(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return a.completeLookup(cmd.Context(), "partitions", "sinfo", "-h", "-o", "%P"), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeProfiles(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	a.completionSetup()
	var out []string
	for name := range a.loadConfig().Profiles {
		out = append(out, name)
	}
	slices.Sort(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}
