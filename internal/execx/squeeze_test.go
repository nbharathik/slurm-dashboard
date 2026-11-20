package execx

import (
	"strings"
	"testing"
)

// D12: padding does not count toward the output cap, across write splits.
func TestCapBufferSqueezesSpaces(t *testing.T) {
	line := "12345" + strings.Repeat(" ", 43) + "alice" + strings.Repeat(" ", 43) + "gpu\n"
	c := &capBuffer{limit: 40, squeeze: true}
	for _, part := range []string{line[:20], line[20:60], line[60:]} { // runs cut across writes
		if n, err := c.Write([]byte(part)); err != nil || n != len(part) {
			t.Fatalf("write %d, %v", n, err)
		}
	}
	if got := string(c.Bytes()); got != "12345 alice gpu\n" || c.Overflowed() {
		t.Fatalf("got %q, overflow %v", got, c.Overflowed())
	}
	plain := &capBuffer{limit: 40}
	_, _ = plain.Write([]byte(line))
	if !plain.Overflowed() {
		t.Fatal("without squeezing the padded line passes the cap")
	}
}
