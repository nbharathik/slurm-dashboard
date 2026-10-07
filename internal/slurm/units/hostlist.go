package units

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxHosts bounds host list expansion so a malformed list cannot exhaust
// memory.
const (
	MaxHosts     = 100000
	MaxHostBytes = 8 << 20
	maxHostName  = 255
)

// ExpandHostlist expands a host list such as "gpu[01-04,07]" keeping zero padding.
// "None assigned", "(null)" and empty give no hosts.
func ExpandHostlist(s string) ([]string, error) {
	if len(s) > 1<<20 {
		return nil, fmt.Errorf("host expression exceeds byte budget")
	}
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "(null)", "none assigned", "n/a", "none":
		return nil, nil
	}
	var out []string
	totalBytes := 0
	for _, item := range splitOutsideBrackets(s, ',') {
		if item == "" {
			continue
		}
		hosts, err := expandOne(item)
		if err != nil {
			return nil, fmt.Errorf("host list %q: %w", s, err)
		}
		if len(hosts) > MaxHosts-len(out) {
			return nil, fmt.Errorf("more than %d hosts", MaxHosts)
		}
		for _, h := range hosts {
			if len(h) > maxHostName || len(h) > MaxHostBytes-totalBytes {
				return nil, fmt.Errorf("host list exceeds byte budget")
			}
			totalBytes += len(h)
		}
		out = append(out, hosts...)
		if len(out) > MaxHosts {
			return nil, fmt.Errorf("host list %q: more than %d hosts", s, MaxHosts)
		}
	}
	return out, nil
}

func expandOne(item string) ([]string, error) {
	if len(item) > 65536 || strings.Count(item, "[") > 64 {
		return nil, fmt.Errorf("host expression too complex")
	}
	open := strings.IndexByte(item, '[')
	if open < 0 {
		if strings.ContainsAny(item, "]") {
			return nil, fmt.Errorf("unbalanced bracket in %q", item)
		}
		if len(item) > maxHostName {
			return nil, fmt.Errorf("host name too long")
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
	middleBytes := 0
	for _, r := range strings.Split(body, ",") {
		lo, hi, isRange := strings.Cut(r, "-")
		if !isRange {
			if _, err := strconv.ParseUint(lo, 10, 64); err != nil {
				return nil, fmt.Errorf("bad range %q", r)
			}
			if len(middles) >= MaxHosts {
				return nil, fmt.Errorf("too many hosts")
			}
			if len(prefix)+len(lo) > maxHostName || len(lo) > MaxHostBytes-middleBytes {
				return nil, fmt.Errorf("host list exceeds byte budget")
			}
			middleBytes += len(lo)
			middles = append(middles, lo)
			continue
		}
		a, err1 := strconv.ParseUint(lo, 10, 64)
		b, err2 := strconv.ParseUint(hi, 10, 64)
		if err1 != nil || err2 != nil || b < a {
			return nil, fmt.Errorf("bad range %q", r)
		}
		if b-a >= uint64(MaxHosts-len(middles)) { //nolint:gosec // middles never exceeds MaxHosts
			return nil, fmt.Errorf("range %q too large", r)
		}
		width := len(lo)
		maxWidth := max(width, len(hi))
		if len(prefix)+maxWidth > maxHostName || uint64(maxWidth) > uint64(MaxHostBytes-middleBytes)/(b-a+1) { //nolint:gosec // both lengths and the remaining budget are nonnegative
			return nil, fmt.Errorf("host list exceeds byte budget")
		}
		for offset := uint64(0); offset < b-a+1; offset++ {
			m := fmt.Sprintf("%0*d", width, a+offset)
			middleBytes += len(m)
			middles = append(middles, m)
		}
	}
	rests, err := expandOne(suffix)
	if err != nil {
		return nil, err
	}
	if len(rests) > 0 && len(middles) > MaxHosts/len(rests) {
		return nil, fmt.Errorf("more than %d hosts", MaxHosts)
	}
	maxMiddle, maxRest := 0, 0
	for _, m := range middles {
		maxMiddle = max(maxMiddle, len(m))
	}
	for _, r := range rests {
		maxRest = max(maxRest, len(r))
	}
	maxLen := len(prefix) + maxMiddle + maxRest
	if maxLen > maxHostName || (maxLen > 0 && len(rests) > 0 && len(middles) > MaxHostBytes/maxLen/len(rests)) {
		return nil, fmt.Errorf("host list exceeds byte budget")
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
