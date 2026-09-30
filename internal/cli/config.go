package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

func newConfigCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show your settings, where each comes from, and any problems",
		Long: `Show every setting with its value and where it comes from: the default,
the site file ` + config.SiteFile + `, or your file. Problems in the files are
listed with their line; the status is 1 when there are errors.

Change settings with , in the dashboard (it saves them to your file), or
edit the file with 'sdash config edit'. docs/config.md describes each one.`,
		Example: `  sdash config
  sdash config edit`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return a.showConfig(cmd.OutOrStdout()) },
	}
	initCmd, pathCmd, validateCmd := newConfigInitCmd(a), newConfigPathCmd(a), newConfigValidateCmd(a)
	initCmd.Hidden, pathCmd.Hidden, validateCmd.Hidden = true, true, true
	cmd.AddCommand(initCmd, pathCmd, validateCmd, newConfigEditCmd(a))
	return cmd
}

// showConfig prints every setting, where it comes from, and the problems
// of each file.
func (a *app) showConfig(out io.Writer) error {
	if err := a.requirePaths(); err != nil {
		return err
	}
	cfg := a.loadConfig()
	layers := a.configLayers()
	from := config.SetIn(layers)
	origin := func(key string) string {
		switch from[key] {
		case "":
			return "default"
		case a.siteConfig:
			return "site"
		}
		return "yours"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %-22s %s\n", "SETTING", "VALUE", "FROM")
	for _, st := range config.Settings {
		val := st.Format(&cfg)
		if st.Key == "notify_command" && val != "-" {
			val = "set"
		}
		fmt.Fprintf(&b, "%-16s %-22s %s\n", st.Key, val, origin(st.Key))
	}
	names := fmt.Sprintf("%d", len(cfg.GPUNames))
	storage := "detected"
	if len(cfg.Storage) > 0 {
		storage = plural(len(cfg.Storage), "location")
	}
	fmt.Fprintf(&b, "%-16s %-22s %s\n", "gpu_names", names, origin("gpu_names"))
	fmt.Fprintf(&b, "%-16s %-22s %s\n", "storage", storage, origin("storage"))
	fmt.Fprintf(&b, "%-16s %-22s %s\n", "profiles", fmt.Sprintf("%d", len(cfg.Profiles)), origin("profiles"))
	b.WriteString("\nFiles:\n")
	for _, p := range layers {
		state := "not there"
		if _, err := os.Stat(p); err == nil {
			state = "read"
		}
		fmt.Fprintf(&b, "  %s (%s)\n", p, state)
	}
	fmt.Fprintf(&b, "Change settings with , in the dashboard, or: %s config edit\n\n", meta.AppName)
	if _, err := io.WriteString(out, b.String()); err != nil {
		return err
	}
	return a.validateConfig(out)
}

func newConfigInitCmd(a *app) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a commented config file with every default",
		Long: `Write a commented config file listing every option with its default.

An existing file is never overwritten unless --force is given; the old file
is then kept next to it with a .bak suffix.`,
		Example: `  sdash config init
  sdash config init --force`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.requirePaths(); err != nil {
				return err
			}
			path := a.configPath()
			backup, err := config.WriteDefault(path, force)
			if err != nil {
				return err
			}
			a.log.Info("config init", "path", path, "backup", backup)
			var msg strings.Builder
			fmt.Fprintf(&msg, "Wrote %s\n", path)
			if backup != "" {
				fmt.Fprintf(&msg, "The previous file was kept as %s\n", backup)
			}
			fmt.Fprintf(&msg, "Edit it with '%s config edit' and check it with '%s config validate'.\n", meta.AppName, meta.AppName)
			_, err = io.WriteString(cmd.OutOrStdout(), msg.String())
			return err
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing file (keeps a .bak copy)")
	return cmd
}

func newConfigPathCmd(a *app) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the config file path (--all: every file read, in order)",
		Long: `Print your config file: --config, else $SDASH_CONFIG, else
~/.config/sdash/config.toml. With --all, list every file sdash reads, the
site file ` + config.SiteFile + ` first; later files override earlier ones.`,
		Example: `  $EDITOR "$(sdash config path)"
  sdash config path --all`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.requirePaths(); err != nil {
				return err
			}
			if !all {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), a.configPath())
				return err
			}
			var b strings.Builder
			for _, p := range a.configLayers() {
				state := "missing"
				if _, err := os.Stat(p); err == nil {
					state = "read"
				}
				fmt.Fprintf(&b, "%s  (%s)\n", p, state)
			}
			_, err := io.WriteString(cmd.OutOrStdout(), b.String())
			return err
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "list the site file and your file")
	return cmd
}

func newConfigValidateCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check the config file and report problems with line numbers",
		Long: `Check the config file. Each problem is printed as FILE:LINE:COL with the key
and what sdash does about it. Exits with status 1 if there are errors;
warnings alone exit with 0.`,
		Example: `  sdash config validate
  sdash --config ./test.toml config validate`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.validateConfig(cmd.OutOrStdout())
		},
	}
}

// validateConfig checks every config file that exists and prints its
// issues. It returns errSilent when there are errors, so the exit status
// is 1.
func (a *app) validateConfig(out io.Writer) error {
	if err := a.requirePaths(); err != nil {
		return err
	}
	var msg strings.Builder
	result := error(nil)
	found := 0
	for _, path := range a.configLayers() {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		found++
		_, issues, err := config.Load(path, a.env.Getenv)
		if err != nil {
			return err
		}
		for _, is := range config.TrustIssues(path) {
			is.File = ""
			issues = append(issues, is)
		}
		var errs, warns int
		for _, is := range issues {
			fmt.Fprintf(&msg, "%s:%s\n", path, is)
			if is.Level == config.Error {
				errs++
			} else {
				warns++
			}
		}
		switch {
		case errs > 0:
			fmt.Fprintf(&msg, "%s: %s, %s. sdash still starts, using defaults for the invalid values.\n",
				path, plural(errs, "error"), plural(warns, "warning"))
			result = errSilent
		case warns > 0:
			fmt.Fprintf(&msg, "%s: OK with %s.\n", path, plural(warns, "warning"))
		default:
			fmt.Fprintf(&msg, "%s: OK\n", path)
		}
	}
	if found == 0 {
		fmt.Fprintf(&msg, "No config file yet; the defaults above are used.\n")
	}
	if _, err := io.WriteString(out, msg.String()); err != nil {
		return err
	}
	return result
}

func newConfigEditCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the config file in $VISUAL or $EDITOR, then validate it",
		Long: `Open the config file in $VISUAL, else $EDITOR, else vi. The file is created
from the defaults first if it does not exist. After the editor exits the file
is validated.`,
		Example: `  sdash config edit
  EDITOR=nano sdash config edit`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.requirePaths(); err != nil {
				return err
			}
			path := a.configPath()
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				if _, err := config.WriteDefault(path, false); err != nil {
					return err
				}
			}
			editor := strings.Fields(a.env.Getenv("VISUAL"))
			if len(editor) == 0 {
				editor = strings.Fields(a.env.Getenv("EDITOR"))
			}
			if len(editor) == 0 {
				editor = []string{"vi"}
			}
			c, err := execx.NewInteractive("", append(editor, path)...)
			if err != nil {
				return fmt.Errorf("editor %q: %w", strings.Join(editor, " "), err)
			}
			c.SetStdin(a.env.Stdin)
			c.SetStdout(a.env.Stdout)
			c.SetStderr(a.env.Stderr)
			if err := c.Run(); err != nil {
				return fmt.Errorf("editor: %w", err)
			}
			return a.validateConfig(cmd.OutOrStdout())
		},
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
