package views

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// Render implements View.
func (v *Nodes) Render(ctx *Context, w, h int) string {
	var foot string
	if v.input != nil {
		foot = "\n" + v.input.View(ctx.Theme, w)
		h--
	}
	if v.detail != "" && ctx.Mode < layout.Wide {
		if ctx.Mode <= layout.Compact {
			return v.nodeDetail(ctx, w, h) + foot
		}
		half := h / 2
		return v.listing(ctx, w, h-half) + "\n" + v.nodeDetail(ctx, w, half) + foot
	}
	nodes := block{min: 5, render: func(w, h int) string {
		if v.detail != "" {
			tw := w * 6 / 10
			return joinH(v.listing(ctx, tw, h), tw, v.nodeDetail(ctx, w-tw, h))
		}
		return v.listing(ctx, w, h)
	}}
	if !ctx.Config.Detailed() {
		return stack([]block{nodes}, w, h, 0) + foot
	}
	bottom := block{min: 3, want: min(max(len(v.parts), len(v.res), 1)+2, max(h/3, 3)), render: func(w, h int) string {
		if len(v.res) == 0 || w < 80 {
			return partitionsPanel(ctx, v.parts, w, h)
		}
		return sideBySide(w, h, 0.62,
			func(w, h int) string { return partitionsPanel(ctx, v.parts, w, h) },
			func(w, h int) string { return reservationsPanel(ctx, v.res, w, h) })
	}}
	nodes.want = h - bottom.want
	return stack([]block{nodes, bottom}, w, h, 0) + foot
}

// listing is the status line, the maintenance line when there is one, and
// the table.
func (v *Nodes) listing(ctx *Context, w, h int) string {
	th := ctx.Theme
	head := []string{layout.Truncate(v.statusLine(ctx), w, th.Sym.Ellipsis)}
	if !ctx.Config.Detailed() {
		if l := v.reservationLine(ctx); l != "" {
			head = append(head, layout.Truncate(l, w, th.Sym.Ellipsis))
		}
	}
	var body string
	switch st := ctx.Store.Nodes; {
	case st.Err != nil && !st.Has:
		body = th.Crit.Render(layout.FirstLine(st.Err.Error()))
	case !st.Has:
		body = th.Muted.Render("loading" + th.Sym.Ellipsis)
	default:
		if v.kind == "gpu" && !v.anyGPU {
			v.table.Empty = "No GPU nodes on this cluster. Press G for all nodes."
		} else {
			v.table.Empty = "No nodes to show."
		}
		body = v.table.Render(th, ctx.Zones, w, h-len(head))
	}
	return strings.Join(append(head, body), "\n")
}

// statusLine counts what is listed, then names each way the view differs
// from the plain one: kind, filter, sort and grouping.
func (v *Nodes) statusLine(ctx *Context) string {
	th := ctx.Theme
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	var cpuFree, cpuTotal, gpuFree, gpuTotal int
	for _, u := range v.listed {
		n := u.Node
		if !state.Available(n) {
			continue
		}
		cpuFree += n.CPUTotal - n.CPUAlloc
		cpuTotal += n.CPUTotal
		gpuFree += u.Free
		gpuTotal += n.GPUTotal
	}
	parts := []string{plural(len(v.listed), "node"), fmt.Sprintf("%d/%d CPUs free", cpuFree, cpuTotal)}
	if gpuTotal > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d GPUs free", gpuFree, gpuTotal))
	}
	line := th.Muted.Render(strings.Join(parts, " "+th.Sym.Separator+" "))
	var view []string
	switch v.kind {
	case "gpu":
		view = append(view, "GPU nodes only")
	case "cpu":
		view = append(view, "CPU-only nodes")
	}
	if !v.filter.Empty() {
		view = append(view, "filter: "+v.filter.Raw)
	}
	if v.sortBy != "name" || v.sortDesc {
		s := "sorted by " + v.sortBy
		if v.sortDesc {
			s += " (reversed)"
		}
		view = append(view, s)
	}
	if !v.grouped {
		view = append(view, "not grouped")
	}
	if len(view) > 0 {
		line += sep + th.Info.Render(strings.Join(view, " "+th.Sym.Separator+" "))
	}
	return line
}

