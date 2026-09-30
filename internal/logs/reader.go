// Package logs tails job log files and prepares their text for display:
// an incremental reader (stat polling, never inotify), a bounded line
// buffer that collapses progress bars, highlighting and search.
package logs

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
)

// FS opens log files. OS reads the real filesystem; demo mode supplies a
// simulated one.
type FS interface {
	Open(name string) (File, error)
}

// File is an open log file.
type File interface {
	io.ReaderAt
	io.Closer
	Stat() (fs.FileInfo, error)
}

type osFS struct{}

func (osFS) Open(name string) (File, error) { return os.Open(name) }

// OS is the real filesystem.
var OS FS = osFS{}

// State describes the file being followed.
type State int

// File states.
const (
	Reading    State = iota // the file exists and is being read
	Missing                 // not created yet (the job may not have started)
	Unreadable              // permission denied or not visible from this node
	Failed                  // another error
)

// MaxChunk bounds the bytes read in one poll, so a burst of output cannot
// stall the UI; the rest is read on the next poll.
const MaxChunk = 4 << 20

// Chunk is the result of one poll.
type Chunk struct {
	Data  []byte
	Reset bool // discard what was read before (first read or truncation)
	State State
	Err   error
	Size  int64 // file size at this poll
	More  bool  // more data is waiting (MaxChunk reached)
}

// Reader reads a file incrementally. It is not safe for concurrent use:
// the UI runs one poll at a time.
type Reader struct {
	fs         FS
	path       string
	maxInitial int64
	offset     int64
	started    bool
	previous   fs.FileInfo
	missing    bool
}

// InitialBytes is how much of a log the viewer reads when it opens.
const InitialBytes = 256 << 10

// NewReader prepares to follow path, starting maxInitial bytes before the
// end.
func NewReader(fsys FS, path string, maxInitial int64) *Reader {
	if fsys == nil {
		fsys = OS
	}
	if maxInitial <= 0 {
		maxInitial = 256 << 10
	}
	return &Reader{fs: fsys, path: path, maxInitial: maxInitial}
}

// Path is the file being read.
func (r *Reader) Path() string { return r.path }

// Poll reads what is new since the last poll.
func (r *Reader) Poll() Chunk {
	f, err := r.fs.Open(r.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			r.missing = true
		}
		return Chunk{State: stateOf(err), Err: err}
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return Chunk{State: stateOf(err), Err: err}
	}
	if fi.IsDir() {
		return Chunk{State: Failed, Err: errors.New("is a directory")}
	}
	size := fi.Size()
	var c Chunk
	c.Size = size
	start := r.offset
	switch {
	case !r.started:
		start = max(size-r.maxInitial, 0)
		c.Reset = true
	case r.missing || (os.SameFile(fi, fi) && !os.SameFile(r.previous, fi)):
		// Synthetic/demo FileInfo has no OS identity; keep its size-based behaviour.
		start, c.Reset = 0, true
	case size < r.offset:
		// Truncated or replaced: start over.
		start, c.Reset = 0, true
	case size == r.offset:
		return c
	}
	end := size
	if end-start > MaxChunk {
		end, c.More = start+MaxChunk, true
	}
	buf := make([]byte, end-start)
	n, err := f.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return Chunk{State: Failed, Err: err}
	}
	buf = buf[:n]
	if !r.started && start > 0 {
		// Skip the partial first line.
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	r.started = true
	r.previous, r.missing = fi, false
	r.offset = start + int64(n)
	c.Data = buf
	return c
}

func stateOf(err error) State {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Missing
	case errors.Is(err, fs.ErrPermission):
		return Unreadable
	}
	return Failed
}
