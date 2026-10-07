package logs

import (
	"strings"
	"testing"
)

func TestBufferByteAndLogicalLineBounds(t *testing.T) {
	b := NewBuffer(MaxLines, false, nil)
	for range 1000 {
		b.Write([]byte(strings.Repeat("x", 10000)))
	}
	if b.Bytes() > MaxBytes || len(b.Line(0).Text) > MaxLineBytes || !strings.Contains(b.Line(0).Text, "truncated") {
		t.Fatal("unfinished line is not bounded and marked")
	}
	b.Write([]byte("\nnext\n"))
	if b.Line(1).Text != "next" {
		t.Fatal("truncation consumed the next line")
	}
	line := []byte(strings.Repeat("a", 40000) + "\n")
	for range 1000 {
		b.Write(line)
	}
	if b.Bytes() > MaxBytes || b.Dropped() == 0 {
		t.Fatalf("bytes=%d dropped=%d", b.Bytes(), b.Dropped())
	}
	for i := 0; i < b.Len(); i++ {
		if len(b.Line(i).Text) > MaxLineBytes {
			t.Fatal("oversized retained line")
		}
	}
}

func TestBufferSplitEscapeAndTabExpansion(t *testing.T) {
	b := NewBuffer(10, false, nil)
	b.Write([]byte("before\x1b]52;c;"))
	b.Write([]byte("secret\x07after\n"))
	if b.Line(0).Text != "beforeafter" {
		t.Fatalf("escape leaked: %q", b.Line(0).Text)
	}
	b.Write([]byte(strings.Repeat("\t", MaxLineBytes) + "\n"))
	if len(b.Line(1).Text) > MaxLineBytes {
		t.Fatal("tab expansion exceeded line cap")
	}
}

func TestSmallLogAllocatesSmallRing(t *testing.T) {
	b := NewBuffer(MaxLines, false, nil)
	b.Write([]byte("one line\n"))
	if len(b.ring) > 128 {
		t.Fatalf("small log reserved %d line slots", len(b.ring))
	}
	for range 300 {
		b.Write([]byte("next\n"))
	}
	if b.Line(0).Text != "one line" || b.Len() != 301 {
		t.Fatal("growing ring lost lines")
	}
}
