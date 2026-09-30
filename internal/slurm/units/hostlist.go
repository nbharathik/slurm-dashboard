package units

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxHosts bounds host list expansion so a malformed list cannot exhaust
// memory.
const MaxHosts = 100000

// ExpandHostlist expands a host list such as "gpu[01-04,07]" keeping zero padding.
// "None assigned", "(null)" and empty give no hosts.
func ExpandHostlist(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "(null)", "none assigned", "n/a", "none":
		return nil, nil
	}
	var out []string
	for _, item := range splitOutsideBrackets(s, ',') {
		if item == "" {
			continue
		}
		hosts, err := expandOne(item)
		if err != nil {
			return nil, fmt.Errorf("host list %q: %w", s, err)
		}
		out = append(out, hosts...)
		if len(out) > MaxHosts {
			return nil, fmt.Errorf("host list %q: more than %d hosts", s, MaxHosts)
		}
	}
	return out, nil
}

func expandOne(item string) ([]string, error) {
	open := strings.IndexByte(item, '[')
	if open < 0 {
		if strings.ContainsAny(item, "]") {
			return nil, fmt.Errorf("unbalanced bracket in %q", item)
		}
		return []string{item}, nil
	}
	closeIdx := strings.IndexByte(item[open:], ']')
	if closeIdx < 0 {
		return nil, fmt.Errorf("unbalanced bracket in %q", item)
	}
	closeIdx += open
	prefix, body, suffix := item[:open], item[open+1:closeIdx], item[closeIdx+1:]

	var middles []string
	for _, r := range strings.Split(body, ",") {
		lo, hi, isRange := strings.Cut(r, "-")
		if !isRange {
			if _, err := strconv.ParseUint(lo, 10, 64); err != nil {
				return nil, fmt.Errorf("bad range %q", r)
			}
			middles = append(middles, lo)
			continue
		}
		a, err1 := strconv.ParseUint(lo, 10, 64)
		b, err2 := strconv.ParseUint(hi, 10, 64)
		if err1 != nil || err2 != nil || b < a {
			return nil, fmt.Errorf("bad range %q", r)
		}
		if b-a > MaxHosts {
			return nil, fmt.Errorf("range %q too large", r)
		}
		width := len(lo)
		for n := a; n <= b; n++ {
			middles = append(middles, fmt.Sprintf("%0*d", width, n))
		}
	}
	rests, err := expandOne(suffix)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(middles)*len(rests))
	for _, m := range middles {
		for _, r := range rests {
			out = append(out, prefix+m+r)
			if len(out) > MaxHosts {
				return nil, fmt.Errorf("more than %d hosts", MaxHosts)
			}
		}
	}
	return out, nil
}

// splitOutsideBrackets splits s on sep where sep is not inside [...].
func splitOutsideBrackets(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
