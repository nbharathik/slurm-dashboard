package units

import (
	"strconv"
	"strings"
)

// ParseTRES splits "cpu=4,mem=8G,gres/gpu=1" into a map; "gres/gpu:1g.33gb:1" reads as key "gres/gpu:1g.33gb".
func ParseTRES(s string) map[string]string {
	m := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" || s == "(null)" || s == "N/A" {
		return m
	}
	for _, kv := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			m[k] = v
			continue
		}
		if i := strings.LastIndexByte(kv, ':'); strings.HasPrefix(kv, "gres/gpu:") && i > len("gres/gpu") {
			if _, err := strconv.Atoi(kv[i+1:]); err == nil {
				m[kv[:i]] = kv[i+1:]
			}
		}
	}
	return m
}

// GPUsFromTRES returns the GPU count and type from a TRES map; the untyped total wins, else typed entries sum.
func GPUsFromTRES(m map[string]string) (int, string) {
	if v, ok := m["gres/gpu"]; ok {
		n, _ := strconv.Atoi(v)
		return n, typesFromTRES(m)
	}
	total := 0
	for k, v := range m {
		if strings.HasPrefix(k, "gres/gpu:") {
			n, _ := strconv.Atoi(v)
			total += n
		}
	}
	return total, typesFromTRES(m)
}

func typesFromTRES(m map[string]string) string {
	var types []string
	for k := range m {
		if t, ok := strings.CutPrefix(k, "gres/gpu:"); ok && t != "" {
			types = append(types, t)
		}
	}
	return joinSorted(types)
}

// ParseGRES returns the GPU count and type from a GRES or TRES-style request
// such as "gpu:h200:4(S:0-1)" or "gres/gpu=2"; non-GPU resources are ignored.
func ParseGRES(s string) (int, string) {
	s = strings.TrimSpace(s)
	switch s {
	case "", "N/A", "(null)":
		return 0, ""
	}
	total := 0
	var types []string
	for _, item := range splitOutsideParens(s) {
		n, t, ok := parseOneGRES(item)
		if !ok {
			continue
		}
		total += n
		if t != "" {
			types = append(types, t)
		}
	}
	return total, joinSorted(types)
}

func parseOneGRES(item string) (int, string, bool) {
	if i := strings.IndexByte(item, '('); i >= 0 {
		item = item[:i]
	}
	item = strings.TrimSpace(item)
	item = strings.TrimPrefix(item, "gres:")
	item = strings.TrimPrefix(item, "gres/")
	// "gpu:h200=2" or "gpu=2"
	if name, val, ok := strings.Cut(item, "="); ok {
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, "", false
		}
		kind, typ, _ := strings.Cut(name, ":")
		if kind != "gpu" {
			return 0, "", false
		}
		return n, typ, true
	}
	parts := strings.Split(item, ":")
	if parts[0] != "gpu" {
		return 0, "", false
	}
	switch len(parts) {
	case 1: // "gpu" means one
		return 1, "", true
	case 2: // gpu:4 or gpu:a100
		if n, err := strconv.Atoi(parts[1]); err == nil {
			return n, "", true
		}
		return 1, parts[1], true
	default: // gpu:type:count, possibly with flags such as no_consume
		n, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return 0, "", false
		}
		var typ []string
		for _, p := range parts[1 : len(parts)-1] {
			if p != "no_consume" {
				typ = append(typ, p)
			}
		}
		return n, strings.Join(typ, ":"), true
	}
}

func splitOutsideParens(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func joinSorted(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var uniq []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			uniq = append(uniq, x)
		}
	}
	// small n: insertion sort keeps this dependency-free and stable
	for i := 1; i < len(uniq); i++ {
		for j := i; j > 0 && uniq[j] < uniq[j-1]; j-- {
			uniq[j], uniq[j-1] = uniq[j-1], uniq[j]
		}
	}
	return strings.Join(uniq, ",")
}
