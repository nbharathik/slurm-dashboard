package parse

import (
	"regexp"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// kvKey finds "Key=" tokens in scontrol's one-line output. Keys start with
// an upper-case letter (every scontrol key does), which keeps values such as
// "Command=train.py lr=0.1" intact.
var kvKey = regexp.MustCompile(`(?:^|\s)([A-Z][A-Za-z0-9_:/.-]*)=`)

// KV parses one line of "scontrol show ... -o" output. A value runs until
// the next key and is trimmed, so values may contain spaces and '='. It
// returns the values and the keys in order.
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
