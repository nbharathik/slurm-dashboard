package execx

import (
	"slices"
	"testing"
)

func TestEditorArgv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		vars map[string]string
		want []string
	}{
		{map[string]string{}, []string{"vi"}},
		{map[string]string{"EDITOR": "nano"}, []string{"nano"}},
		{map[string]string{"VISUAL": "code --wait", "EDITOR": "nano"}, []string{"code", "--wait"}},
		{map[string]string{"VISUAL": "   ", "EDITOR": "nano"}, []string{"nano"}}, // spaces only: unset
	} {
		if got := EditorArgv(env(tc.vars)); !slices.Equal(got, tc.want) {
			t.Errorf("%v: got %v, want %v", tc.vars, got, tc.want)
		}
	}
	if got := PagerArgv(env(map[string]string{"PAGER": " "})); !slices.Equal(got, []string{"less"}) {
		t.Errorf("pager %v", got)
	}
}
