package units

import (
	"path/filepath"
	"strconv"
	"strings"
)

// LogVars are the values used to expand Slurm output-file patterns.
type LogVars struct {
	JobID      string // %j
	Name       string // %x
	User       string // %u
	ArrayJobID string // %A
	TaskID     string // %a
	FirstNode  string // %N
	WorkDir    string // relative paths are joined to this
}

// ExpandLogPattern expands %j, %x, %u, %A, %a, %N and %% (with optional
// zero-padding widths such as %4a) in an StdOut/StdErr path from scontrol,
// and makes it absolute relative to WorkDir. Unknown specifiers are kept.
func ExpandLogPattern(p string, v LogVars) string {
	if strings.ContainsRune(p, '%') {
		var b strings.Builder
		for i := 0; i < len(p); i++ {
			if p[i] != '%' || i+1 >= len(p) {
				b.WriteByte(p[i])
				continue
			}
			j := i + 1
			for j < len(p) && p[j] >= '0' && p[j] <= '9' {
				j++
			}
			if j >= len(p) {
				b.WriteString(p[i:])
				break
			}
			width, _ := strconv.Atoi(p[i+1 : j])
			var val string
			known := true
			switch p[j] {
			case 'j':
				val = v.JobID
			case 'x':
				val = v.Name
			case 'u':
				val = v.User
			case 'A':
				val = v.ArrayJobID
			case 'a':
				val = v.TaskID
			case 'N':
				val = v.FirstNode
			case '%':
				val = "%"
			default:
				known = false
			}
			if !known {
				b.WriteString(p[i : j+1])
			} else {
				if width > 0 && len(val) < width && isDigits(val) {
					val = strings.Repeat("0", width-len(val)) + val
				}
				b.WriteString(val)
			}
			i = j
		}
		p = b.String()
	}
	if p != "" && !filepath.IsAbs(p) && v.WorkDir != "" {
		p = filepath.Join(v.WorkDir, p)
	}
	return p
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
