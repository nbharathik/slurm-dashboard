package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/meta"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/update"
)

// Check statuses.
const (
	checkOK   = "ok"
	checkWarn = "warn"
	checkFail = "fail"
	checkInfo = "info"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

type doctorDoc struct {
	Schema int     `json:"schema"`
	Checks []check `json:"checks"`
}

func newDoctorCmd(a *app) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the environment: Slurm commands, controller, accounting, terminal and config",
		Long: `Check everything sdash depends on and print one line per check with a fix
hint for each problem: ✓ fine, ! works with limits, ✗ broken. Only read-only
commands run. Exit status is 1 when a check fails and 3 when the Slurm
commands are missing.`,
		Example: `  sdash doctor
  sdash doctor --json`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks, slurmMissing := a.doctor(cmd.Context())
			if asJSON {
				if err := writeJSON(cmd.OutOrStdout(), doctorDoc{Schema: 1, Checks: checks}); err != nil {
					return err
				}
			} else if err := renderChecks(cmd.OutOrStdout(), checks); err != nil {
				return err
			}
			switch {
			case slurmMissing:
				return fmt.Errorf("%w", errSlurmMissing)
			case hasFailure(checks):
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func hasFailure(checks []check) bool {
	for _, c := range checks {
		if c.Status == checkFail {
			return true
		}
	}
	return false
}

// essential Slurm commands; the others enable optional features.
var slurmCommands = []struct {
	name      string
	essential bool
	feature   string
}{
	{"squeue", true, "job lists"},
	{"sinfo", true, "version detection"},
	{"scontrol", true, "nodes, partitions and job details"},
	{"sacct", false, "history, efficiency and notifications"},
	{"sstat", false, "live usage of running jobs"},
	{"sshare", false, "fairshare"},
	{"sprio", false, "priority factors"},
	{"scancel", false, "cancelling jobs"},
	{"sbatch", false, "submitting jobs"},
	{"srun", false, "/shell and /gpu"},
}

func (a *app) doctor(ctx context.Context) (checks []check, slurmMissing bool) {
	add := func(name, status, detail, hint string) {
		checks = append(checks, check{Name: name, Status: status, Detail: detail, Hint: hint})
	}

	missingEssential := 0
	for _, c := range slurmCommands {
		path, err := lookPath(a, c.name)
		switch {
		case err == nil:
			add(c.name, checkOK, path, "")
		case c.essential:
			missingEssential++
			add(c.name, checkFail, "not found in PATH", "Run sdash on a Slurm login node, or load the Slurm module (module load slurm).")
		default:
			add(c.name, checkWarn, "not found in PATH", "Without it sdash has no "+c.feature+".")
		}
	}
	if missingEssential > 0 {
		a.doctorStorage(ctx, add, a.loadConfig(), a.env.Getenv("USER"))
		a.doctorLocal(add)
		return checks, true
	}

	r, err := a.newRunner()
	if err != nil {
		add("runner", checkFail, err.Error(), "")
		return checks, false
	}
	user, err := slurm.User(ctx, a.env.Getenv, r)
	if err != nil {
		add("user", checkFail, err.Error(), "Set $USER.")
	} else {
		detail := user
		if res, err := r.Run(execx.WithLabel(ctx, "user"), "id", "-un"); err == nil {
			if id := strings.TrimSpace(string(res.Stdout)); id != "" && id != user {
				add("user", checkWarn, fmt.Sprintf("$USER is %q but id -un says %q", user, id), "sdash uses $USER; make sure it matches your Slurm account.")
			} else {
				add("user", checkOK, detail, "")
			}
		} else {
			add("user", checkOK, detail+" (from $USER)", "")
		}
	}

	caps, err := slurm.Probe(ctx, r, user)
	switch {
	case err != nil:
		add("slurm version", checkFail, err.Error(), "sinfo --version must work.")
	case !caps.AtLeast(20, 11):
		add("slurm version", checkWarn, caps.Version, "Slurm older than 20.11: sdash uses -u $USER instead of --me and disables /shell.")
	default:
		add("slurm version", checkOK, caps.Version, "")
	}
	cfg := a.loadConfig()
	override := cfg.ClusterName
	if p, ok := cfg.Profiles[a.profile]; ok && p.ClusterName != "" {
		override = p.ClusterName
	}
	info, err := slurm.ClusterInfo(ctx, r, "", a.clock())
	add(clusterCheck(info.Name, err, override))
	if caps.Major > 0 { // the version was read
		for _, f := range slurm.Features(caps, info) {
			switch f.Status {
			case slurm.Available:
				add(f.Name, checkOK, "available", "")
			case slurm.Limited:
				add(f.Name, checkInfo, "limited: "+f.Note, "")
			default:
				add(f.Name, checkInfo, "not available: "+f.Note, "")
			}
		}
	}

	cmds := slurm.Commands{Caps: caps, User: user}
	start := time.Now()
	_, err = r.Run(execx.WithLabel(ctx, "doctor-controller"), cmds.MyJobs()...)
	took := time.Since(start).Round(time.Millisecond)
	switch {
	case err != nil:
		add("controller", checkFail, execx.FirstLine([]byte(err.Error())), "squeue must reach slurmctld; try again later or ask your admins.")
	case took > 2*time.Second:
		add("controller", checkWarn, fmt.Sprintf("squeue --me took %s", took), "The controller is slow; sdash backs off automatically.")
	default:
		add("controller", checkOK, fmt.Sprintf("squeue --me in %s", took), "")
	}
	{
		start = time.Now()
		_, err = r.Run(execx.WithLabel(ctx, "doctor-accounting"), "sacct", "-n", "-X", "-S", "now-1hours", "-o", "JobID")
		took = time.Since(start).Round(time.Millisecond)
		switch {
		case err != nil && info.AccountingOff():
			add("accounting", checkInfo, "off, as configured", "")
		case err != nil:
			add("accounting", checkWarn, execx.FirstLine([]byte(err.Error())), "Usage, efficiency and job-end notifications need slurmdbd accounting.")
		case took > 5*time.Second:
			add("accounting", checkWarn, fmt.Sprintf("sacct took %s", took), "Accounting is slow; the Usage tab loads slowly.")
		default:
			add("accounting", checkOK, fmt.Sprintf("sacct in %s", took), "")
		}
	}

	a.doctorStorage(ctx, add, cfg, user)
	a.doctorLocal(add)
	return checks, false
}

// genericClusterNames are ClusterName values that say nothing in a header.
var genericClusterNames = []string{"cluster", "slurm", "linux", "default"}

// clusterCheck reports the cluster name, warning when Slurm's name is
// generic and the config does not override it.
func clusterCheck(name string, err error, override string) (string, string, string, string) {
	const hint = "Set cluster_name in the config file (sdash config edit) for a nicer header."
	switch {
	case override != "" && err == nil && name != override:
		return "cluster", checkOK, override + " (from the config; Slurm calls it " + name + ")", ""
	case override != "":
		return "cluster", checkOK, override + " (from the config)", ""
	case err != nil:
		return "cluster", checkWarn, err.Error(), hint
	case slices.Contains(genericClusterNames, strings.ToLower(name)):
		return "cluster", checkWarn, fmt.Sprintf("ClusterName is generic (%q)", name), hint
	}
	return "cluster", checkOK, name, ""
}

// pathCheck reports whether typing "sdash" runs this binary.
func pathCheck(onPath string, lookErr error, self string) (string, string, string, string) {
	switch {
	case lookErr != nil:
		return "on PATH", checkWarn, "sdash is not on your PATH", "Run make install (or the install script), or add its directory to PATH."
	case self != "" && onPath != self:
		return "on PATH", checkWarn, fmt.Sprintf("PATH runs %s, not this binary (%s)", onPath, self), "Remove the older copy, or put this one first in PATH."
	}
	return "on PATH", checkOK, onPath, ""
}

// resolved follows symlinks, keeping p if that fails.
func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// doctorLocal adds the checks that need no Slurm: environment, terminal
// and config.
func (a *app) doctorLocal(add func(name, status, detail, hint string)) {
	if a.runner == nil { // tests run with a fake runner and no installed sdash
		self, _ := os.Executable()
		onPath, err := execx.LookPath(meta.AppName)
		add(pathCheck(resolved(onPath), err, resolved(self)))
		method, hint := update.Method(resolved(self), meta.Version, meta.Build)
		add("install method", checkInfo, method, "Update with: "+hint)
	}

	var fmtVars []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		for _, p := range []string{"SQUEUE_", "SINFO_", "SACCT_", "SSTAT_", "SPRIO_", "SSHARE_"} {
			if strings.HasPrefix(k, p) {
				fmtVars = append(fmtVars, k)
			}
		}
		if k == "SLURM_TIME_FORMAT" {
			fmtVars = append(fmtVars, k)
		}
	}
	if len(fmtVars) > 0 {
		add("format variables", checkInfo, strings.Join(slices.Sorted(slices.Values(fmtVars)), ", "), "sdash ignores these for its own commands; your shell keeps them.")
	}

	getenv := a.env.Getenv
	term := getenv("TERM")
	switch term {
	case "", "dumb":
		add("terminal", checkWarn, fmt.Sprintf("TERM=%q", term), "The dashboard needs a real terminal; the CLI commands still work.")
	default:
		detail := "TERM=" + term
		if ct := getenv("COLORTERM"); ct != "" {
			detail += ", COLORTERM=" + ct
		}
		if getenv("NO_COLOR") != "" {
			detail += ", NO_COLOR set (monochrome)"
		}
		if getenv("TMUX") != "" {
			detail += ", inside tmux"
		}
		add("terminal", checkOK, detail, "")
	}
	if utf8Locale(getenv) {
		add("locale", checkOK, "UTF-8", "")
	} else {
		add("locale", checkWarn, "not UTF-8", "sdash switches to ASCII symbols; set LANG=en_US.UTF-8 (or C.UTF-8) for icons.")
	}

	if err := a.requirePaths(); err != nil {
		add("config", checkWarn, err.Error(), "")
		return
	}
	path := a.configPath()
	cfg, issues, err := config.Load(path, getenv)
	_, statErr := os.Stat(path)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		add("config", checkOK, "no file (defaults) at "+path, "Not needed; press , in sdash to change settings, or run: sdash config edit")
	case err != nil:
		add("config", checkFail, err.Error(), "")
	case config.HasErrors(issues):
		add("config", checkWarn, fmt.Sprintf("%s: %s", path, plural(len(issues), "problem")), "Run: sdash config")
	case len(issues) > 0:
		add("config", checkOK, fmt.Sprintf("%s (%s)", path, plural(len(issues), "warning")), "Run: sdash config")
	default:
		add("config", checkOK, path, "")
	}
	mouse := "on"
	if !cfg.Mouse {
		mouse = "off"
	}
	add("mouse", checkInfo, mouse, "Hold Shift (Option in iTerm2) to select text while the mouse is on.")
}

func utf8Locale(getenv func(string) string) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

func lookPath(a *app, name string) (string, error) {
	if a.runner != nil { // tests: pretend every tool exists
		return "/usr/bin/" + name, nil
	}
	return execx.LookPath(name)
}

func renderChecks(w io.Writer, checks []check) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	mark := map[string]string{checkOK: "✓", checkWarn: "!", checkFail: "✗", checkInfo: "·"}
	for _, c := range checks {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", mark[c.Status], c.Name, c.Detail)
		if c.Hint != "" && c.Status != checkOK {
			_, _ = fmt.Fprintf(tw, "\t\t→ %s\n", c.Hint)
		}
	}
	_, _ = fmt.Fprintf(tw, "\n%s %s\n", meta.AppName, meta.Version)
	return tw.Flush()
}
