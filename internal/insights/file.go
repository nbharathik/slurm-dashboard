package insights

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/privatefile"
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
	raw, err := privatefile.Read(path, 16<<20)
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

// Persist replaces the file with retained samples; samples stay in memory on error.
func (l *Log) Persist(samples []Sample) error {
	if l == nil || l.path == "" || len(samples) == 0 {
		return nil
	}
	return l.rewrite()
}

// rewrite replaces the file with the samples in memory, atomically.
func (l *Log) rewrite() error {
	l.mu.RLock()
	var buf bytes.Buffer
	for _, series := range l.by {
		for _, s := range series {
			b, err := json.Marshal(s)
			if err == nil {
				if buf.Len()+len(b)+1 > 16<<20 {
					l.mu.RUnlock()
					return fmt.Errorf("storage history exceeds persistence budget")
				}
				buf.Write(b)
				buf.WriteByte('\n')
			}
		}
	}
	l.mu.RUnlock()
	if buf.Len() > 16<<20 {
		return fmt.Errorf("storage history exceeds persistence budget")
	}
	return privatefile.Write(l.path, buf.Bytes())
}
