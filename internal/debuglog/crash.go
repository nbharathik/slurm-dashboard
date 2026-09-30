package debuglog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

// WriteCrash records a recovered panic in cacheDir/crash-<timestamp>.log
// and returns the file's path. The terminal must already be restored before
// the path is shown to the user.
func WriteCrash(cacheDir string, recovered any, stack []byte) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
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
	return path, f.Close()
}
