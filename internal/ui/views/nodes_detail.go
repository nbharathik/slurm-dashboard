package views

import (
	"fmt"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// nodeDetail shows one node: state, GPUs by type (full names), CPUs,
// memory, features, boot time and every running job.
func (v *Nodes) nodeDetail(ctx *Context, w, h int) string {
	th := ctx.Theme
	u, ok := v.usage[v.detail]
	if !ok {
		return components.Panel(th, v.detail, "", w, h, false)
	}
	n := u.Node
	var b []string
	row := func(label, value string) { b = append(b, th.Muted.Render(layout.Pad(label, 11, false, ""))+value) }
	stateLabel := n.State
	if len(n.Flags) > 0 {
		stateLabel += "+" + strings.Join(n.Flags, "+")
	}
	row("State", stateLabel)
	if n.Reason != "" {
		row("Reason", n.Reason)
	}
	row("Partitions", strings.Join(n.Partitions, ", "))
	if n.GPUTotal > 0 {
		row("GPUs", fmt.Sprintf("%s  %d of %d in use", components.GPUCells(th, u.Mine, u.Others, u.Free, n.GPUTotal, 16), n.GPUTotal-u.Free, n.GPUTotal))
	}
	if n.MIGTotal > 0 {
		row("MIG", fmt.Sprintf("%d of %d slices in use", n.MIGTotal-u.MIGFree, n.MIGTotal))
	}
	for _, g := range n.GPUs {
		label := g.Type
		if short := units.GPUDisplayName(g.Type, ctx.Config.GPUNames); short != "" {
			label = short
			if ctx.Config.Detailed() && !strings.Contains(strings.ToLower(short), strings.ToLower(g.Type)) {
				label += th.Faint.Render(" (" + g.Type + ")")
			}
		}
		if label == "" {
			label = "gpu"
		}
		row("", fmt.Sprintf("%d/%d  %s", g.Alloc, g.Total, label))
	}
	if n.CPUTotal > 0 {
		row("CPUs", fmt.Sprintf("%d of %d allocated, load %.1f", n.CPUAlloc, n.CPUTotal, n.CPULoad))
	}
	if n.MemTotalMB > 0 {
		row("Memory", memDetail(ctx, n))
	}
	if f := freeIn(ctx, u); f != "" {
		row("Est. free", "in "+f)
	}
	if len(n.Features) > 0 {
		row("Features", strings.Join(n.Features, ", "))
	}
	if !n.BootTime.IsZero() {
		row("Booted", friendlyTime(ctx.Now, n.BootTime)+" ("+units.FormatShort(ctx.Now.Sub(n.BootTime))+" ago)")
	}
	b = append(b, "")
	if len(u.Jobs) == 0 {
		b = append(b, th.Muted.Render("No running jobs visible."))
	} else {
		b = append(b, th.Bold.Render("Running jobs"))
	}
	for _, j := range u.Jobs {
		who := j.User
		if who == ctx.Store.User {
			who = th.Accent.Render("you")
		}
		end := "no end time"
		if !j.EndTime.IsZero() {
			end = "estimated end " + friendlyTime(ctx.Now, j.EndTime) + " (in " + units.FormatShort(max(j.EndTime.Sub(ctx.Now), 0)) + ")"
		}
		res := fmt.Sprintf("%d CPU", j.CPUs/max(len(j.NodeList), 1))
		if g := state.GPUShare(j); g > 0 {
			kind := "GPU"
			if j.MIGSlices > 0 {
				kind = "MIG"
			}
			res += fmt.Sprintf(" %d %s", g, kind)
		}
		b = append(b, layout.Pad(j.ID.Raw, 11, false, "")+layout.Pad(who, 10, false, th.Sym.Ellipsis)+
			layout.Pad(res, 14, false, "")+th.Muted.Render(end))
	}
	if u.Estimate {
		b = append(b, "", th.Faint.Render("Multi-node jobs are assumed to spread evenly."))
	}
	return v.pane.render(ctx, n.Name, n.Name, "nodes:close", strings.Join(b, "\n"), w, h)
}
