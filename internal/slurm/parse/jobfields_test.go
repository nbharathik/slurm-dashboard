package parse

import (
	"strings"
	"testing"
)

// The format must not change: recorded fixtures and every cluster's
// squeue output depend on it, byte for byte.
func TestJobsFormatUnchanged(t *testing.T) {
	want := strings.Join([]string{
		"%i", "%F", "%K", "%P", "%q", "%a", "%T", "%M", "%l", "%L", "%D", "%C",
		"%m", "%b", "%r", "%N", "%S", "%e", "%V", "%Q", "%E", "%u", "%j",
	}, Sep)
	if got := JobsFormat(); got != want {
		t.Fatalf("format changed:\n got %q\nwant %q", got, want)
	}
	for i, c := range jobFormatCodes {
		if c == "" {
			t.Fatalf("field %d has no code", i)
		}
	}
	if jobFormatCodes[fName] != "%j" || fName != jobFieldCount-1 {
		t.Fatal("the job name must stay the last field")
	}
}
