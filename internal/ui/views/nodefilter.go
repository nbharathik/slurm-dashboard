package views

import (
	"fmt"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// NodeFilter is a parsed Nodes filter: space-separated terms that must all
// match. state:idle,mixed, part:gpu, feat:ib, gpu:h200 (a type) or gpu:>0
// (free GPUs), cpu:>=16 (free CPUs), and bare words that match the node
// name or its GPU type.
type NodeFilter struct {
	Raw   string
	terms []nodeTerm
}

type nodeTerm struct {
	key    string
	values []string
	op     string
	num    int
}

// ParseNodeFilter parses filter text. An empty string matches everything.
func ParseNodeFilter(s string) (NodeFilter, error) {
	f := NodeFilter{Raw: strings.TrimSpace(s)}
	for _, tok := range strings.Fields(s) {
		k, v, hasKey := strings.Cut(tok, ":")
		if !hasKey || v == "" {
			f.terms = append(f.terms, nodeTerm{values: []string{strings.ToLower(tok)}})
			continue
		}
		t := nodeTerm{key: strings.ToLower(k), values: strings.Split(strings.ToLower(v), ",")}
		switch t.key {
		case "state", "st":
			t.key = "state"
		case "part", "partition", "p":
			t.key = "part"
		case "feat", "feature", "f":
			t.key = "feat"
		case "gpu", "gpus":
			t.key = "gpu"
			if op, n, err := numTerm(v); err == nil {
				t.op, t.num = op, n
			}
		case "cpu", "cpus":
			t.key = "cpu"
			op, n, err := numTerm(v)
			if err != nil {
				return f, fmt.Errorf("cpu:%s: use cpu:>=16 (free CPUs)", v)
			}
			t.op, t.num = op, n
		default:
			return f, fmt.Errorf("unknown filter %q (use state, part, feat, gpu, cpu or a name)", k)
		}
		f.terms = append(f.terms, t)
	}
	return f, nil
}

// Empty reports whether the filter matches everything.
func (f NodeFilter) Empty() bool { return len(f.terms) == 0 }

// Match reports whether a node passes every term. aliases are the
// [gpu] aliases, so gpu:h200 matches "nvidia_h200_nvl" by its short name.
func (f NodeFilter) Match(u state.NodeUsage, aliases map[string]string) bool {
	for _, t := range f.terms {
		if !t.match(u, aliases) {
			return false
		}
	}
	return true
}

func (t nodeTerm) match(u state.NodeUsage, aliases map[string]string) bool {
	n := u.Node
	anyOf := func(have []string) bool {
		for _, v := range t.values {
			for _, h := range have {
				if strings.EqualFold(h, v) {
					return true
				}
			}
		}
		return false
	}
	types := func() string {
		var b strings.Builder
		for _, g := range n.GPUs {
			b.WriteString(strings.ToLower(g.Type + " " + units.GPUDisplayName(g.Type, aliases) + " "))
		}
		return b.String()
	}
	switch t.key {
	case "":
		w := t.values[0]
		return strings.Contains(strings.ToLower(n.Name), w) || strings.Contains(types(), w)
	case "state":
		label := state.NodeStateLabel(n)
		return anyOf([]string{label, strings.TrimSuffix(label, "*"), strings.ToLower(n.State)}) ||
			(n.HasFlag("DRAIN") && anyOf([]string{"drain"}))
	case "part":
		return anyOf(n.Partitions)
	case "feat":
		return anyOf(n.Features)
	case "gpu":
		if t.op != "" {
			return compareNum(u.Free+u.MIGFree, t.op, t.num)
		}
		all := types()
		for _, v := range t.values {
			if strings.Contains(all, v) {
				return true
			}
		}
		return false
	case "cpu":
		free := 0
		if state.Available(n) {
			free = max(n.CPUTotal-n.CPUAlloc, 0)
		}
		return compareNum(free, t.op, t.num)
	}
	return true
}
