package units

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

var (
	taskSpecRe = regexp.MustCompile(`^(\d+|\[[0-9,%\-]+\]|[0-9,%\-]+)$`)
	stepRe     = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// ParseJobID parses job IDs as Slurm prints them: 123, 123_4, 123_[5-99%10], 123+0, 123.batch.
func ParseJobID(s string) (model.JobID, error) {
	s = strings.TrimSpace(s)
	id := model.JobID{Raw: s, HetOffset: -1}
	if s == "" {
		return id, fmt.Errorf("job id: empty")
	}

	rest := s
	// The step follows the first '.' that is outside brackets.
	if i := indexOutsideBrackets(rest, '.'); i >= 0 {
		id.Step = rest[i+1:]
		rest = rest[:i]
		if !stepRe.MatchString(id.Step) {
			return id, fmt.Errorf("job id %q: bad step %q", s, id.Step)
		}
	}
	if base, off, ok := strings.Cut(rest, "+"); ok {
		n, err := strconv.Atoi(off)
		if err != nil || n < 0 {
			return id, fmt.Errorf("job id %q: bad heterogeneous offset", s)
		}
		id.HetOffset, rest = n, base
	}
	if base, task, ok := strings.Cut(rest, "_"); ok {
		if !taskSpecRe.MatchString(task) {
			return id, fmt.Errorf("job id %q: bad array task %q", s, task)
		}
		id.TaskSpec, rest = task, base
	}
	n, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return id, fmt.Errorf("job id %q: not a number", s)
	}
	id.ArrayJobID = n
	return id, nil
}

// ValidJobRef accepts 123, 123_4, 123_[1-5], 123+0 and combinations.
var ValidJobRef = regexp.MustCompile(`^\d+(_(\d+|\[[0-9,%-]+\]))?(\+\d+)?$`)

func indexOutsideBrackets(s string, c byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
		case c:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
