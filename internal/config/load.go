// Package config loads, validates and writes the TOML config; bad values warn and fall back to defaults.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Level is the severity of an Issue.
type Level int

const (
	// Warning means sdash understood the file but something looks wrong,
	// such as an unknown key or a value that was adjusted.
	Warning Level = iota
	// Error means a value was invalid and its default is used instead.
	Error
)

func (l Level) String() string {
	if l == Error {
		return "error"
	}
	return "warning"
}

// Issue is one problem found while loading the config file.
type Issue struct {
	File  string // set when several files are read
	Level Level
	Line  int // 1-based; 0 when unknown
	Col   int // 1-based; 0 when unknown
	Key   string
	Msg   string
}

// String formats the issue as "FILE:LINE:COL: level: key: message" (the
// file only when set).
func (i Issue) String() string {
	var b strings.Builder
	if i.File != "" {
		b.WriteString(i.File + ":")
		if i.Line == 0 {
			b.WriteByte(' ')
		}
	}
	if i.Line > 0 {
		fmt.Fprintf(&b, "%d:", i.Line)
		if i.Col > 0 {
			fmt.Fprintf(&b, "%d:", i.Col)
		}
		b.WriteByte(' ')
	}
	b.WriteString(i.Level.String())
	b.WriteString(": ")
	if i.Key != "" {
		b.WriteString(i.Key)
		b.WriteString(": ")
	}
	b.WriteString(i.Msg)
	return b.String()
}

// HasErrors reports whether any issue is an Error.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Level == Error {
			return true
		}
	}
	return false
}

// maxTypeErrors bounds how many type errors Load isolates before it gives
// up on the file.
const maxTypeErrors = 50

// Load reads the config file at path. A missing file returns the defaults
// and no issues. The returned error is only for a file that exists but
// cannot be read; the Config is usable even then.
func Load(path string, getenv func(string) string) (Config, []Issue, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil, nil
	}
	if err != nil {
		return Default(), nil, err
	}
	cfg, issues := Parse(data, getenv)
	return cfg, issues, nil
}

// Parse decodes and validates config file contents.
func Parse(data []byte, getenv func(string) string) (Config, []Issue) {
	var issues []Issue

	// A syntax error makes the whole file untrustworthy.
	var probe map[string]any
	if err := toml.Unmarshal(data, &probe); err != nil {
		issues = append(issues, decodeIssue(err, "the file is not valid TOML; using all defaults"))
		return Default(), issues
	}

	l := indexLayout(data)
	work := bytes.Clone(data)
	issues = append(issues, oldTables(l, work)...)

	// Type errors (a string where a number belongs, ...) stop the decoder.
	// Report each one, blank out just that expression, and decode again.
	cfg := Default()
	for attempt := 0; ; attempt++ {
		cfg = Default()
		err := toml.NewDecoder(bytes.NewReader(work)).Decode(&cfg)
		if err == nil {
			break
		}
		var de *toml.DecodeError
		row := 0
		if errors.As(err, &de) {
			row, _ = de.Position()
		}
		i, ok := l.exprAt(row)
		if !ok || l.exprs[i].Header || attempt >= maxTypeErrors {
			issues = append(issues, decodeIssue(err, "cannot recover from this; using all defaults"))
			return Default(), issues
		}
		issues = append(issues, decodeIssue(err, "using the default"))
		l.blankExpr(work, i)
	}

	issues = append(issues, unknownKeys(work)...)
	v := validator{layout: l, getenv: getenv}
	v.validate(&cfg)
	issues = append(issues, v.issues...)
	sortIssues(issues)
	return cfg, issues
}

// sortIssues orders issues by position; issues without a line go last.
func sortIssues(issues []Issue) {
	slices.SortStableFunc(issues, func(a, b Issue) int {
		switch {
		case a.Line == b.Line:
			return cmp.Compare(a.Col, b.Col)
		case a.Line == 0:
			return 1
		case b.Line == 0:
			return -1
		}
		return cmp.Compare(a.Line, b.Line)
	})
}

// decodeIssue converts a go-toml error into an Issue.
func decodeIssue(err error, consequence string) Issue {
	is := Issue{Level: Error, Msg: strings.TrimPrefix(err.Error(), "toml: ") + "; " + consequence}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		is.Line, is.Col = de.Position()
		is.Key = strings.Join(de.Key(), ".")
	}
	return is
}

// unknownKeys reports keys that sdash does not know as warnings.
func unknownKeys(data []byte) []Issue {
	var sink Config
	err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&sink)
	var sme *toml.StrictMissingError
	if !errors.As(err, &sme) {
		return nil
	}
	issues := make([]Issue, 0, len(sme.Errors))
	for _, e := range sme.Errors {
		line, col := e.Position()
		key := []string(e.Key())
		msg := "unknown key; it is ignored"
		if now, ok := movedKey(strings.Join(key, ".")); ok {
			msg = now
		} else if s := suggestKey(key); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		issues = append(issues, Issue{Level: Warning, Line: line, Col: col, Key: strings.Join(key, "."), Msg: msg})
	}
	return issues
}

// moved maps keys and tables of the older config to what replaced
// them; "" means the setting was removed.
var moved = map[string]string{
	"default_tab": "start_tab", "refresh": "refresh",
	"ui": "theme, mouse and ascii", "ui.theme": "theme", "ui.mouse": "mouse", "ui.ascii": "ascii", "ui.show_other_users": "",
	"notify": "notify and notify_command", "notify.on_job_end": "notify_command", "notify.methods": "notify", "notify.on": "notify",
	"actions": "shell", "actions.shell_method": "shell", "actions.confirm_destructive": "",
	"nodes": "hide_partitions", "nodes.hide_partitions": "hide_partitions",
	"cluster": "hide_partitions", "cluster.partitions": "hide_partitions", "cluster.enabled": "",
	"alerts": "storage_warn and storage_crit", "alerts.storage_warn": "storage_warn", "alerts.storage_crit": "storage_crit",
	"gpu": "gpu_names", "gpu.aliases": "gpu_names", "jobs": "", "queue": "", "history": "", "logs": "", "keys": "",
}

// movedKey explains an old key, matching it or one of its parents.
func movedKey(key string) (string, bool) {
	for k := key; k != ""; {
		if now, ok := moved[k]; ok {
			if now == "" {
				return "this setting was removed; it is ignored", true
			}
			return "replaced by " + now + " (see 'sdash config'); it is ignored", true
		}
		i := strings.LastIndexByte(k, '.')
		if i < 0 {
			break
		}
		k = k[:i]
	}
	return "", false
}

// oldTables blanks the old [refresh] and [notify] tables in work and warns per key.
func oldTables(l layout, work []byte) []Issue {
	var issues []Issue
	for i, e := range l.exprs {
		top, _, dotted := strings.Cut(e.Key, ".")
		if (top != "refresh" && top != "notify") || (!e.Header && !dotted) {
			continue
		}
		l.blankExpr(work, i)
		if e.Header {
			continue
		}
		msg, ok := movedKey(e.Key)
		if !ok {
			msg = "this setting was removed; it is ignored"
		}
		p := l.keys[e.Key]
		issues = append(issues, Issue{Level: Warning, Line: p.Line, Col: p.Col, Key: e.Key, Msg: msg})
	}
	return issues
}
