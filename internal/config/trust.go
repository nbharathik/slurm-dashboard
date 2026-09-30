package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// getuid is the user whose files are trusted; tests replace it.
var getuid = os.Getuid

// TrustIssues reports why a config file (or its directories) is writable by others.
// LoadLayers then drops its commands, since editors could run programs as this user.
func TrustIssues(path string) []Issue {
	issue := func(why string) []Issue {
		return []Issue{{File: path, Level: Error, Msg: why + "; its storage commands and notify_command are ignored (check ownership and write permissions on the file and its parent directories)"}}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return issue("cannot check config permissions: " + err.Error())
	}
	if !fi.Mode().IsRegular() {
		return issue("the config is not a regular file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return issue("cannot resolve config path: " + err.Error())
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return issue("cannot resolve config links: " + err.Error())
	}
	// Check both the link location and its target, including their ancestors.
	for _, p := range []string{abs, resolved} {
		for parent := filepath.Dir(p); ; parent = filepath.Dir(parent) {
			dir, err := os.Stat(parent)
			if err != nil {
				return issue("cannot check config directory: " + err.Error())
			}
			if why := untrusted(fi, dir, getuid()); why != "" {
				return issue(why)
			}
			if parent == filepath.Dir(parent) {
				break
			}
		}
	}
	return nil
}

// untrusted explains why a file with these modes and owners is not safe to
// take commands from, or returns "".
func untrusted(file, dir fs.FileInfo, uid int) string {
	owner := func(fi fs.FileInfo) int {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			return int(st.Uid)
		}
		return uid
	}
	switch m := file.Mode().Perm(); {
	case m&0o002 != 0:
		return fmt.Sprintf("the file is writable by everyone (mode %04o)", m)
	case m&0o020 != 0:
		return fmt.Sprintf("the file is writable by its group (mode %04o)", m)
	case owner(file) != uid && owner(file) != 0:
		return fmt.Sprintf("the file belongs to another user (uid %d)", owner(file))
	case owner(dir) != uid && owner(dir) != 0:
		return fmt.Sprintf("a parent directory belongs to another user (uid %d)", owner(dir))
	case dir.Mode().Perm()&0o002 != 0 && dir.Mode()&fs.ModeSticky == 0:
		return "a parent directory is writable by everyone"
	case dir.Mode().Perm()&0o020 != 0 && dir.Mode()&fs.ModeSticky == 0:
		return "a parent directory is writable by its group"
	}
	return ""
}

// dropCommands removes what runs programs from one decoded layer.
func dropCommands(m map[string]any) {
	delete(m, "notify_command")
	entries, ok := m["storage"].([]any)
	if !ok {
		return
	}
	kept := []any{}
	for _, e := range entries {
		if t, ok := e.(map[string]any); ok && (t["command"] != nil || t["backend"] == "command") {
			continue
		}
		kept = append(kept, e)
	}
	m["storage"] = kept
}
