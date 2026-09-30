package views

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Filter is a parsed table filter: space-separated terms that must all match
// (state:, part:, user:, gpu:, name:~regex, id:, or bare fuzzy words).
type Filter struct {
	Raw   string
	terms []filterTerm
}

type filterTerm struct {
	key    string
	values []string
	op     string
	num    int
	re     *regexp.Regexp
	word   string
}

// stateCodes maps short codes and friendly names to states.
var stateCodes = map[string][]model.JobState{
	"R": {model.StateRunning}, "RUN": {model.StateRunning}, "RUNNING": {model.StateRunning},
	"PD": {model.StatePending}, "PEND": {model.StatePending}, "PENDING": {model.StatePending},
	"CG": {model.StateCompleting}, "CD": {model.StateCompleted}, "COMPLETED": {model.StateCompleted},
	"F":      {model.StateFailed, model.StateOOM, model.StateTimeout, model.StateNodeFail, model.StateBootFail},
	"FAILED": {model.StateFailed}, "CA": {model.StateCancelled}, "CANCELLED": {model.StateCancelled},
	"TO": {model.StateTimeout}, "TIMEOUT": {model.StateTimeout}, "OOM": {model.StateOOM},
	"NF": {model.StateNodeFail}, "S": {model.StateSuspended}, "PR": {model.StatePreempted},
}

// ParseFilter parses filter text. An empty string matches everything.
func ParseFilter(s string) (Filter, error) {
	f := Filter{Raw: strings.TrimSpace(s)}
	for _, tok := range strings.Fields(s) {
		k, v, hasKey := strings.Cut(tok, ":")
		if !hasKey || v == "" {
			f.terms = append(f.terms, filterTerm{word: strings.ToLower(tok)})
			continue
		}
		t := filterTerm{key: strings.ToLower(k)}
		switch t.key {
		case "state", "st":
			t.key = "state"
			for _, c := range strings.Split(strings.ToUpper(v), ",") {
				if c == "H" || c == "HELD" {
					t.values = append(t.values, "HELD")
					continue
				}
				if _, ok := stateCodes[c]; !ok {
					return f, fmt.Errorf("state %q: use R, PD, CG, CD, F, CA, TO, OOM, NF, S or H", c)
				}
				t.values = append(t.values, c)
			}
		case "part", "partition", "p":
			t.key, t.values = "part", strings.Split(v, ",")
		case "user", "u":
			t.key, t.values = "user", strings.Split(v, ",")
		case "id":
			t.values = strings.Split(v, ",")
		case "gpu", "gpus":
			t.key = "gpu"
			op, n, err := numTerm(v)
			if err != nil {
				return f, fmt.Errorf("gpu:%s: use gpu:>0, gpu:2 or gpu:<4", v)
			}
			t.op, t.num = op, n
		case "name", "n":
			t.key = "name"
			if re, ok := strings.CutPrefix(v, "~"); ok {
				rx, err := regexp.Compile("(?i)" + re)
				if err != nil {
					return f, fmt.Errorf("name:~%s: %v", re, err)
				}
				t.re = rx
			} else {
				t.values = []string{strings.ToLower(v)}
			}
		default:
			return f, fmt.Errorf("unknown filter %q (use state, part, user, gpu, name or id)", k)
		}
		f.terms = append(f.terms, t)
	}
	return f, nil
}

// Empty reports whether the filter matches everything.
func (f Filter) Empty() bool { return len(f.terms) == 0 }

// Match reports whether a job passes every term.
func (f Filter) Match(j model.Job) bool {
	for _, t := range f.terms {
		if !t.match(j) {
			return false
		}
	}
	return true
}

func (t filterTerm) match(j model.Job) bool {
	switch t.key {
	case "":
		return fuzzyContains(strings.ToLower(j.Name), t.word) || strings.HasPrefix(j.ID.Raw, t.word)
	case "state":
		for _, v := range t.values {
			if v == "HELD" {
				if j.State == model.StatePending && strings.HasPrefix(j.Reason, "JobHeld") {
					return true
				}
				continue
			}
			for _, s := range stateCodes[v] {
				if j.State == s {
					return true
				}
			}
		}
		return false
	case "part":
		for _, v := range t.values {
			for _, p := range strings.Split(j.Partition, ",") {
				if strings.EqualFold(p, v) {
					return true
				}
			}
		}
		return false
	case "user":
		for _, v := range t.values {
			if strings.EqualFold(j.User, v) {
				return true
			}
		}
		return false
	case "id":
		for _, v := range t.values {
			if j.ID.Raw == v || strings.HasPrefix(j.ID.Raw, v+"_") || strconv.FormatUint(j.ID.ArrayJobID, 10) == v {
				return true
			}
		}
		return false
	case "gpu":
		return compareNum(j.GPUs, t.op, t.num)
	case "name":
		if t.re != nil {
			return t.re.MatchString(j.Name)
		}
		return strings.Contains(strings.ToLower(j.Name), t.values[0])
	}
	return true
}

// numTerm parses a comparison such as ">0", ">=4", "<2" or "3".
func numTerm(v string) (op string, n int, err error) {
	op = "="
	for _, o := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(v, o) {
			op, v = o, strings.TrimPrefix(v, o)
			break
		}
	}
	n, err = strconv.Atoi(v)
	return op, n, err
}

// compareNum applies a numTerm comparison.
func compareNum(have int, op string, want int) bool {
	switch op {
	case ">":
		return have > want
	case ">=":
		return have >= want
	case "<":
		return have < want
	case "<=":
		return have <= want
	}
	return have == want
}

// fuzzyContains reports whether the letters of pattern appear in s in
// order (a subsequence match), like fzf.
func fuzzyContains(s, pattern string) bool {
	i := 0
	for _, r := range s {
		if i < len(pattern) && r == rune(pattern[i]) {
			i++
		}
	}
	return i == len(pattern)
}
