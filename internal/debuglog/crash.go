package debuglog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/privatefile"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

// WriteCrash records a recovered panic in cacheDir/crash-<timestamp>.log and returns its path.
func WriteCrash(cacheDir string, recovered any, stack []byte) (string, error) {
	if err := privatefile.Directory(cacheDir); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	name := fmt.Sprintf("crash-%s.log", now.Format("20060102T150405.000Z"))
	path := filepath.Join(cacheDir, name)
	info := meta.Info()
	body := fmt.Sprintf("%s %s (commit %s, built %s, %s, %s)\ntime: %s\npanic: %v\n\n%s",
		info.Name, info.Version, info.Commit, info.Date, info.GoVersion, runtime.GOOS+"/"+runtime.GOARCH,
		now.Format(time.RFC3339Nano), recovered, stack)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	pruneCrashes(cacheDir, keepCrashes)
	return path, nil
}

// keepCrashes is how many crash reports are kept; older ones are removed.
const keepCrashes = 10

func pruneCrashes(dir string, keep int) {
	old, err := filepath.Glob(filepath.Join(dir, "crash-*.log"))
	if err != nil || len(old) <= keep {
		return
	}
	sort.Strings(old) // the timestamp in the name sorts oldest first
	for _, p := range old[:len(old)-keep] {
		_ = os.Remove(p)
	}
}
