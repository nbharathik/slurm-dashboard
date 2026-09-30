package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// slurmRuntime is everything a data command needs.
type slurmRuntime struct {
	cfg     config.Config
	runner  execx.Runner
	sources *state.Sources
	store   *state.Store
	errs    []error
}

// configLayers are the config files read, lowest first: the site file,
// then the user's.
func (a *app) configLayers() []string {
	var out []string
	if a.siteConfig != "" {
		out = append(out, a.siteConfig)
	}
	return append(out, a.configPath())
}

// loadConfig reads the config files; problems are logged, never fatal.
func (a *app) loadConfig() config.Config {
	if err := a.requirePaths(); err != nil {
		return config.Default()
	}
	cfg, issues, err := config.LoadLayers(a.configLayers(), a.env.Getenv)
	if err != nil {
		a.log.Warn("config unreadable", "err", err)
	}
	for _, is := range issues {
		a.log.Warn("config issue", "issue", is.String())
	}
	return cfg
}

// newRunner returns the subprocess runner for Slurm commands. With
// --profile, commands see the profile's SLURM_CONF.
func (a *app) newRunner() (execx.Runner, error) {
	if a.runner != nil {
		return a.runner, nil
	}
	if _, err := execx.LookPath("sinfo"); err != nil {
		return nil, fmt.Errorf("%w (sinfo is not in PATH; are you on a Slurm login node?)", errSlurmMissing)
	}
	opts := execx.Options{Logger: a.log}
	if p, ok := a.activeProfile(); ok {
		opts.BaseEnv = append(os.Environ(), "SLURM_CONF="+p.SlurmConf)
	}
	return execx.NewReal(opts), nil
}

// activeProfile returns the --profile entry, if one is selected and
// defined (checkProfile reports an unknown name first).
func (a *app) activeProfile() (config.Profile, bool) {
	if a.profile == "" {
		return config.Profile{}, false
	}
	p, ok := a.loadConfig().Profiles[a.profile]
	return p, ok
}

// checkProfile rejects an unknown --profile.
func (a *app) checkProfile(cfg config.Config) error {
	if a.profile == "" {
		return nil
	}
	if _, ok := cfg.Profiles[a.profile]; ok {
		return nil
	}
	var names []string
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	if len(names) == 0 {
		return usageError{fmt.Errorf("no profile %q: the config has no [profiles.NAME] sections", a.profile)}
	}
	return usageError{fmt.Errorf("no profile %q (have: %s)", a.profile, strings.Join(slices.Sorted(slices.Values(names)), ", "))}
}

// slurmRuntime resolves the user, Slurm capabilities and cluster name.
func (a *app) slurmRuntime(ctx context.Context) (*slurmRuntime, error) {
	cfg := a.loadConfig()
	if err := a.checkProfile(cfg); err != nil {
		return nil, err
	}
	r, err := a.newRunner()
	if err != nil {
		return nil, err
	}
	user, err := slurm.User(ctx, a.env.Getenv, r)
	if err != nil {
		return nil, err
	}
	cacheDir := a.paths.CacheDir
	switch {
	case a.noCache:
		cacheDir = ""
	case a.profile != "":
		// Capabilities and names differ per cluster.
		cacheDir = filepath.Join(cacheDir, "profiles", a.profile)
	}
	caps, err := slurm.CachedProbe(ctx, r, user, cacheDir, a.clock())
	if err != nil {
		if errors.Is(err, execx.ErrNotFound) {
			return nil, fmt.Errorf("%w: %v", errSlurmMissing, err)
		}
		return nil, fmt.Errorf("cannot read the Slurm version: %w", err)
	}
	cluster := cfg.ClusterName
	if p, ok := cfg.Profiles[a.profile]; ok && p.ClusterName != "" {
		cluster = p.ClusterName
	}
	// Best effort: the name falls back to none and PrivateData to off.
	info, _ := slurm.ClusterInfo(ctx, r, cacheDir, a.clock())
	if cluster == "" {
		cluster = info.Name
	}
	rt := &slurmRuntime{
		cfg:    cfg,
		runner: r,
		sources: &state.Sources{
			Runner: r,
			Cmd:    slurm.Commands{Caps: caps, User: user},
			Log:    a.log,
		},
		store: &state.Store{
			User: user, UID: a.uid(), ClusterName: cluster, Caps: caps, PrivateJobs: info.HidesJobs(), Site: info,
			StorageWarn: cfg.StorageWarn, StorageCrit: cfg.StorageCrit,
			TimeLeftWarn: cfg.TimeLeftWarn(), WarnWaste: cfg.WarnWaste,
		},
	}
	return rt, nil
}

// fail records a source error for the report and returns false.
func (rt *slurmRuntime) fail(source string, err error) bool {
	if err != nil {
		rt.errs = append(rt.errs, fmt.Errorf("%s: %w", source, err))
		return false
	}
	return true
}

// loadJobs fills my jobs (or all jobs), queue ranks and cluster GPUs.
func (rt *slurmRuntime) loadJobs(ctx context.Context, all bool) {
	st := rt.store
	var err error
	if all {
		st.AllJobs.Data, err = rt.sources.AllJobs(ctx)
		st.AllJobs.Has = rt.fail("alljobs", err)
	}
	st.MyJobs.Data, err = rt.sources.MyJobs(ctx)
	st.MyJobs.Has = rt.fail("myjobs", err)
	st.Cluster.Data, err = rt.sources.Cluster(ctx, nil)
	st.Cluster.Has = rt.fail("cluster", err)
	if parts := state.PartitionsOfPending(st.MyJobs.Data); len(parts) > 0 {
		st.QueueRank.Data, err = rt.sources.QueueRank(ctx, parts)
		st.QueueRank.Has = rt.fail("queuerank", err)
	}
}

// loadNodes fills nodes (and cluster jobs if not loaded yet).
func (rt *slurmRuntime) loadNodes(ctx context.Context) {
	st := rt.store
	var err error
	st.Nodes.Data, err = rt.sources.Nodes(ctx)
	st.Nodes.Has = rt.fail("nodes", err)
	if !st.Cluster.Has {
		st.Cluster.Data, err = rt.sources.Cluster(ctx, nil)
		st.Cluster.Has = rt.fail("cluster", err)
	}
}

// errStrings formats the collected source errors.
func (rt *slurmRuntime) errStrings() []string {
	out := []string{}
	for _, e := range rt.errs {
		out = append(out, e.Error())
	}
	return out
}

// hardFail returns an error when the main source failed, so the command
// exits non-zero; missing Slurm commands map to exit 3.
func (rt *slurmRuntime) hardFail() error {
	for _, e := range rt.errs {
		if state.KindOf(e) == state.ErrMissing {
			return fmt.Errorf("%w: %v", errSlurmMissing, e)
		}
	}
	if len(rt.errs) > 0 {
		return fmt.Errorf("%s", rt.errs[0].Error())
	}
	return nil
}
