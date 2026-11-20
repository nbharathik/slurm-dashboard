package execx

import "strings"

// EditorArgv is the user's editor as argv: $VISUAL, else $EDITOR, else vi.
// A variable that holds only spaces counts as unset, so the file is never
// run in place of an editor.
func EditorArgv(getenv func(string) string) []string {
	return firstCommand(getenv, []string{"VISUAL", "EDITOR"}, "vi")
}

// PagerArgv is the user's pager as argv: $PAGER, else less.
func PagerArgv(getenv func(string) string) []string {
	return firstCommand(getenv, []string{"PAGER"}, "less")
}

func firstCommand(getenv func(string) string, vars []string, fallback string) []string {
	for _, v := range vars {
		if f := strings.Fields(getenv(v)); len(f) > 0 {
			return f
		}
	}
	return []string{fallback}
}
