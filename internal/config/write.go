package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

//go:embed default.toml
var defaultFile []byte

// ErrExists means the config file already exists and force was not set.
var ErrExists = errors.New("config file already exists")

// WriteDefault writes the commented default config to path. It refuses to
// replace an existing file unless force is set; with force, the old file is
// kept as path+".bak". The directory is created with mode 0700 and the file
// with 0600, and the write is atomic (temporary file, then rename). It
// returns the backup path, if one was made.
func WriteDefault(path string, force bool) (backup string, err error) {
	if _, err := os.Lstat(path); err == nil {
		if !force {
			return "", fmt.Errorf("%s: %w (use --force to replace it; the old file is kept as .bak)", path, ErrExists)
		}
		backup = path + ".bak"
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := writeAtomic(dir, path, defaultFile); err != nil {
		return "", err
	}
	return backup, nil
}

// writeAtomic writes data to a temporary file in dir and renames it over
// path, keeping any existing file as path+".bak".
func writeAtomic(dir, path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, statErr := os.Lstat(path); statErr == nil {
		if err := os.Rename(path, path+".bak"); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), path)
}

// ErrNotSettable means a key is not one sdash writes itself.
var ErrNotSettable = errors.New("not a setting sdash can change")

// newFileHeader starts a config file that Save creates.
const newFileHeader = "# sdash settings. Changed with the Settings screen (,) or by hand;\n# every key is described in 'sdash config' and docs/config.md.\n"

// Save writes one setting's value from c into the file at path and keeps
// everything else, comments included. It replaces the key's line (or its
// commented-out form) above the first table, or adds the key there. A
// missing file is created with a short header and just that setting, so
// later versions' defaults still apply to the rest. Only settings the
// Settings screen may change are written; commands never are.
func Save(path string, c Config, key string) error {
	s, ok := Lookup(key)
	if !ok || !s.Screen {
		return fmt.Errorf("%s: %w", key, ErrNotSettable)
	}
	val, err := s.tomlValue(&c)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		// Check the original path before resolving links or changing permissions.
		if issues := TrustIssues(path); len(issues) > 0 {
			return fmt.Errorf("refusing to save untrusted config %s: %s", path, issues[0].Msg)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target // keep a symlinked dotfile a symlink
	}
	data, err := os.ReadFile(path) //nolint:gosec // the user's own config file
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = []byte(newFileHeader)
	case err != nil:
		return err
	}
	out := setLine(string(data), key, val)
	var check map[string]any
	if err := toml.Unmarshal([]byte(out), &check); err != nil {
		return fmt.Errorf("%s: could not set %s without breaking the file (%v); edit it with 'sdash config edit'", path, key, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeAtomic(dir, path, []byte(out))
}

var headerLine = regexp.MustCompile(`^\s*\[`)

// setLine sets key = val in the top-level part of a TOML file (above the
// first table).
func setLine(data, key, val string) string {
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	lines := strings.SplitAfter(data, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	end := len(lines)
	for i, l := range lines {
		if headerLine.MatchString(l) {
			end = i
			break
		}
	}
	active := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(key) + `\s*=\s*`)
	commented := regexp.MustCompile(`^\s*#\s*(` + regexp.QuoteMeta(key) + `\s*=\s*)`)
	replace := func(i int, prefix, rest string, commentCol int) string {
		last := valueEnd(lines, i, rest)
		code, comment := splitComment(strings.TrimRight(rest, "\n"))
		if last > i {
			comment = "" // a multi-line value becomes one line
		}
		line := prefix + val
		if comment != "" && code != "" {
			line += strings.Repeat(" ", max(commentCol-len(line), 1)) + comment
		}
		return strings.Join(append(append(slices.Clone(lines[:i]), line+"\n"), lines[last+1:]...), "")
	}
	for i := range end {
		if m := active.FindStringIndex(lines[i]); m != nil {
			rest := lines[i][m[1]:]
			code, _ := splitComment(strings.TrimRight(rest, "\n"))
			return replace(i, lines[i][:m[1]], rest, m[1]+len(code))
		}
	}
	for i := range end {
		if m := commented.FindStringSubmatchIndex(lines[i]); m != nil {
			rest := lines[i][m[1]:]
			code, _ := splitComment(strings.TrimRight(rest, "\n"))
			return replace(i, lines[i][m[2]:m[3]], rest, m[1]-m[2]+len(code))
		}
	}
	add := []string{key + " = " + val + "\n"}
	if end < len(lines) {
		add = append(add, "\n")
	}
	return strings.Join(append(append(slices.Clone(lines[:end]), add...), lines[end:]...), "")
}

// splitComment splits a TOML line into its code and a trailing "# ..."
// comment, ignoring '#' inside strings. The code keeps its trailing
// spaces, so the comment's column is len(code).
func splitComment(s string) (code, comment string) {
	var quote byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quote == '"' && c == '\\':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == '#':
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// valueEnd returns the last line of a value that starts on line i with
// rest: an array can continue over several lines.
func valueEnd(lines []string, i int, rest string) int {
	depth := 0
	for j, text := i, rest; j < len(lines); j++ {
		if j > i {
			text = lines[j]
		}
		code, _ := splitComment(text)
		var quote byte
		for k := 0; k < len(code); k++ {
			switch c := code[k]; {
			case quote == '"' && c == '\\':
				k++
			case quote != 0 && c == quote:
				quote = 0
			case quote == 0 && (c == '"' || c == '\''):
				quote = c
			case quote == 0 && c == '[':
				depth++
			case quote == 0 && c == ']':
				depth--
			}
		}
		if depth <= 0 {
			return j
		}
	}
	return i
}
