package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/debuglog"
	"github.com/nbharathik/slurm-dashboard/internal/demo"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/meta"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/update"
)

// Env is the process environment the CLI runs in. Tests inject buffers and
// a fake environment.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
}

// app is the state shared by all subcommands of one invocation.
type app struct {
	env        Env
	configFlag string
	debug      bool
	profile    string

	paths    config.Paths
	pathsErr error
	log      *slog.Logger
	closer   io.Closer

	// runner, when set (tests, demo mode), replaces the real subprocess
	// runner.
	runner execx.Runner
	// noCache disables the capability and cluster-name caches (demo mode).
	noCache bool

	// siteConfig is the site-wide config read below the user's file
	// (config.SiteFile; tests point it elsewhere).
	siteConfig string

	// demoName is the --demo scenario; sim runs it (setup starts it).
	demoName string
	sim      *demo.Sim
	// now returns the current time; tests fix it.
	now func() time.Time
	// updater, when set (tests), replaces the GitHub release updater.
	updater *update.Updater
}

// clock returns the current time.
func (a *app) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// configPath is the user's config file: --config, else $SDASH_CONFIG, else
// the XDG default. It is always absolute, so it can be passed to an editor
// without being mistaken for an option.
func (a *app) configPath() string {
	flag := a.configFlag
	if flag == "" {
		flag = a.env.Getenv("SDASH_CONFIG")
	}
	if flag == "" {
		return a.paths.ConfigFile
	}
	p, _ := config.ExpandPath(flag, a.env.Getenv)
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// Main runs sdash with args and returns the process exit code. It never
// panics: a panic is written to a crash report and exits with 1.
func Main(ctx context.Context, args []string, env Env) int {
	a := &app{env: env, siteConfig: config.SiteFile}
	return run(ctx, newRoot(a), a, args)
}

func run(ctx context.Context, root *cobra.Command, a *app, args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			code = crashed(a, r, debug.Stack())
		}
	}()
	defer func() {
		if a.closer != nil {
			_ = a.closer.Close()
		}
	}()
	root.SetArgs(args)
	return exitWith(root.ExecuteContext(ctx), a.env.Stderr)
}

// crashed writes a crash report and tells the user where it is.
func crashed(a *app, recovered any, stack []byte) int {
	dir := a.paths.CacheDir
	if dir == "" {
		if p, err := config.ResolvePaths(a.env.Getenv); err == nil {
			dir = p.CacheDir
		} else {
			dir = os.TempDir()
		}
	}
	_, _ = fmt.Fprintf(a.env.Stderr, "%s crashed: %v\n", meta.AppName, recovered)
	if path, err := debuglog.WriteCrash(dir, recovered, stack); err == nil {
		_, _ = fmt.Fprintf(a.env.Stderr, "A crash report was saved to %s\nPlease attach it to a bug report: %s\n", path, meta.IssuesURL)
	} else {
		_, _ = fmt.Fprintf(a.env.Stderr, "Could not save a crash report (%v).\n%s\n", err, stack)
	}
	return ExitError
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   meta.AppName,
		Short: "A terminal dashboard for Slurm: your jobs, the queue, nodes, GPUs and quotas",
		Long: meta.AppName + ` shows your Slurm jobs, the whole queue, every node's free CPUs and
GPUs, your storage quotas and past-job efficiency, and lets you act on your
jobs from the keyboard or mouse.

It only runs Slurm commands you could run yourself, polls the controller
gently, and by default shows the exact command before any destructive action.`,
		Example: `  sdash                    open the dashboard
  sdash --demo             try it on a simulated cluster
  sdash config             show your settings (press , in the dashboard to change them)
  sdash version            show version and build information`,
		Version:       meta.Version,
		Args:          usageArgs(cobra.NoArgs),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return a.setup(cmd)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runDashboard(cmd.Context())
		},
	}
	root.SetIn(a.env.Stdin)
	root.SetOut(a.env.Stdout)
	root.SetErr(a.env.Stderr)
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })

	pf := root.PersistentFlags()
	pf.StringVar(&a.configFlag, "config", "", "config file `path` (default $SDASH_CONFIG, else ~/.config/sdash/config.toml)")
	pf.BoolVar(&a.debug, "debug", false, "write verbose records to the debug log (~/.cache/sdash/debug.log)")
	pf.StringVar(&a.profile, "profile", "", "use another cluster from [profiles.NAME] in the config")
	pf.StringVar(&a.demoName, "demo", "", "use a simulated cluster (no Slurm needed); --demo=hetero for the mixed one")
	pf.Lookup("demo").NoOptDefVal = "default"

	_ = root.RegisterFlagCompletionFunc("demo", fixedCompletions(demo.Names()...))
	_ = root.RegisterFlagCompletionFunc("profile", a.completeProfiles)

	root.AddCommand(newVersionCmd(), newConfigCmd(a), newStatusCmd(a), newJobsCmd(a), newQueueCmd(a), newNodesCmd(a),
		newPartitionsCmd(a), newGPUsCmd(a), newFreeCmd(a), newHistoryCmd(a), newDoctorCmd(a), newRecordCmd(a), newQuotaCmd(a),
		newWhyCmd(a), newEffCmd(a), newLogsCmd(a), newUpdateCmd(a), newUsageCmd(a))
	return root
}

// setup resolves paths and opens the debug log before any subcommand runs.
// Neither failure is fatal here: commands that need the paths call
// requirePaths, and a missing debug log only matters with --debug.
func (a *app) setup(cmd *cobra.Command) error {
	a.log = debuglog.Discard()
	a.paths, a.pathsErr = config.ResolvePaths(a.env.Getenv)
	if a.pathsErr != nil {
		return nil
	}
	log, closer, err := debuglog.Open(a.paths.CacheDir, a.debug)
	a.log, a.closer = log, closer
	if err != nil && a.debug {
		_, _ = fmt.Fprintf(a.env.Stderr, "%s: debug log unavailable: %v\n", meta.AppName, err)
	}
	if cmd != nil {
		a.log.Debug("start", "version", meta.Version, "command", cmd.CommandPath())
	}
	return a.startDemo()
}

// startDemo runs --demo: Slurm commands, the user, quotas and job logs all
// come from the scenario, and no cache is read or written.
func (a *app) startDemo() error {
	if a.demoName == "" {
		return nil
	}
	sc, err := demo.Load(a.demoName)
	if err != nil {
		return usageError{err}
	}
	a.sim = demo.New(sc, state.RealClock{})
	a.runner = execx.WithHistory(a.sim.Runner(), execx.DefaultHistorySize, a.log)
	env := a.env.Getenv
	a.env.Getenv = func(k string) string {
		if k == "USER" {
			return sc.User
		}
		return env(k)
	}
	a.noCache = true
	return nil
}

// uid is the numeric user ID (the scenario's in demo mode).
func (a *app) uid() string {
	if a.sim != nil {
		return strconv.Itoa(a.sim.Scenario().UID)
	}
	return strconv.Itoa(os.Getuid())
}

// logFS reads job logs (the scenario's in demo mode).
func (a *app) logFS() logs.FS {
	if a.sim != nil {
		return a.sim.LogFS()
	}
	return logs.OS
}

// requirePaths reports why the XDG paths could not be resolved, if they
// could not.
func (a *app) requirePaths() error {
	if a.pathsErr != nil && a.configFlag == "" {
		return a.pathsErr
	}
	return nil
}
