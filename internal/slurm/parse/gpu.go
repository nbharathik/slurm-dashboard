package parse

import (
	"sort"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// setGPUs fills a node's GPU groups from Gres, GresUsed, CfgTRES and AllocTRES.
// Whole GPUs and MIG slices are counted apart. Allocation comes from GresUsed,
// else typed AllocTRES; an untyped remainder goes to whole GPUs first.
func setGPUs(n *model.Node, gres, gresUsed, cfgTRES, allocTRES string) {
	total := units.GPUsByType(gres)
	if len(total) == 0 {
		typed, all := units.GPUsByTypeTRES(units.ParseTRES(cfgTRES))
		total = typed
		if rest := all - units.SumGPUs(typed); rest > 0 {
			total = append(total, units.GPUCount{N: rest})
		}
	}
	groups := make([]model.GPUGroup, 0, len(total))
	for _, c := range total {
		if c.N > 0 {
			groups = append(groups, model.GPUGroup{Type: c.Type, Total: c.N, MIG: units.IsMIG(c.Type)})
		}
	}
	// Whole GPUs first, then MIG slices; each by type.
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].MIG != groups[j].MIG {
			return !groups[i].MIG
		}
		return groups[i].Type < groups[j].Type
	})

	used := units.GPUsByType(gresUsed)
	untyped := 0
	if units.SumGPUs(used) == 0 {
		typed, all := units.GPUsByTypeTRES(units.ParseTRES(allocTRES))
		used = typed
		untyped = max(all-units.SumGPUs(typed), 0)
	}
	for _, c := range used {
		if c.Type == "" {
			untyped += c.N
			continue
		}
		if i := groupIndex(groups, c.Type); i >= 0 {
			groups[i].Alloc += c.N
		} else {
			untyped += c.N
		}
	}
	for i := range groups {
		if untyped == 0 {
			break
		}
		take := min(untyped, groups[i].Total-groups[i].Alloc)
		groups[i].Alloc += max(take, 0)
		untyped -= max(take, 0)
	}

	var types []string
	for _, g := range groups {
		if g.MIG {
			n.MIGTotal += g.Total
			n.MIGAlloc += g.Alloc
			continue
		}
		n.GPUTotal += g.Total
		n.GPUAlloc += g.Alloc
		if g.Type != "" {
			types = append(types, g.Type)
		}
	}
	n.GPUType = strings.Join(types, ",")
	if len(groups) > 0 {
		n.GPUs = groups
	}
}

func groupIndex(groups []model.GPUGroup, typ string) int {
	for i, g := range groups {
		if g.Type == typ {
			return i
		}
	}
	return -1
}
