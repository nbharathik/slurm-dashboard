package views

import (
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// gpuTypeLabel is the short name of a node's whole-GPU types, or "MIG"
// for a node with only MIG slices.
func gpuTypeLabel(ctx *Context, n model.Node) string {
	if name := units.GPUDisplayNames(n.GPUType, ctx.Config.GPUNames); name != "" {
		return name
	}
	if n.MIGTotal > 0 {
		return "MIG"
	}
	return ""
}

// nodeMemCell renders "free/total" memory, or "-/total" when jobs run on
// the node without allocating memory (Slurm does not track it there).
func nodeMemCell(ctx *Context, n model.Node) string {
	th := ctx.Theme
	switch {
	case n.MemTotalMB <= 0:
		return ""
	case !n.MemTracked():
		return th.Faint.Render("-/") + units.FormatMB(float64(n.MemTotalMB))
	}
	return units.FormatMB(float64(n.MemTotalMB-n.MemAllocMB)) + "/" + units.FormatMB(float64(n.MemTotalMB))
}

// memDetail is the node detail's memory line.
func memDetail(ctx *Context, n model.Node) string {
	free := ""
	if n.MemFreeMB >= 0 {
		free = " " + ctx.Theme.Sym.Separator + " OS reports " + units.FormatMB(float64(n.MemFreeMB)) + " free"
	}
	if !n.MemTracked() {
		return "not tracked by Slurm (jobs here did not request memory) of " + units.FormatMB(float64(n.MemTotalMB)) + free
	}
	return units.FormatMB(float64(n.MemAllocMB)) + " of " + units.FormatMB(float64(n.MemTotalMB)) + " allocated" + free
}
