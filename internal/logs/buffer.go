package logs

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// MaxLines is the default line cap of a Buffer.
const (
	MaxLines      = 50000
	MaxBytes      = 8 << 20
	MaxLineBytes  = 64 << 10
	truncatedLine = " [line truncated]"
)

// Line is one display line with its highlight level.
type Line struct {
	Text  string
	Level Level
}

// Buffer keeps the last lines of a log. Carriage returns collapse, so a
// progress bar that redraws itself shows only its latest state.
type Buffer struct {
	max      int
	keepANSI bool
	hl       *Highlighter

	ring     []Line
	start    int // index of the oldest line in ring
	n        int
	partial  string // text after the last newline
	dropped  int    // lines dropped from the front
	bytes    int
	overflow bool
	gen      int // incremented on every change
}

// NewBuffer returns an empty buffer holding up to max lines.
func NewBuffer(maxLines int, keepANSI bool, hl *Highlighter) *Buffer {
	if maxLines <= 0 {
		maxLines = MaxLines
	}
	return &Buffer{max: min(maxLines, MaxLines), keepANSI: keepANSI, hl: hl}
}

// Reset empties the buffer.
func (b *Buffer) Reset() {
	b.ring, b.start, b.n, b.partial, b.dropped = nil, 0, 0, "", 0
	b.bytes, b.overflow = 0, false
	b.gen++
}

// Write appends raw file bytes.
func (b *Buffer) Write(data []byte) {
	if len(data) == 0 {
		return
	}
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		n := len(data)
		if i >= 0 {
			n = i
		}
		room := MaxLineBytes - len(truncatedLine) - len(b.partial)
		if !b.overflow && n > 0 {
			take := min(n, max(room, 0))
			b.partial += string(data[:take])
			if take < n {
				b.overflow = true
			}
		}
		if i < 0 {
			break
		}
		text := b.clean(b.partial)
		if b.overflow {
			text = clipLine(text, MaxLineBytes-len(truncatedLine)) + truncatedLine
		}
		b.push(text)
		b.partial = ""
		b.overflow = false
		data = data[i+1:]
	}
	for b.n > 0 && b.bytes+len(b.partial) > MaxBytes {
		b.drop()
	}

	b.gen++
}

// SetText replaces the content (for static text such as a batch script).
func (b *Buffer) SetText(s string) {
	b.Reset()
	b.Write([]byte(strings.TrimSuffix(s, "\n") + "\n"))
}

func (b *Buffer) push(text string) {
	l := Line{Text: text}
	if b.hl != nil {
		l.Level = b.hl.Level(ansi.Strip(text))
	}
	for b.n > 0 && (b.n == b.max || b.bytes+len(text) > MaxBytes-MaxLineBytes) {
		b.drop()
	}
	if b.n == len(b.ring) && b.n < b.max {
		next := make([]Line, min(b.max, max(128, len(b.ring)*2)))
		for i := 0; i < b.n; i++ {
			next[i] = b.ring[(b.start+i)%len(b.ring)]
		}
		b.ring, b.start = next, 0
	}
	b.ring[(b.start+b.n)%len(b.ring)] = l
	b.n++
	b.bytes += len(text)
}

// clean collapses carriage returns and strips ANSI codes unless kept.
func (b *Buffer) clean(s string) string {
	s = strings.TrimSuffix(s, "\r")
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	if !b.keepANSI {
		s = textsafe.Field(s)
	}
	if len(s) > MaxLineBytes {
		return clipLine(s, MaxLineBytes-len(truncatedLine)) + truncatedLine
	}
	return clipLine(s, MaxLineBytes)
}

// Len is the number of lines, including an unterminated last line.
func (b *Buffer) Len() int {
	if b.partial != "" {
		return b.n + 1
	}
	return b.n
}

// Line returns line i (0 = oldest kept).
func (b *Buffer) Line(i int) Line {
	if i == b.n && b.partial != "" {
		t := b.clean(b.partial)
		if b.overflow {
			t = clipLine(t, MaxLineBytes-len(truncatedLine)) + truncatedLine
		}
		l := Line{Text: t}
		if b.hl != nil {
			l.Level = b.hl.Level(ansi.Strip(t))
		}
		return l
	}
	if i < 0 || i >= b.n {
		return Line{}
	}
	return b.ring[(b.start+i)%len(b.ring)]
}

// Dropped counts lines dropped from the front to respect the cap.
func (b *Buffer) Dropped() int { return b.dropped }

// Gen changes whenever the content changes.
func (b *Buffer) Gen() int { return b.gen }

func (b *Buffer) drop() {
	b.bytes -= len(b.ring[b.start].Text)
	b.ring[b.start] = Line{}
	b.start = (b.start + 1) % len(b.ring)
	b.n--
	b.dropped++
}

// Bytes reports retained text bytes, including an unfinished line.
func (b *Buffer) Bytes() int { return b.bytes + len(b.partial) }

func clipLine(s string, n int) string {
	if len(s) <= n {
		return strings.Clone(s)
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return strings.Clone(s[:n])
}
