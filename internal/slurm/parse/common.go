// Package parse turns raw Slurm output into model types; parsers are pure, skip bad lines with warnings and never panic.
package parse

import (
	"bufio"
	"bytes"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// Sep is the field delimiter (ASCII unit separator), as job names can contain '|'.
const Sep = "\x1f"

// warnings collects parse warnings for one source.
type warnings struct {
	source string
	list   []model.ParseWarning
}

func (w *warnings) add(line int, raw, format string, args ...any) {
	raw = textsafe.Field(strings.ReplaceAll(raw, Sep, "\u241f")) // show the separator as a visible symbol
	if len(raw) > 300 {
		raw = raw[:300] + "…"
	}
	w.list = append(w.list, model.ParseWarning{Source: w.source, Line: line, Raw: raw, Msg: fmt.Sprintf(format, args...)})
}

// recover turns a parser panic into a warning.
func (w *warnings) recover() {
	if r := recover(); r != nil {
		w.add(0, "", "parser bug: %v\n%s", r, debug.Stack())
	}
}

// lines iterates over non-empty lines with their 1-based numbers. Lines
// longer than 1 MB are reported and skipped.
func lines(raw []byte, w *warnings, fn func(n int, line string)) {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fn(n, line)
	}
	if err := sc.Err(); err != nil {
		w.add(n+1, "", "stopped reading: %v", err)
	}
}

// cleanFields strips control sequences from every field (other users write them).
// Run it after splitting, as Sep is itself a control character.
func cleanFields(f []string) []string {
	for i := range f {
		f[i] = textsafe.Field(f[i])
	}
	return f
}

// fieldErr accumulates the first conversion error in a record.
type fieldErr struct{ err error }

func (f *fieldErr) set(err error) {
	if err != nil && f.err == nil {
		f.err = err
	}
}

func (f *fieldErr) atoi(s string) int {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		f.set(fmt.Errorf("%q is not an integer", s))
	}
	return n
}

func (f *fieldErr) atoi64(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f.set(fmt.Errorf("%q is not an integer", s))
	}
	return n
}

func (f *fieldErr) atof(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		f.set(fmt.Errorf("%q is not a number", s))
	}
	return v
}

func (f *fieldErr) time(s string, parse func(string, *time.Location) (time.Time, error)) time.Time {
	t, err := parse(s, nil)
	f.set(err)
	return t
}

// splitList splits "a,b,c", treating (null), N/A and empty as no items.
func splitList(s string) []string {
	s = strings.TrimSpace(s)
	switch s {
	case "", "(null)", "N/A", "None":
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
