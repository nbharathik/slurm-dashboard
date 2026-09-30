package config

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Setting is one top-level config key; Settings drives validation, the UI and docs.
type Setting struct {
	Key     string
	Label   string   // on the Settings screen
	Help    string   // one line for "sdash config" and docs/config.md
	Choices []string // allowed values of a text setting; empty = free text
	// Alias maps an older spelling to a choice before checking.
	Alias func(string) string
	Min   int // range of a number setting
	Max   int
	// Screen is true if the Settings screen may change it (never commands).
	Screen bool
	// Group is the heading the Settings screen puts the row under.
	Group string

	// Exactly one of these points at the value.
	Str  func(*Config) *string
	Bool func(*Config) *bool
	Int  func(*Config) *int
	List func(*Config) *[]string
}

// Settings are the top-level settings, in the order the Settings screen
// and the docs show them.
var Settings = []Setting{
	{
		Key: "refresh", Label: "Refresh", Choices: Speeds, Screen: true, Group: "Data",
		Help: "how often data refreshes: fast, normal, slow, or manual (load once, then press r)",
		Str:  func(c *Config) *string { return &c.Refresh },
	},
	{
		Key: "hide_partitions", Label: "Hide partitions", Screen: true, Group: "Data",
		Help: `partitions left out of Nodes and the Overview, e.g. ["login"]`,
		List: func(c *Config) *[]string { return &c.HidePartitions },
	},
	{
		Key: "theme", Label: "Theme", Choices: Themes, Screen: true, Group: "Look",
		Help: "colours: auto follows the terminal; NO_COLOR is always honoured",
		Str:  func(c *Config) *string { return &c.Theme },
	},
	{
		Key: "layout", Label: "Layout", Choices: Layouts, Screen: true, Group: "Look",
		Help: "clean shows the essentials; detailed adds the extra columns and summary lines to each tab",
		Str:  func(c *Config) *string { return &c.Layout },
	},
	{
		Key: "ascii", Label: "Plain symbols", Screen: true, Group: "Look",
		Help: "ASCII instead of Unicode symbols, for terminals or fonts without them",
		Bool: func(c *Config) *bool { return &c.ASCII },
	},
	{
		Key: "mouse", Label: "Mouse", Screen: true, Group: "Look",
		Help: "clicks and the wheel; off frees the mouse for selecting text",
		Bool: func(c *Config) *bool { return &c.Mouse },
	},
	{
		Key: "start_tab", Label: "Start on", Choices: Tabs, Screen: true, Group: "Look",
		Help: "the tab sdash opens on",
		Alias: func(v string) string {
			if t, _, ok := model.ResolveTab(v); ok {
				return t
			}
			return v
		},
		Str: func(c *Config) *string { return &c.StartTab },
	},
	{
		Key: "notify", Label: "Job-end notices", Screen: true, Group: "Alerts",
		Help: "a message, the bell and a desktop notice when one of your jobs ends",
		Bool: func(c *Config) *bool { return &c.Notify },
	},
	{
		Key: "warn_time_left", Label: "Time-limit warning", Choices: TimeLeftChoices, Screen: true, Group: "Alerts",
		Help: "warn when a running job is this close to its time limit: off, 5m, 10m, 30m or 1h",
		Alias: func(v string) string {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "0", "0s", "0m", "none", "false", "no":
				return "off"
			}
			return v
		},
		Str: func(c *Config) *string { return &c.WarnTimeLeft },
	},
	{
		Key: "warn_waste", Label: "Idle-job warning", Screen: true, Group: "Alerts",
		Help: "after 30 minutes, note a running job that leaves most of its CPUs, memory or GPUs idle (one sstat call every 5 minutes)",
		Bool: func(c *Config) *bool { return &c.WarnWaste },
	},
	{
		Key: "storage_warn", Label: "Storage warning at", Min: 1, Max: 99, Screen: true, Group: "Alerts",
		Help: "percent of a quota that shows a warning",
		Int:  func(c *Config) *int { return &c.StorageWarn },
	},
	{
		Key: "storage_crit", Label: "Storage critical at", Min: 2, Max: 100, Screen: true, Group: "Alerts",
		Help: "percent of a quota that is critical",
		Int:  func(c *Config) *int { return &c.StorageCrit },
	},
	{
		Key: "shell", Label: "Shell in a job via", Choices: Shells, Screen: true, Group: "Other",
		Help: "how t opens a shell inside a running job: srun, or ssh where pam_slurm_adopt allows it",
		Str:  func(c *Config) *string { return &c.Shell },
	},
	{
		Key:  "cluster_name",
		Help: "the name shown in the header, instead of Slurm's ClusterName",
		Str:  func(c *Config) *string { return &c.ClusterName },
	},
	{
		Key:  "notify_command",
		Help: `also run this (with sh -c) when your job ends, e.g. 'notify-send "$SDASH_JOB_NAME $SDASH_JOB_STATE"'`,
		Str:  func(c *Config) *string { return &c.NotifyCommand },
	},
}

