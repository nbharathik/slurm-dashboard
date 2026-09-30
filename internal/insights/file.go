package insights

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// FileName is the log inside the state directory.
const FileName = "storage.jsonl"

// Open reads the log at path, dropping old or unreadable lines (rewriting the file), and returns a Log that appends there.
// A missing or unreadable file gives an empty Log; an empty path keeps it in memory.
func Open(path string, now time.Time) *Log {
	l := New(nil, now)
	l.path = path
	if path == "" {
		return l
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	var all []Sample
	dropped := false
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var s Sample
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		if json.Unmarshal(sc.Bytes(), &s) != nil || s.Key == "" || s.T.IsZero() || s.Used < 0 || now.Sub(s.T) > Keep {
			dropped = true
			continue
		}
		all = append(all, s)
	}
	l = New(all, now)
	l.path = path
	if dropped {
		_ = l.rewrite() // best effort
	}
	return l
}

// Persist appends samples to the file (mode 0600, dir 0700); samples stay in memory on error.
func (l *Log) Persist(samples []Sample) error {
	if l == nil || l.path == "" || len(samples) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, s := range samples {
		b, err := json.Marshal(s)
		if err != nil {
			continue
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	_, werr := f.Write(buf.Bytes())
	return errors.Join(werr, f.Close())
}

// rewrite replaces the file with the samples in memory, atomically.
func (l *Log) rewrite() error {
	l.mu.RLock()
	var buf bytes.Buffer
	for _, series := range l.by {
		for _, s := range series {
			b, err := json.Marshal(s)
			if err == nil {
				buf.Write(b)
				buf.WriteByte('\n')
			}
		}
	}
	l.mu.RUnlock()
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
