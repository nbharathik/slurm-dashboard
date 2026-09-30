package parse

import (
	"regexp"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// kvKey finds "Key=" tokens; keys start upper-case so "lr=0.1" inside values stays intact.
var kvKey = regexp.MustCompile(`(?:^|\s)([A-Z][A-Za-z0-9_:/.-]*)=`)

// KV parses one "scontrol show ... -o" line into values and ordered keys; a value runs to the next key.
func KV(line string) (map[string]string, []string) {
	m := map[string]string{}
	var order []string
	idx := kvKey.FindAllStringSubmatchIndex(line, -1)
	for i, loc := range idx {
		key := line[loc[2]:loc[3]]
		end := len(line)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		val := textsafe.Field(strings.TrimSpace(line[loc[1]:end]))
		if _, dup := m[key]; !dup {
			order = append(order, key)
		}
		m[key] = val
	}
	return m, order
}
