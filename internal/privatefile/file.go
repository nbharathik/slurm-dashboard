// Package privatefile handles disposable application state in private directories.
package privatefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nbharathik/slurm-dashboard/internal/atomicfile"
)

// ErrUnsafe identifies paths that cannot hold private application state.
var ErrUnsafe = errors.New("unsafe application persistence path")

// Directory creates dir and requires it to be the user's own and writable
// only by them; its parents may be shared.
func Directory(dir string) error {
	if dir == "" || dir == "." {
		return ErrUnsafe
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if abs == filepath.Dir(abs) {
		return ErrUnsafe
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return err
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafe, abs)
	}
	if !safeDirectory(st) && ownDirectory(st) {
		if err := os.Chmod(abs, 0o700); err == nil { //nolint:gosec // a directory needs x to be entered
			st, err = os.Lstat(abs)
			if err != nil {
				return err
			}
		}
	}
	if !safeDirectory(st) {
		return fmt.Errorf("%w: %s must belong to you and be writable only by you", ErrUnsafe, abs)
	}
	return nil
}

func checkFile(f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || !safeFile(st) {
		return ErrUnsafe
	}
	return nil
}

func open(path string, flags int) (*os.File, error) {
	if err := Directory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := openNoFollow(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	if err = checkFile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// Read enforces a byte budget and refuses unsafe files.
func Read(path string, limit int64) ([]byte, error) {
	f, err := open(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("state exceeds %d bytes", limit)
	}
	return b, err
}

// Append opens a regular private file without following a symlink.
func Append(path string) (*os.File, error) { return open(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND) }

// Write replaces a private file atomically.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := Directory(dir); err != nil {
		return err
	}
	if st, err := os.Lstat(path); err == nil {
		if !st.Mode().IsRegular() || !safeFile(st) {
			return ErrUnsafe
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicfile.Write(path, data, 0o600, false)
}
