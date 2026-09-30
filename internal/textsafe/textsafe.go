// Package textsafe strips terminal control sequences and invisible characters from text others wrote (job names, reasons, accounts).
package textsafe

import (
	"strings"
	"unicode/utf8"
)

// Field cleans a single-line value: strips escapes, controls, bidi and zero-width characters.
func Field(s string) string { return clean(s, false) }

// Text cleans multi-line text the same way, keeping newlines.
func Text(s string) string { return clean(s, true) }

func clean(s string, keepNL bool) string {
	if plain(s, keepNL) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			b.WriteRune(utf8.RuneError)
			i++
			continue
		case r == 0x1b: // ESC
			i = skipEscape(s, i+1)
			continue
		case r == 0x9b: // 8-bit CSI
			i = skipCSI(s, i+size)
			continue
		case r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f: // DCS, SOS, OSC, PM, APC
			i = skipString(s, i+size)
			continue
		case r == '\t':
			b.WriteByte(' ')
		case r == '\n' && keepNL:
			b.WriteByte('\n')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			// other C0 and C1 controls
		case r == 0x2028 || r == 0x2029: // line and paragraph separators
			b.WriteByte(' ')
		case invisible(r):
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// plain reports whether s is printable ASCII (plus \n when kept), the
// common case, which needs no copy.
func plain(s string, keepNL bool) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 || c >= 0x7f) && (c != '\n' || !keepNL) {
			return false
		}
	}
	return true
}

// invisible reports bidi controls and zero-width characters.
func invisible(r rune) bool {
	switch {
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069: // bidi embeddings, overrides, isolates
		return true
	case r == 0x200e || r == 0x200f || r == 0x061c: // LRM, RLM, ALM
		return true
	case r >= 0x200b && r <= 0x200d, r >= 0x2060 && r <= 0x2064, r == 0xfeff, r == 0x180e:
		return true
	}
	return false
}

// skipEscape skips the rest of an escape sequence that started with ESC
// just before s[i].
func skipEscape(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		return skipCSI(s, i+1)
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC
		return skipString(s, i+1)
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f { // intermediates, e.g. ESC ( B
		i++
	}
	if i < len(s) && s[i] >= 0x30 && s[i] <= 0x7e {
		i++
	}
	return i
}

// skipCSI skips parameters, intermediates and the final byte.
func skipCSI(s string, i int) int {
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
		i++
	}
	return i
}

// skipString skips a control string up to and including its terminator
// (BEL, ESC \ or U+009C). An unterminated string swallows the rest.
func skipString(s string, i int) int {
	for i < len(s) {
		switch {
		case s[i] == 0x07:
			return i + 1
		case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\':
			return i + 2
		case strings.HasPrefix(s[i:], "\u009c"):
			return i + len("\u009c")
		}
		i++
	}
	return i
}
