// Package privatefile handles disposable application state in private directories.
package privatefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrUnsafe identifies paths that cannot hold private application state.
var ErrUnsafe = errors.New("unsafe application persistence path")

// Directory refuses symlinks and writable ancestors before creating private state.
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
	for p := abs; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e == nil {
			if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || !safeDirectory(st, p == abs) {
				return fmt.Errorf("%w: %s", ErrUnsafe, p)
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return err
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if !safeDirectory(st, true) || st.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafe
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
	f, err := os.CreateTemp(dir, ".sdash-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, werr := f.Write(data)
	if err = errors.Join(werr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
