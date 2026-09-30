// Package storage finds the user's storage locations, detects their
// filesystems and reads quotas with the right backend.
package storage

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/config"
)

// Mount is one line of /proc/self/mounts.
type Mount struct {
	Device string
	Point  string
	FSType string
}

// ParseMounts parses /proc/self/mounts, decoding octal escapes such as
// \040 for spaces.
func ParseMounts(data []byte) []Mount {
	var out []Mount
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		out = append(out, Mount{Device: unescape(f[0]), Point: unescape(f[1]), FSType: f[2]})
	}
	return out
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// FindMount returns the mount containing path (longest prefix; the last
// entry wins for stacked mounts).
func FindMount(mounts []Mount, path string) (Mount, bool) {
	var best Mount
	found := false
	for _, m := range mounts {
		if !within(path, m.Point) {
			continue
		}
		if !found || len(m.Point) >= len(best.Point) {
			best, found = m, true
		}
	}
	return best, found
}

func within(path, dir string) bool {
	if dir == "/" {
		return strings.HasPrefix(path, "/")
	}
	return path == dir || strings.HasPrefix(path, dir+"/")
}

// Location is a place whose usage sdash reports.
type Location struct {
	Label   string
	Path    string // as configured or detected
	Real    string // symlinks resolved
	Mount   Mount
	Backend string // lustre | gpfs | beegfs | quota | statfs | command
	Note    string
	Format  string // raw | json (command backend)
	Command config.CommandSpec
}

// Env is what location detection reads from the system.
type Env struct {
	Getenv       func(string) string
	User         string
	Mounts       []Mount
	EvalSymlinks func(string) (string, error)
	IsDir        func(string) bool
}

// BackendFor picks the quota backend for a filesystem type.
func BackendFor(fstype string) string {
	switch fstype {
	case "lustre":
		return "lustre"
	case "gpfs":
		return "gpfs"
	case "beegfs":
		return "beegfs"
	case "nfs", "nfs4", "xfs", "ext4", "ext3":
		return "quota"
	}
	return "statfs"
}

// Locate returns the configured locations, or detects them: $HOME, then
// $SCRATCH, $WORK, $PROJECT and $DATA when set, then /scratch/$USER and
// /work/$USER, keeping one location per mount point.
func Locate(entries []config.StorageEntry, env Env) []Location {
	resolve := func(l *Location) {
		l.Real = l.Path
		if env.EvalSymlinks != nil {
			if r, err := env.EvalSymlinks(l.Path); err == nil {
				l.Real = r
			}
		}
		l.Mount, _ = FindMount(env.Mounts, l.Real)
		if l.Backend == "" || l.Backend == "auto" {
			l.Backend = BackendFor(l.Mount.FSType)
		}
	}
	var out []Location
	if len(entries) > 0 {
		for _, e := range entries {
			l := Location{Label: e.Label, Path: e.Path, Backend: e.Backend, Note: e.Note, Format: e.Format, Command: e.Command}
			if l.Label == "" {
				l.Label = filepath.Base(e.Path)
			}
			resolve(&l)
			out = append(out, l)
		}
		return out
	}
	type cand struct{ label, path string }
	var cands []cand
	if h := env.Getenv("HOME"); h != "" {
		cands = append(cands, cand{"Home", h})
	}
	for _, v := range []string{"SCRATCH", "WORK", "PROJECT", "DATA"} {
		if p := env.Getenv(v); p != "" {
			cands = append(cands, cand{strings.ToUpper(v[:1]) + strings.ToLower(v[1:]), p})
		}
	}
	if env.User != "" {
		cands = append(cands, cand{"Scratch", "/scratch/" + env.User}, cand{"Work", "/work/" + env.User})
	}
	seen := map[string]bool{}
	labels := map[string]bool{}
	for _, c := range cands {
		if !filepath.IsAbs(c.path) || (env.IsDir != nil && !env.IsDir(c.path)) {
			continue
		}
		l := Location{Label: c.label, Path: c.path}
		resolve(&l)
		key := l.Mount.Point
		if key == "" {
			key = l.Real
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		for labels[l.Label] {
			l.Label += "'"
		}
		labels[l.Label] = true
		out = append(out, l)
	}
	return out
}
