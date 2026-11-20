// Package atomicfile replaces a file in one rename, so readers see the old
// or the new content, never a part.
package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
)

// Write puts data at path with mode: it writes a temporary file in the same
// directory and renames it over path. With durable, the data and the rename
// are synced to disk where the filesystem allows.
func Write(path string, data []byte, mode os.FileMode, durable bool) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".sdash-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // a no-op after the rename
	_, werr := f.Write(data)
	merr := f.Chmod(mode)
	var serr error
	if durable {
		serr = f.Sync()
	}
	if err := errors.Join(werr, merr, serr, f.Close()); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if durable {
		SyncDir(dir)
	}
	return nil
}

// SyncDir makes a rename in dir durable; errors are ignored, as some network
// filesystems refuse to sync directories.
func SyncDir(dir string) {
	if d, err := os.Open(dir); err == nil { //nolint:gosec // a directory sdash writes to
		_ = d.Sync()
		_ = d.Close()
	}
}
