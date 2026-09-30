package textsafe

import (
	"strings"
	"unicode/utf8"
)

// Stream cleans terminal text across arbitrary read boundaries. Its zero value
// is ready to use; it retains only an incomplete UTF-8 rune and parser state.
type Stream struct {
	state   streamState
	partial string
}

type streamState uint8

const (
	ground streamState = iota
	escape
	intermediate
	csi
	controlString
	stringEscape
)

// Text removes controls while retaining newlines, as Text does for a full string.
func (s *Stream) Text(data []byte) string { return s.clean(string(data), false) }

// Flush finishes incomplete UTF-8 and discards unterminated control sequences.
func (s *Stream) Flush() string {
	out := s.clean("", true)
	*s = Stream{}
	return out
}

func (s *Stream) clean(data string, final bool) string {
	data = s.partial + data
	s.partial = ""
	var out strings.Builder
	out.Grow(len(data))
	for len(data) > 0 {
		if !final && !utf8.FullRuneInString(data) {
			s.partial = data
			break
		}
		r, n := utf8.DecodeRuneInString(data)
		data = data[n:]
		if s.skip(r) {
			continue
		}
		switch {
		case r == 0x1b:
			s.state = escape
		case r == 0x9b:
			s.state = csi
		case r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f:
			s.state = controlString
		case r == '\t' || r == 0x2028 || r == 0x2029:
			out.WriteByte(' ')
		case r == '\n':
			out.WriteByte('\n')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || invisible(r):
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// skip consumes a control sequence, or returns false to reprocess a rune as text.
func (s *Stream) skip(r rune) bool {
	switch s.state {
	case stringEscape:
		if r == '\\' {
			s.state = ground
			return true
		}
		s.state = controlString
		fallthrough
	case controlString:
		switch r {
		case 0x07, 0x9c:
			s.state = ground
		case 0x1b:
			s.state = stringEscape
		}
		return true
	case escape:
		switch r {
		case '[':
			s.state = csi
			return true
		case ']', 'P', 'X', '^', '_':
			s.state = controlString
			return true
		}
		s.state = intermediate
		fallthrough
	case intermediate:
		if r >= 0x20 && r <= 0x2f {
			return true
		}
		s.state = ground
		return r >= 0x30 && r <= 0x7e
	case csi:
		if r >= 0x20 && r <= 0x3f {
			return true
		}
		s.state = ground
		return r >= 0x40 && r <= 0x7e
	case ground:
	}
	return false
}
