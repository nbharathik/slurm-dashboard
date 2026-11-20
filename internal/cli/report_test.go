package cli

import (
	"strings"
	"testing"
)

// doctor --report hides short names too, and only as whole names.
func TestRenderReportHidesWholeNames(t *testing.T) {
	checks := []check{{Name: "storage", Status: checkOK, Detail: "/home/jb ok, /scratch/jb ok, custom tom-lab jbx"}}
	var b strings.Builder
	if err := renderReport(&b, checks, map[string]string{"/home/jb": "~", "jb": "USER", "tom": "USER"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"~ ok", "/scratch/USER ok", "custom", "USER-lab", "jbx"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}
