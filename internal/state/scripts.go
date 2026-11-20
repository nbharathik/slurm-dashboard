package state

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/privatefile"
)

// Kept batch scripts: Slurm forgets a job's script soon after it ends on
// sites that do not store scripts, which is when a rerun needs it.
const (
	ScriptKeep    = 30 * 24 * time.Hour
	maxScriptSize = 1 << 20
	maxScripts    = 500
)

// scriptPath is where a job's script copy lives; "" when there is no cache
// or the ID has unexpected characters.
func (s *Sources) scriptPath(id string) string {
	if s.ScriptDir == "" || id == "" || strings.Trim(id, "0123456789_") != "" {
		return ""
	}
	return filepath.Join(s.ScriptDir, id+".sh")
}

// keepScript saves a copy (0600) and drops copies older than ScriptKeep.
// Errors are ignored: the copy is a convenience.
func (s *Sources) keepScript(id, script string) {
	p := s.scriptPath(id)
	if p == "" || len(script) > maxScriptSize {
		return
	}
	if privatefile.Write(p, []byte(script)) != nil {
		return
	}
	entries, err := os.ReadDir(s.ScriptDir)
	if err != nil {
		return
	}
	type file struct {
		name string
		mod  time.Time
	}
	var files []file
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		files = append(files, file{e.Name(), info.ModTime()})
	}
	// Newest first, so the cap drops the oldest copies.
	saved := filepath.Base(p)
	slices.SortFunc(files, func(a, b file) int {
		switch {
		case a.name == saved:
			return -1
		case b.name == saved:
			return 1
		}
		return b.mod.Compare(a.mod)
	})
	now := time.Now()
	for i, f := range files {
		if i >= maxScripts || now.Sub(f.mod) > ScriptKeep {
			_ = os.Remove(filepath.Join(s.ScriptDir, f.name))
		}
	}
}

// keptScript is the saved copy of a job's script, if any.
func (s *Sources) keptScript(id string) (string, bool) {
	p := s.scriptPath(id)
	if p == "" {
		return "", false
	}
	b, err := privatefile.Read(p, maxScriptSize)
	if err != nil || len(b) == 0 {
		return "", false
	}
	return string(b), true
}
