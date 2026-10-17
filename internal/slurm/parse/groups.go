package parse

import (
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// Groups parses "id -Gn": group names separated by spaces.
func Groups(raw []byte) (groups []string, warns []model.ParseWarning) {
	w := &warnings{source: "groups"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(_ int, line string) {
		for _, g := range strings.Fields(line) {
			if g = textsafe.Field(g); g != "" {
				groups = append(groups, g)
			}
		}
	})
	return groups, w.list
}
