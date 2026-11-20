package textsafe

import "strings"

// CSVCell stops a spreadsheet from reading a cell (a job name another user
// chose) as a formula: one starting with = + - @ tab or CR gets a leading '.
// Plain numbers such as -5 are left as they are.
func CSVCell(s string) string {
	if s == "" || !strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return s
	}
	if s == "-" || (s[0] == '-' && isNumber(s[1:])) {
		return s
	}
	return "'" + s
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}
