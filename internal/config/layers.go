package config

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// SiteFile is the site-wide config an administrator can install. The
// user's own file is read on top of it.
const SiteFile = "/etc/sdash/config.toml"

// LoadLayers reads config files in order, lowest first (the site file,
// then the user's). A file others can change loses its commands (see
// TrustIssues). Each file is checked on its own, so issues name their
// file; then the keys each file sets are merged: tables merge key by key,
// while values and arrays (and so [[storage]] as a whole) are replaced by
// the later file. Missing files are skipped. The error is for a file that
// exists but cannot be read; the Config is usable even then.
func LoadLayers(paths []string, getenv func(string) string) (Config, []Issue, error) {
	merged := map[string]any{}
	var issues []Issue
	var firstErr error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_, fileIssues := Parse(data, getenv)
		for i := range fileIssues {
			fileIssues[i].File = path
		}
		issues = append(issues, fileIssues...)
		var m map[string]any
		if toml.Unmarshal(data, &m) != nil {
			continue // a syntax error: the file is ignored (reported above)
		}
		for _, is := range fileIssues {
			if is.Level == Error && is.Key != "" {
				deleteKey(m, strings.Split(is.Key, ".")) // an invalid value keeps the lower layer's
			}
		}
		if trust := TrustIssues(path); len(trust) > 0 {
			issues = append(issues, trust...)
			dropCommands(m)
		}
		mergeTables(merged, m)
	}
	if len(merged) == 0 {
		return Default(), issues, firstErr
	}
	data, err := toml.Marshal(merged)
	if err != nil {
		return Default(), issues, err
	}
	cfg, _ := Parse(data, getenv) // the issues were reported per file
	return cfg, issues, firstErr
}

// deleteKey removes a dotted key path from nested tables.
func deleteKey(m map[string]any, path []string) {
	for len(path) > 1 {
		next, ok := m[path[0]].(map[string]any)
		if !ok {
			return
		}
		m, path = next, path[1:]
	}
	delete(m, path[0])
}

// mergeTables copies src into dst: tables merge recursively, anything else
// replaces.
func mergeTables(dst, src map[string]any) {
	for k, v := range src {
		sub, isTable := v.(map[string]any)
		have, hasTable := dst[k].(map[string]any)
		if isTable && hasTable {
			mergeTables(have, sub)
			continue
		}
		dst[k] = v
	}
}

// SetIn reports, for each top-level key, the last of paths that sets it.
// Files that are missing or not valid TOML are skipped.
func SetIn(paths []string) map[string]string {
	out := map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // config files the user chose
		if err != nil {
			continue
		}
		var m map[string]any
		if toml.Unmarshal(data, &m) != nil {
			continue
		}
		for k := range m {
			out[k] = p
		}
	}
	return out
}
