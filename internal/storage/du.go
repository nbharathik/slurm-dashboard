package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Disk-usage analyser limits.
const (
	DuTimeout  = 10 * time.Minute
	DuCacheTTL = time.Hour
)

// DuArgv is the low-priority du command for a directory (no ionice on macOS).
func DuArgv(path string, withIonice bool) ([]string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("%q is not a clean absolute path", path)
	}
	argv := []string{"nice", "-n", "19"}
	if withIonice {
		argv = append(argv, "ionice", "-c3")
	}
	return append(argv, "du", "-x", "-d1", "-k", "--", path), nil
}

// NeedsConfirm reports filesystems where a scan loads a shared metadata
// server, so the user confirms first.
func NeedsConfirm(fstype string) bool {
	return fstype == "lustre" || fstype == "gpfs" || fstype == "beegfs"
}

// ParseDu reads "du -k -d1" output: "<KB>\t<path>" lines, the root last.
// Entries come back largest first; errors on stderr make it partial.
func ParseDu(out []byte, root string) (model.DiskUsage, error) {
	u := model.DiskUsage{Path: root}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		kb, p, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(kb), 10, 64)
		if err != nil {
			continue
		}
		if filepath.Clean(p) == filepath.Clean(root) {
			u.Total = n * 1024
			continue
		}
		u.Entries = append(u.Entries, model.DiskEntry{Path: p, Bytes: n * 1024})
	}
	if u.Total == 0 && len(u.Entries) == 0 {
		return u, errors.New("du printed nothing")
	}
	slices.SortStableFunc(u.Entries, func(a, b model.DiskEntry) int {
		switch {
		case a.Bytes > b.Bytes:
			return -1
		case a.Bytes < b.Bytes:
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return u, nil
}
