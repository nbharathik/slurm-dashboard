package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lines(b *Buffer) []string {
	var out []string
	for i := range b.Len() {
		out = append(out, b.Line(i).Text)
	}
	return out
}

func TestReaderFollowsGrowthAndTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.out")
	r := NewReader(nil, path, 10)
	if c := r.Poll(); c.State != Missing {
		t.Fatalf("missing file: %+v", c)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(path, []byte("line one\nline two\nline three\n"), 0o600))
	c := r.Poll()
	// Starts 10 bytes before the end and skips the partial first line.
	if !c.Reset || string(c.Data) != "" && !strings.HasPrefix("line three\n", string(c.Data)) {
		t.Fatalf("initial = %+v %q", c, c.Data)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	must(err)
	_, err = f.WriteString("line four\n")
	must(err)
	must(f.Close())
	if c := r.Poll(); c.Reset || string(c.Data) != "line four\n" {
		t.Fatalf("append = %+v %q", c, c.Data)
	}
	if c := r.Poll(); len(c.Data) != 0 || c.State != Reading {
		t.Fatalf("no change = %+v", c)
	}
	must(os.WriteFile(path, []byte("new\n"), 0o600))
	if c := r.Poll(); !c.Reset || string(c.Data) != "new\n" {
		t.Fatalf("truncate = %+v %q", c, c.Data)
	}
	must(os.Chmod(path, 0))
	if os.Geteuid() != 0 {
		if c := r.Poll(); c.State != Unreadable {
			t.Fatalf("unreadable = %+v", c)
		}
	}
	if c := NewReader(nil, dir, 0).Poll(); c.State != Failed {
		t.Fatalf("directory = %+v", c)
	}
}

func TestReaderChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxChunk+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewReader(nil, path, MaxChunk*2)
	c := r.Poll()
	if !c.More || len(c.Data) != MaxChunk {
		t.Fatalf("first chunk %d more=%v", len(c.Data), c.More)
	}
	if c := r.Poll(); c.More || len(c.Data) != 10 {
		t.Fatalf("second chunk %d", len(c.Data))
	}
}

func TestReaderDetectsReplacement(t *testing.T) {
	for _, body := range []string{"new\n", "new\nmore\n"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("bytes=%d/missing=%v", len(body), missing), func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "job.out")
				if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				r := NewReader(nil, path, 100)
				if c := r.Poll(); string(c.Data) != "old\n" || !c.Reset {
					t.Fatalf("initial: %+v", c)
				}
				if err := os.WriteFile(path+".new", []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				if missing {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if c := r.Poll(); c.State != Missing {
						t.Fatalf("missing: %+v", c)
					}
				}
				if err := os.Rename(path+".new", path); err != nil {
					t.Fatal(err)
				}
				if c := r.Poll(); !c.Reset || string(c.Data) != body {
					t.Fatalf("replacement: %+v, data=%q", c, c.Data)
				}
				if c := r.Poll(); c.Reset || len(c.Data) != 0 {
					t.Fatalf("unchanged: %+v", c)
				}
			})
		}
	}
}

func TestBufferCleansAndCaps(t *testing.T) {
	hl, bad := NewHighlighter([]string{`custom boom`}, []string{`(`})
	if len(bad) != 1 {
		t.Fatalf("bad patterns = %v", bad)
	}
	b := NewBuffer(3, false, hl)
	b.Write([]byte("epoch 1\r 10%\r 50%\r100%\n\x1b[31mERROR: disk\x1b[0m\r\n"))
	b.Write([]byte("par"))
	b.Write([]byte("tial"))
	got := lines(b)
	if strings.Join(got, "|") != "100%|ERROR: disk|partial" {
		t.Fatalf("lines = %q", got)
	}
	if b.Line(1).Level != Error || b.Line(0).Level != Plain {
		t.Fatal("levels")
	}
	b.Write([]byte("\nWarning: slow\ncustom BOOM\n"))
	if got := lines(b); strings.Join(got, "|") != "partial|Warning: slow|custom BOOM" || b.Dropped() != 2 {
		t.Fatalf("capped = %q dropped %d", got, b.Dropped())
	}
	if b.Line(1).Level != Warning || b.Line(2).Level != Error || b.Line(9).Text != "" {
		t.Fatal("levels after wrap")
	}
	gen := b.Gen()
	b.SetText("a\tb\n")
	if lines(b)[0] != "a    b" || b.Gen() == gen || b.Dropped() != 0 {
		t.Fatalf("SetText = %q", lines(b))
	}
	keep := NewBuffer(0, true, nil)
	keep.Write([]byte("\x1b[1mbold\x1b[0m\n"))
	if keep.Line(0).Text != "\x1b[1mbold\x1b[0m" {
		t.Fatal("keepANSI")
	}
}

func TestBufferManyLines(t *testing.T) {
	b := NewBuffer(1000, false, nil)
	var sb strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	b.Write([]byte(sb.String()))
	if b.Len() != 1000 || b.Line(0).Text != "line 4000" || b.Line(999).Text != "line 4999" {
		t.Fatalf("len %d first %q last %q", b.Len(), b.Line(0).Text, b.Line(999).Text)
	}
}

func TestDefaultPatterns(t *testing.T) {
	hl, _ := NewHighlighter(nil, nil)
	for line, want := range map[string]Level{
		"Traceback (most recent call last):":                             Error,
		"torch.cuda.OutOfMemoryError: CUDA out of memory.":               Error,
		"slurmstepd: error: Detected 1 oom_kill event":                   Error,
		"srun: error: node01: task 0: Killed":                            Error,
		"Segmentation fault (core dumped)":                               Error,
		"*** JOB 12 ON n1 CANCELLED AT 2026-09-28 DUE TO TIME LIMIT ***": Error,
		"NCCL WARN something":                                            Error,
		"ValueError: bad":                                                Plain,
		"Error: bad input":                                               Error,
		"UserWarning: deprecated":                                        Plain,
		"WARNING: slow":                                                  Warning,
		"all good":                                                       Plain,
		"errors=0":                                                       Plain,
	} {
		if got := hl.Level(line); got != want {
			t.Errorf("Level(%q) = %d, want %d", line, got, want)
		}
	}
}

func TestMatcher(t *testing.T) {
	m, err := NewMatcher("loss")
	if err != nil || !m.Match("Loss: 0.3") {
		t.Fatal("lower-case query is case-insensitive")
	}
	m, _ = NewMatcher("Loss")
	if m.Match("loss: 0.3") || !m.Match("Loss: 0.3") {
		t.Fatal("upper-case query is case-sensitive")
	}
	m, _ = NewMatcher("a.c")
	if !m.Match("abc") {
		t.Fatal("regex characters make a regex")
	}
	m, _ = NewMatcher("x+y")
	if m.Match("x+y") || !m.Match("xxy") {
		t.Fatal("regex")
	}
	if _, err := NewMatcher("(unclosed"); err == nil {
		t.Fatal("bad regex accepted")
	}
	if m, err := NewMatcher(""); m != nil || err != nil || m.Match("x") || m.Spans("x") != nil {
		t.Fatal("empty query")
	}
	m, _ = NewMatcher("o")
	if s := m.Spans("foo"); len(s) != 2 {
		t.Fatalf("spans = %v", s)
	}
}
