package debuglog

import (
	"fmt"
	"os"
	"sync"

	"github.com/nbharathik/slurm-dashboard/internal/privatefile"
)

// rotatingFile is an io.WriteCloser that rotates path when it would grow
// past maxSize, keeping keep files in total (path, path.1, ... ).
type rotatingFile struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	keep    int
	f       *os.File
	size    int64
}

func openRotating(path string, maxSize int64, keep int) (*rotatingFile, error) {
	r := &rotatingFile{path: path, maxSize: maxSize, keep: max(keep, 1)}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := privatefile.Append(r.path)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.f, r.size = f, fi.Size()
	return nil
}

// Write appends p, rotating first if p would push the file past maxSize.
func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate shifts path.(k-2) -> path.(k-1), ..., path -> path.1 and reopens.
func (r *rotatingFile) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	r.f = nil
	for i := r.keep - 1; i >= 1; i-- {
		src := r.path
		if i > 1 {
			src = fmt.Sprintf("%s.%d", r.path, i-1)
		}
		dst := fmt.Sprintf("%s.%d", r.path, i)
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if r.keep == 1 {
		if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return r.open()
}

// Close closes the current file.
func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
