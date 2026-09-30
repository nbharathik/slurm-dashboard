package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ExpandPath expands a leading "~" or "~/" to $HOME and every $VAR or
// ${VAR} from getenv. It returns the names of variables that were unset or
// empty, so callers can report them instead of silently using "".
// "~user" is left unchanged.
func ExpandPath(s string, getenv func(string) string) (string, []string) {
	var missing []string
	lookup := func(name string) string {
		v := getenv(name)
		if v == "" && !slices.Contains(missing, name) {
			missing = append(missing, name)
		}
		return v
	}
	switch {
	case s == "~":
		s = "$HOME"
	case strings.HasPrefix(s, "~/"):
		s = "$HOME" + s[1:]
	}
	out := os.Expand(s, lookup)
	if out != "" {
		out = filepath.Clean(out)
	}
	return out, missing
}
