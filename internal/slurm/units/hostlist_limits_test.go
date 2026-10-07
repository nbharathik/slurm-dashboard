package units

import (
	"strings"
	"testing"
)

func TestHostlistExpansionPreflight(t *testing.T) {
	for _, s := range []string{"n[0-18446744073709551615]", "n[18446744073709551614-18446744073709551615][0-99999]", "n[0-99999][0-99999]", strings.Repeat("n", 256) + "[0-99999]", strings.Repeat("[1]", 65), "n[" + strings.Repeat("0", 60000) + "-99999]", "n[" + strings.Repeat("0", 200) + "-99999]"} {
		if _, err := ExpandHostlist(s); err == nil {
			t.Fatalf("unsafe range accepted: %.100s", s)
		}
	}
	got, err := ExpandHostlist("n[18446744073709551614-18446744073709551615]")
	if err != nil || len(got) != 2 || got[1] != "n18446744073709551615" {
		t.Fatalf("bounded uint64 endpoint: %v %v", got, err)
	}
}
