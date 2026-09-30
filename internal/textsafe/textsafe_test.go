package textsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestField(t *testing.T) {
	cases := map[string]string{
		"train-lora":                           "train-lora",
		"a\tb":                                 "a b",
		"\x1b]52;c;ZXZpbA==\x07job":            "job",
		"\x1b]52;c;ZXZpbA==\x1b\\job":          "job",
		"\x1b[31mred\x1b[0m":                   "red",
		"\x1b[?1049h\x1b[2J":                   "",
		"\x1bPdevice control\x1b\\ok":          "ok",
		"\x1b(Bplain":                          "plain",
		"bell\x07 and nul\x00 and del\x7f":     "bell and nul and del",
		"line\nbreak\rreturn":                  "linebreakreturn",
		"\u009b31mc1 csi":                      "c1 csi",
		"\u009d0;title\u009cafter":             "after",
		"user\u202egnp.exe":                    "usergnp.exe",
		"a\u2066b\u2069c\u200bd\ufeffe\u200ff": "abcdef",
		"bad\xffutf8":                          "bad\ufffdutf8",
		"unterminated \x1b]0;title":            "unterminated ",
		"日本語 ok":                               "日本語 ok",
		"sep\u2028arator":                      "sep arator",
	}
	for in, want := range cases {
		if got := Field(in); got != want {
			t.Errorf("Field(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Text("a\nb\x1b[1mc"); got != "a\nbc" {
		t.Errorf("Text kept %q", got)
	}
}

// clean reports why out is not safe to print, or "".
func unsafeRune(out string, keepNL bool) string {
	if !utf8.ValidString(out) {
		return "invalid UTF-8"
	}
	for _, r := range out {
		switch {
		case r == '\n' && keepNL:
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return "control character"
		case invisible(r):
			return "invisible character"
		}
	}
	return ""
}

func FuzzField(f *testing.F) {
	for _, s := range []string{"job", "\x1b]52;c;x\x07y", "\x1b[1;2", "\u202e\u2066", "\xff\xfe", strings.Repeat("\x1b", 3)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, keep := range []bool{false, true} {
			out := clean(s, keep)
			if why := unsafeRune(out, keep); why != "" {
				t.Fatalf("clean(%q) = %q: %s", s, out, why)
			}
			if again := clean(out, keep); again != out {
				t.Fatalf("not idempotent: %q -> %q -> %q", s, out, again)
			}
		}
	})
}

func TestStreamAcrossReadBoundaries(t *testing.T) {
	for _, input := range []string{
		"hello\nworld\t日本語\n",
		"before\x1b]52;c;ZXZpbA==\x07after\n",
		"before\x1b]52;c;ZXZpbA==\x1b\\after",
		"\u009d0;title\u009cafter",
		"\x1bPdevice\x1b\\ok\x1b[31mred\x1b[0m",
		"\x1b(Bplain\u009b31mc1\u202e\u200b",
		"bad\xffutf8\xe6\x97",
		"unterminated\x1b]52;c;payload",
		"\x1b[31", "\x1b", "\x1b\ntext", "\x1b]title\x1b\x1b\\end",
	} {
		for split := 0; split <= len(input); split++ {
			var stream Stream
			got := stream.Text([]byte(input[:split])) + stream.Text([]byte(input[split:])) + stream.Flush()
			if want := Text(input); got != want {
				t.Errorf("input %q split %d: got %q, want %q", input, split, got, want)
			}
		}
		var stream Stream
		var out strings.Builder
		for i := range len(input) {
			out.WriteString(stream.Text([]byte(input[i : i+1])))
		}
		out.WriteString(stream.Flush())
		if out.String() != Text(input) {
			t.Errorf("byte reads %q: %q", input, out.String())
		}
		if got := stream.Text([]byte("next")); got != "next" {
			t.Fatalf("flush did not reset stream: %q", got)
		}
	}
}

func TestStreamDoesNotBufferControlPayload(t *testing.T) {
	var stream Stream
	stream.Text([]byte("\x1b]52;c;"))
	for range 100 {
		if got := stream.Text([]byte(strings.Repeat("x", 4096))); got != "" || len(stream.partial) > 3 {
			t.Fatal("control payload leaked or was retained")
		}
	}
	if got := stream.Text([]byte("\x07done")); got != "done" {
		t.Fatalf("terminator: %q", got)
	}
}

func FuzzStream(f *testing.F) {
	for _, s := range []string{"job", "\x1b]52;c;x\x07y", "\x1b[1;2", "日本語", "\xff\xfe", "\u009d0;title\u009cafter"} {
		f.Add(s, uint8(1))
	}
	f.Fuzz(func(t *testing.T, input string, chunk uint8) {
		var stream Stream
		var out strings.Builder
		step := int(chunk) + 1
		for pos := 0; pos < len(input); pos += step {
			out.WriteString(stream.Text([]byte(input[pos:min(pos+step, len(input))])))
		}
		out.WriteString(stream.Flush())
		if got := out.String(); got != Text(input) || unsafeRune(got, true) != "" {
			t.Fatalf("stream(%q) = %q, want %q", input, got, Text(input))
		}
	})
}