// reservationLine is the next reservation (usually maintenance) in one
// line, so a coming outage is not missed; the detailed layout has a panel.
func (v *Nodes) reservationLine(ctx *Context) string {
	if len(v.res) == 0 {
		return ""
	}
	th := ctx.Theme
	r := v.res[0]
	when := "starts in " + units.FormatShort(r.Start.Sub(ctx.Now))
	if !r.Start.After(ctx.Now) {
		when = "active"
	}
	nodes := plural(len(r.Nodes), "node")
	if len(r.Nodes) > 0 && len(r.Nodes) == len(ctx.Store.Nodes.Data) {
		nodes = "all nodes"
	}
	text := fmt.Sprintf("%s %s %s %s %s", r.Name, th.Sym.Separator, when, th.Sym.Separator, nodes)
	if !r.End.IsZero() && !r.Start.IsZero() {
		text += " " + th.Sym.Separator + " " + units.FormatLimit(r.End.Sub(r.Start))
	}
	if extra := len(v.res) - 1; extra > 0 {
		text += fmt.Sprintf(" (+%d more)", extra)
	}
	if r.IsMaintenance() {
		return th.Warn.Render(th.Sym.Warn + " reservation " + text)
	}
	return th.Muted.Render("reservation " + text)
}

// groupRow is a partition's heading. It is a row like the nodes under it:
// its numbers are the totals of those nodes, in the same columns and the same
// free/total form, so the eye can run down a column across the groups.
func (v *Nodes) groupRow(ctx *Context, s state.PartSummary, members []state.NodeUsage, folded bool) components.Row {
	th := ctx.Theme
	p := s.Partition
	marker := th.Sym.Expanded
	if folded {
		marker = th.Sym.Collapsed
	}
	name := p.Name
	if p.Default {
		name += "*"
	}

	var cpuFree, cpuTotal, gpuFree, gpuTotal, migFree, migTotal, untracked int
	var memFree, memTotal int64
	var types []string
	for _, m := range members {
		n := m.Node
		if !state.Available(n) {
			continue // a node that cannot take jobs adds no capacity, as in the status line
		}
		cpuFree += n.CPUTotal - n.CPUAlloc
		cpuTotal += n.CPUTotal
		gpuFree += m.Free
		gpuTotal += n.GPUTotal
		migFree += m.MIGFree
		migTotal += n.MIGTotal
		if n.MemTotalMB > 0 {
			memTotal += n.MemTotalMB
			if n.MemTracked() {
				memFree += n.MemTotalMB - n.MemAllocMB
			} else {
				untracked++
			}
		}
		if n.GPUTotal > 0 {
			if t := gpuTypeLabel(ctx, n); t != "" && !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
	}
	cell := func(free, total int) string {
		if total == 0 {
			return ""
		}
		return th.Bold.Render(fmt.Sprintf("%d/%d", free, total))
	}
	mem := ""
	switch {
	case memTotal == 0:
	case untracked > 0:
		mem = th.Faint.Render("-/") + th.Bold.Render(units.FormatMB(float64(memTotal)))
	default:
		mem = th.Bold.Render(units.FormatMB(float64(memFree)) + "/" + units.FormatMB(float64(memTotal)))
	}
	gpus, gtype := cell(gpuFree, gpuTotal), ""
	switch {
	case gpuTotal > 0:
		gtype = "mixed"
		if len(types) == 1 {
			gtype = types[0]
		}
		if migTotal > 0 {
			gtype += fmt.Sprintf(" +%d/%d MIG", migFree, migTotal)
		}
	case migTotal > 0:
		gpus, gtype = cell(migFree, migTotal), "MIG"
	}

	state := th.Muted.Render(strings.ToLower(p.State))
	if p.State != "UP" {
		state = th.Warn.Render(strings.ToLower(p.State))
	}
	var note []string
	if s.PendingKnown && s.Pending > 0 {
		note = append(note, fmt.Sprintf("%d pending", s.Pending))
	}
	if p.MaxTime != nil {
		note = append(note, "limit "+units.FormatLimit(*p.MaxTime))
	}
	return components.Row{ID: "part:" + p.Name, Heading: true, Cells: map[string]string{
		"node":  th.Bold.Render(marker+" "+name) + th.Faint.Render(" ("+strconv.Itoa(len(members))+")"),
		"state": state, "cpu": cell(cpuFree, cpuTotal), "mem": mem, "gpus": gpus, "type": th.Bold.Render(gtype),
		"jobs": th.Muted.Render(strings.Join(note, " "+th.Sym.Separator+" ")),
	}}
}

func (v *Nodes) nodeRow(ctx *Context, u state.NodeUsage) components.Row {
	th := ctx.Theme
	n := u.Node
	stateLabel := state.NodeStateLabel(n)
	stateCell := stateLabel
	switch {
	case !state.Available(n):
		stateCell = th.Warn.Render(stateLabel)
	case n.State == "IDLE":
		stateCell = th.OK.Render(stateLabel)
	}
	cpu := ""
	if n.CPUTotal > 0 {
		cpu = fmt.Sprintf("%d/%d", n.CPUTotal-n.CPUAlloc, n.CPUTotal)
	}
	load := fmt.Sprintf("%.1f", n.CPULoad)
	switch state.LoadLevel(n) {
	case state.LoadOver:
		load = th.Crit.Render(load)
	case state.LoadHigh:
		load = th.Warn.Render(load)
	}
	gpus, gtype := "", ""
	switch {
	case n.GPUTotal > 0:
		gpus = fmt.Sprintf("%d/%d", u.Free, n.GPUTotal)
		gtype = gpuTypeLabel(ctx, n)
		if n.MIGTotal > 0 {
			gtype += fmt.Sprintf(" +%d/%d MIG", u.MIGFree, n.MIGTotal)
		}
	case n.MIGTotal > 0:
		gpus = fmt.Sprintf("%d/%d", u.MIGFree, n.MIGTotal)
		gtype = "MIG"
	}
	jobs := ""
	switch {
	case len(u.Jobs) == 0 && n.CPUAlloc > 0 && state.Available(n):
		jobs = th.Muted.Render("busy, jobs not visible")
	case !state.Available(n) && n.Reason != "":
		jobs = th.Muted.Render("\"" + state.ShortReason(n.Reason) + "\"")
	case len(u.Jobs) > 0:
		jobs = jobOwners(ctx, u.Jobs)
	}
	return components.Row{ID: n.Name, Muted: !state.Available(n), Cells: map[string]string{
		"node": n.Name, "state": stateCell, "cpu": cpu, "load": load, "mem": nodeMemCell(ctx, n),
		"gpus": gpus, "type": gtype, "jobs": jobs, "freeby": freeIn(ctx, u),
	}}
}

// freeIn is when a full node frees up: its first GPU for GPU nodes, else
// its first CPUs, from the end times of its running jobs.
func freeIn(ctx *Context, u state.NodeUsage) string {
	n := u.Node
	var at model.RunningJob
	switch {
	case !state.Available(n) || len(u.Jobs) == 0:
		return ""
	case n.HasGPUs():
		if u.Free > 0 || (n.GPUTotal == 0 && u.MIGFree > 0) || u.FreeBy.IsZero() {
			return ""
		}
		at.EndTime = u.FreeBy
	case n.CPUAlloc < n.CPUTotal:
		return ""
	default:
		at = u.Jobs[0] // soonest end first
	}
	if at.EndTime.IsZero() {
		return ""
	}
	return ctx.Theme.Muted.Render(units.FormatShort(max(at.EndTime.Sub(ctx.Now), 0)))
}

// jobOwners renders "you 1 · carol 2" from the running jobs on a node.
func jobOwners(ctx *Context, jobs []model.RunningJob) string {
	th := ctx.Theme
	count := map[string]int{}
	for _, j := range jobs {
		u := j.User
		if u == ctx.Store.User {
			u = "you"
		}
		count[u]++
	}
	names := make([]string, 0, len(count))
	for u := range count {
		names = append(names, u)
	}
	slices.SortFunc(names, func(a, b string) int {
		if (a == "you") != (b == "you") {
			if a == "you" {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(count[b], count[a]), cmp.Compare(a, b))
	})
	parts := make([]string, 0, len(names))
	for _, u := range names {
		label := fmt.Sprintf("%s %d", u, count[u])
		if u == "you" {
			label = th.Accent.Render(label)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, th.Faint.Render(" "+th.Sym.Separator+" "))
}

// partitionsPanel lists each partition: state, time limit, node states,
// free CPUs, free GPUs by type and pending jobs.
func partitionsPanel(ctx *Context, parts []state.PartSummary, w, h int) string {
	th := ctx.Theme
	var lines []string
	nameW := 4
	for _, s := range parts {
		nameW = max(nameW, layout.Width(s.Partition.Name)+1)
	}
	for _, s := range parts {
		p := s.Partition
		name := p.Name
		if p.Default {
			name += "*"
		}
		state := strings.ToLower(p.State)
		if p.State != "UP" {
			state = th.Warn.Render(state)
		}
		limit := th.Pick("∞", "inf")
		if p.MaxTime != nil {
			limit = units.FormatLimit(*p.MaxTime)
		}
		nodes := fmt.Sprintf("%d idle %d mix %d alloc", s.Idle, s.Mixed, s.Alloc)
		if s.Down > 0 {
			nodes += " " + th.Warn.Render(strconv.Itoa(s.Down)+" down")
		}
		res := fmt.Sprintf("CPUs %d free", s.CPUFree)
		var gpus []string
		for _, t := range s.GPUTypes {
			gpus = append(gpus, fmt.Sprintf("%d/%d %s", t.Free, t.Total, units.GPUDisplayName(t.Type, ctx.Config.GPUNames)))
		}
		if s.MIGTotal > 0 {
			gpus = append(gpus, fmt.Sprintf("%d/%d MIG", s.MIGFree, s.MIGTotal))
		}
		if len(gpus) > 0 {
			res += th.Faint.Render(" "+th.Sym.Separator+" ") + "GPUs " + strings.Join(gpus, ", ") + " free"
		}
		pd := th.Faint.Render("PD ?")
		if s.PendingKnown {
			pd = "PD " + strconv.Itoa(s.Pending)
		}
		lines = append(lines, layout.Pad(name, nameW, false, "")+" "+layout.Pad(state, 5, false, "")+" "+
			layout.Pad(limit, 5, false, "")+" "+layout.Pad(pd, 6, false, "")+" "+res+th.Faint.Render(" "+th.Sym.Separator+" ")+nodes)
	}
	if len(lines) == 0 {
		lines = append(lines, th.Muted.Render("loading"+th.Sym.Ellipsis))
		if ctx.Store.Partitions.Has {
			lines = []string{th.Muted.Render("No partitions.")}
		}
	}
	return components.PaddedPanel(th, "Partitions", strings.Join(lines, "\n"), w, h, false)
}

func reservationsPanel(ctx *Context, res []model.Reservation, w, h int) string {
	th := ctx.Theme
	var lines []string
	for _, r := range res {
		when := "in " + units.FormatShort(r.Start.Sub(ctx.Now))
		if !r.Start.After(ctx.Now) {
			when = th.Warn.Render("active")
		}
		nodes := strconv.Itoa(len(r.Nodes)) + " nodes"
		if len(r.Nodes) == 1 {
			nodes = r.Nodes[0]
		}
		if len(r.Nodes) > 0 && len(r.Nodes) == len(ctx.Store.Nodes.Data) {
			nodes = "all"
		}
		name := r.Name
		if r.IsMaintenance() {
			name = th.Warn.Render(name)
		}
		dur := ""
		if !r.End.IsZero() && !r.Start.IsZero() {
			dur = units.FormatLimit(r.End.Sub(r.Start))
		}
		lines = append(lines, name+"  "+when+"  "+nodes+"  "+th.Muted.Render(dur))
	}
	return components.PaddedPanel(th, "Reservations", strings.Join(lines, "\n"), w, h, false)
}
