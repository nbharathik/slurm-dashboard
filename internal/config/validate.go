package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// validator checks decoded values, resets invalid ones to their defaults,
// and records issues with line numbers.
type validator struct {
	layout layout
	getenv func(string) string
	issues []Issue
}

func (v *validator) add(level Level, key, format string, args ...any) {
	p := v.layout.lookup(key)
	v.issues = append(v.issues, Issue{Level: level, Line: p.Line, Col: p.Col, Key: key, Msg: fmt.Sprintf(format, args...)})
}

func (v *validator) validate(c *Config) {
	def := Default()
	for _, st := range Settings {
		if msg := st.check(c, &def); msg != "" {
			v.add(Error, st.Key, "%s", msg)
		}
	}
	if c.StorageWarn >= c.StorageCrit {
		v.add(Error, "storage_warn", "must be below storage_crit (%d); using %d and %d", c.StorageCrit, def.StorageWarn, def.StorageCrit)
		c.StorageWarn, c.StorageCrit = def.StorageWarn, def.StorageCrit
	}
	if textsafe.Field(c.ClusterName) != c.ClusterName || len(c.ClusterName) > 64 {
		v.add(Error, "cluster_name", "must be up to 64 printable characters; using the detected name")
		c.ClusterName = ""
	}
	v.gpuNames(c.GPUNames)
	c.Storage = v.storage(c.Storage)
	v.profiles(c.Profiles)
}

// storage validates [[storage]] entries. An invalid entry is dropped as a
// whole, since there is no sensible default location.
func (v *validator) storage(entries []StorageEntry) []StorageEntry {
	kept := []StorageEntry{}
	for i, e := range entries {
		key := fmt.Sprintf("storage[%d]", i+1)
		if e.Backend == "" {
			e.Backend = "auto"
		}
		if e.Format == "" {
			e.Format = "raw"
		}
		if !slices.Contains(Backends, e.Backend) {
			v.add(Error, key+".backend", "%q is not one of %s; this entry is skipped", e.Backend, strings.Join(Backends, ", "))
			continue
		}
		if !slices.Contains(Formats, e.Format) {
			v.add(Error, key+".format", "%q is not one of %s; this entry is skipped", e.Format, strings.Join(Formats, ", "))
			continue
		}

		cmd, err := normaliseCommand(e.RawCommand)
		if err != nil {
			v.add(Error, key+".command", "%v; this entry is skipped", err)
			continue
		}
		e.Command = cmd
		switch {
		case e.Backend == "command" && cmd.IsZero():
			v.add(Error, key+".command", "backend \"command\" needs a command; this entry is skipped")
			continue
		case e.Backend != "command" && !cmd.IsZero():
			v.add(Warning, key+".command", "is only used with backend = \"command\"; it is ignored")
			e.Command, e.RawCommand = CommandSpec{}, nil
		}

		if e.Path == "" && e.Backend != "command" {
			v.add(Error, key+".path", "is required; this entry is skipped")
			continue
		}
		if e.Path != "" {
			p, missing := ExpandPath(e.Path, v.getenv)
			if len(missing) > 0 {
				v.add(Warning, key+".path", "%q uses unset variable(s) %s; this entry is skipped", e.Path, "$"+strings.Join(missing, ", $"))
				continue
			}
			if !filepath.IsAbs(p) {
				v.add(Error, key+".path", "%q is not an absolute path; this entry is skipped", e.Path)
				continue
			}
			e.Path = p
		}
		if e.Label == "" {
			e.Label = labelFor(e)
		}
		kept = append(kept, e)
	}
	return kept
}

// normaliseCommand accepts an array of strings (argv) or a string (shell).
func normaliseCommand(raw any) (CommandSpec, error) {
	switch c := raw.(type) {
	case nil:
		return CommandSpec{}, nil
	case string:
		if strings.TrimSpace(c) == "" {
			return CommandSpec{}, fmt.Errorf("is an empty string")
		}
		return CommandSpec{Shell: c}, nil
	case []any:
		argv := make([]string, 0, len(c))
		for _, a := range c {
			s, ok := a.(string)
			if !ok || s == "" {
				return CommandSpec{}, fmt.Errorf("must be an array of non-empty strings")
			}
			argv = append(argv, s)
		}
		if len(argv) == 0 {
			return CommandSpec{}, fmt.Errorf("is an empty array")
		}
		return CommandSpec{Argv: argv}, nil
	default:
		return CommandSpec{}, fmt.Errorf("must be an array of strings (preferred) or a string")
	}
}

func labelFor(e StorageEntry) string {
	if e.Path != "" {
		return filepath.Base(e.Path)
	}
	if len(e.Command.Argv) > 0 {
		return filepath.Base(e.Command.Argv[0])
	}
	return "Storage"
}

// suggestKey returns the known key closest to an unknown one, or "".
func suggestKey(path []string) string {
	if len(path) == 0 {
		return ""
	}
	t := reflect.TypeOf(Config{})
	for _, part := range path[:len(path)-1] {
		f, ok := fieldByTag(t, part)
		if !ok {
			return ""
		}
		t = f.Type
		if t.Kind() == reflect.Slice {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return ""
		}
	}
	unknown := path[len(path)-1]
	best, bestDist := "", 3
	for i := range t.NumField() {
		tag := strings.Split(t.Field(i).Tag.Get("toml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if d := editDistance(unknown, tag); d < bestDist {
			best, bestDist = tag, d
		}
	}
	return best
}

func fieldByTag(t reflect.Type, tag string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		if strings.Split(t.Field(i).Tag.Get("toml"), ",")[0] == tag {
			return t.Field(i), true
		}
	}
	return reflect.StructField{}, false
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)

// profiles checks [profiles.NAME]: a plain name and an absolute slurm_conf
// that exists is not required here (the profile may be for later).
func (v *validator) profiles(ps map[string]Profile) {
	for name, p := range ps {
		key := "profiles." + name
		if !profileName.MatchString(name) {
			v.add(Error, key, "profile names use letters, digits, '-' and '_'; it is ignored")
			delete(ps, name)
			continue
		}
		if p.SlurmConf == "" {
			v.add(Error, key+".slurm_conf", "is required; the profile is ignored")
			delete(ps, name)
			continue
		}
		conf, missing := ExpandPath(p.SlurmConf, v.getenv)
		if len(missing) > 0 || !filepath.IsAbs(conf) {
			v.add(Error, key+".slurm_conf", "%q must be an absolute path; the profile is ignored", p.SlurmConf)
			delete(ps, name)
			continue
		}
		p.SlurmConf = conf
		ps[name] = p
	}
}

// gpuNames drops empty or overlong display names.
func (v *validator) gpuNames(m map[string]string) {
	for k, name := range m {
		if name == "" || len(name) > 24 || textsafe.Field(name) != name {
			v.add(Error, "gpu_names."+k, "must be 1 to 24 printable characters; the name is ignored")
			delete(m, k)
		}
	}
}
