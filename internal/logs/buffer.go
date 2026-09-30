package logs

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// MaxLines is the default line cap of a Buffer.
const MaxLines = 50000

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

	ring    []Line
	start   int // index of the oldest line in ring
	n       int
	partial string // text after the last newline
	dropped int    // lines dropped from the front
	gen     int    // incremented on every change
}

// NewBuffer returns an empty buffer holding up to max lines.
func NewBuffer(maxLines int, keepANSI bool, hl *Highlighter) *Buffer {
	if maxLines <= 0 {
		maxLines = MaxLines
	}
	return &Buffer{max: maxLines, keepANSI: keepANSI, hl: hl}
}

// Reset empties the buffer.
func (b *Buffer) Reset() {
	b.ring, b.start, b.n, b.partial, b.dropped = nil, 0, 0, "", 0
	b.gen++
}

// Write appends raw file bytes.
func (b *Buffer) Write(data []byte) {
	if len(data) == 0 {
		return
	}
	s := b.partial + string(data)
	parts := strings.Split(s, "\n")
	b.partial = parts[len(parts)-1]
	for _, p := range parts[:len(parts)-1] {
		b.push(b.clean(p))
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
	if b.ring == nil {
		b.ring = make([]Line, 0, min(b.max, 1024))
	}
	if b.n < b.max {
		if len(b.ring) < b.max {
			b.ring = append(b.ring, l)
		} else {
			b.ring[(b.start+b.n)%b.max] = l
		}
		b.n++
		return
	}
	b.ring[b.start] = l
	b.start = (b.start + 1) % b.max
	b.dropped++
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
	return s
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
		l := Line{Text: t}
		if b.hl != nil {
			l.Level = b.hl.Level(ansi.Strip(t))
		}
		return l
	}
	if i < 0 || i >= b.n {
		return Line{}
	}
	if len(b.ring) < b.max {
		return b.ring[i]
	}
	return b.ring[(b.start+i)%b.max]
}

// Dropped counts lines dropped from the front to respect the cap.
func (b *Buffer) Dropped() int { return b.dropped }

// Gen changes whenever the content changes.
func (b *Buffer) Gen() int { return b.gen }