// Lookup returns the setting with key.
func Lookup(key string) (Setting, bool) {
	i := slices.IndexFunc(Settings, func(s Setting) bool { return s.Key == key })
	if i < 0 {
		return Setting{}, false
	}
	return Settings[i], true
}

// Format renders a setting's value in c for people: "fast", "on", "90",
// "login, debug" or "-".
func (s Setting) Format(c *Config) string {
	switch {
	case s.Str != nil:
		if v := *s.Str(c); v != "" {
			return v
		}
		return "-"
	case s.Bool != nil:
		if *s.Bool(c) {
			return "on"
		}
		return "off"
	case s.Int != nil:
		return strconv.Itoa(*s.Int(c))
	default:
		if l := *s.List(c); len(l) > 0 {
			return strings.Join(l, ", ")
		}
		return "-"
	}
}

// tomlValue renders the setting's value in c as TOML.
func (s Setting) tomlValue(c *Config) (string, error) {
	quote := func(v string) (string, error) {
		for _, r := range v {
			if r < 0x20 || r == 0x7f {
				return "", fmt.Errorf("%s: control characters cannot be saved", s.Key)
			}
		}
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`, nil
	}
	switch {
	case s.Str != nil:
		return quote(*s.Str(c))
	case s.Bool != nil:
		return strconv.FormatBool(*s.Bool(c)), nil
	case s.Int != nil:
		return strconv.Itoa(*s.Int(c)), nil
	}
	parts := []string{}
	for _, v := range *s.List(c) {
		q, err := quote(v)
		if err != nil {
			return "", err
		}
		parts = append(parts, q)
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

// Step moves a choice, bool or number setting by d (+1 or -1); text and lists are unchanged.
func (s Setting) Step(c *Config, d int) {
	switch {
	case s.Str != nil && len(s.Choices) > 0:
		p := s.Str(c)
		i := max(slices.Index(s.Choices, *p), 0)
		*p = s.Choices[(i+d+len(s.Choices))%len(s.Choices)]
	case s.Bool != nil:
		*s.Bool(c) = !*s.Bool(c)
	case s.Int != nil:
		p := s.Int(c)
		*p = min(max(*p+d, s.Min), s.Max)
	}
}

// partitionName matches Slurm partition names.
var partitionName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// check validates the setting's value in c against def, resetting it to
// def's value when it is invalid. It returns the problem, or "".
func (s Setting) check(c, def *Config) string {
	switch {
	case s.Str != nil && len(s.Choices) > 0:
		if s.Alias != nil {
			*s.Str(c) = s.Alias(*s.Str(c))
		}
		if p := s.Str(c); !slices.Contains(s.Choices, *p) {
			msg := fmt.Sprintf("%q is not one of %s; using %q", *p, strings.Join(s.Choices, ", "), *s.Str(def))
			*p = *s.Str(def)
			return msg
		}
	case s.Int != nil:
		if p := s.Int(c); *p < s.Min || *p > s.Max {
			msg := fmt.Sprintf("must be from %d to %d; using %d", s.Min, s.Max, *s.Int(def))
			*p = *s.Int(def)
			return msg
		}
	case s.List != nil:
		p := s.List(c)
		kept := []string{}
		var bad []string
		for _, v := range *p {
			if partitionName.MatchString(v) {
				kept = append(kept, v)
			} else {
				bad = append(bad, strconv.Quote(v))
			}
		}
		*p = kept
		if len(bad) > 0 {
			return "not a partition name: " + strings.Join(bad, ", ") + "; ignored"
		}
	}
	return ""
}
