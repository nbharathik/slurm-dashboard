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

// migProfile matches MIG profiles such as "1g.33gb" or "1c.3g.40gb", alone or after a GPU name.
var migProfile = regexp.MustCompile(`(?i)(?:^|[_-])(?:\d+c\.)?\d+g\.\d+gb(?:\+me)?$`)

// IsMIG reports whether a GRES type is a MIG slice rather than a whole GPU.
func IsMIG(typ string) bool { return migProfile.MatchString(typ) }

// GPUsByType parses a GRES list into per-type counts in first-seen order, merging repeats.
// Socket/index annotations, flags such as no_consume and other resources are ignored.
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

// GPUsByTypeTRES returns typed GPU counts in a TRES map and the untyped total (-1 if absent).
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
