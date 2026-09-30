package units

import (
	"regexp"
	"strconv"
	"strings"
)

// GPUCount is a number of GPUs of one type; Type is "" when Slurm did not
// say which.
type GPUCount struct {
	Type string
	N    int
}

// migProfile matches MIG slice profiles such as "1g.33gb", "3g.40gb",
// "1g.10gb+me" or "1c.3g.40gb", on their own or after a GPU name
// ("nvidia_a100_3g.39gb").
var migProfile = regexp.MustCompile(`(?i)(?:^|[_-])(?:\d+c\.)?\d+g\.\d+gb(?:\+me)?$`)

// IsMIG reports whether a GRES type is a MIG slice rather than a whole GPU.
func IsMIG(typ string) bool { return migProfile.MatchString(typ) }

// GPUsByType parses a GRES list, as in a node's Gres= and GresUsed= or
// squeue's %b, into per-type counts in first-seen order, merging repeated
// types. Socket and index annotations ("(S:0-1)", "(IDX:0,2)") and flags
// such as no_consume are ignored, as are other resources.
func GPUsByType(s string) []GPUCount {
	s = strings.TrimSpace(s)
	switch s {
	case "", "N/A", "(null)":
		return nil
	}
	var out []GPUCount
	for _, item := range splitOutsideParens(s) {
		if n, t, ok := parseOneGRES(item); ok {
			out = addCount(out, t, n)
		}
	}
	return out
}

// GPUsByTypeTRES returns the typed GPU counts in a TRES map
// ("gres/gpu:h200" = "2") and the untyped total ("gres/gpu"), or -1 when
// there is no untyped entry.
func GPUsByTypeTRES(m map[string]string) (typed []GPUCount, total int) {
	total = -1
	if v, ok := m["gres/gpu"]; ok {
		total, _ = strconv.Atoi(v)
	}
	for k, v := range m {
		t, ok := strings.CutPrefix(k, "gres/gpu:")
		if !ok || t == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			typed = addCount(typed, t, n)
		}
	}
	sortCounts(typed)
	return typed, total
}

// SumGPUs adds up counts.
func SumGPUs(c []GPUCount) int {
	n := 0
	for _, x := range c {
		n += x.N
	}
	return n
}

func addCount(out []GPUCount, typ string, n int) []GPUCount {
	for i := range out {
		if out[i].Type == typ {
			out[i].N += n
			return out
		}
	}
	return append(out, GPUCount{Type: typ, N: n})
}

func sortCounts(c []GPUCount) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].Type < c[j-1].Type; j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}
