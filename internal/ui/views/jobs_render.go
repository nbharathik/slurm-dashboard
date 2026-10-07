package views

import (
	"cmp"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// Render implements View.
func (v *Jobs) Render(ctx *Context, w, h int) string {
	th := ctx.Theme
	var foot string
	if v.input != nil {
		foot = v.input.View(th, w)
		h--
	}
	var body string
	j, hasJob := v.cursorJob()
	if v.detail && v.detailID != "" {
		if dj, ok := v.byID[v.detailID]; ok {
			j, hasJob = dj, true
		}
	}
	switch {
	case !v.detail || !hasJob:
		v.table.Focused = true
		body = v.tablePanel(ctx, w, h)
	default:
		body = detailLayout(w, h, func(w, h int) string { return v.tablePanel(ctx, w, h) }, func(w, h int) string { return v.detailPanel(ctx, j, w, h) })
	}
	if v.menu != nil {
		body = layout.Overlay(body, v.menu.render(ctx), w, h)
	}
	if foot != "" {
		body += "\n" + foot
	}
	return body
}

// tablePanel is the status line and the table (there is no box around
// it; the detail panel keeps its own).
func (v *Jobs) tablePanel(ctx *Context, w, h int) string {
	th := ctx.Theme
	detailed := ctx.Config.Detailed()
	var head []string
	if v.queue {
		st := ctx.Store.AllJobs
		switch {
		case st.Err != nil && !st.Has:
			v.table.Empty = layout.FirstLine(st.Err.Error())
		case !st.Has:
			v.table.Empty = "loading" + th.Sym.Ellipsis
		default:
			v.table.Empty = "No jobs match."
		}
	}
	head = append(head, v.statusLine(ctx))
	if v.queue && detailed {
		// The extra lines: jobs per partition, who runs most, why jobs
		// wait, and the one-click scope chips.
		head = append(head, v.summaryLines(ctx, max((h-3)/3, 1))...)
	} else if v.queue && ctx.Store.PrivateJobs {
		head = append(head, th.Warn.Render("Other users' jobs are hidden by site policy (PrivateData=jobs); only yours are listed."))
	}
	for i, l := range head {
		head[i] = layout.Truncate(l, w, th.Sym.Ellipsis)
	}
	body := v.table.Render(th, ctx.Zones, w, h-len(head))
	return strings.Join(append(head, body), "\n")
}

// statusLine counts the jobs and names each way the view differs from the
// plain one: what is shown, grouping, sort and selection.
func (v *Jobs) statusLine(ctx *Context) string {
	th := ctx.Theme
	s := v.summary
	parts := []string{plural(s.total, "job")}
	if s.running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", s.running))
	}
	if s.pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", s.pending))
	}
	if v.queue && len(s.reasons) > 0 && !ctx.Config.Detailed() {
		var why []string
		for _, r := range s.reasons[:min(len(s.reasons), 2)] {
			why = append(why, fmt.Sprintf("%s %d", cmp.Or(r.reason, "None"), r.n))
		}
		parts = append(parts, "waiting on "+strings.Join(why, ", "))
	}
	sep := " " + th.Sym.Separator + " "
	line := th.Muted.Render(strings.Join(parts, sep))
	var view []string
	if v.queue && v.groupBy != "state" {
		view = append(view, map[string]string{"none": "not grouped", "user": "grouped by user", "partition": "grouped by partition"}[v.groupBy])
	}
	if !v.filter.Empty() {
		if name := v.scopeLabel(ctx); v.queue && name != "" {
			view = append(view, "showing "+name)
		} else {
			view = append(view, "filter: "+v.filter.Raw)
		}
	}
	if v.sortCol != "" {
		sort := "sorted by " + v.sortCol
		if v.sortDesc {
			sort += " (reversed)"
		}
		view = append(view, sort)
	}
	if n := len(v.table.Selected); n > 0 {
		view = append(view, fmt.Sprintf("%d selected", n))
	}
	if len(view) > 0 {
		line += th.Muted.Render(sep) + th.Info.Render(strings.Join(view, sep))
	}
	return line
}

// joinH puts two blocks side by side; the left one is padded (or cut) to aw
// cells so the right one starts at the same column on every line.
func joinH(a string, aw int, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for len(al) < len(bl) {
		al = append(al, "")
	}
	out := make([]string, len(al))
	for i := range al {
		r := ""
		if i < len(bl) {
			r = bl[i]
		}
		out[i] = layout.Pad(al[i], aw, false, "") + r
	}
	return strings.Join(out, "\n")
}

// detailPanel draws the job card: usage for running jobs, the why-pending
// card for pending ones, logs, dependency, buttons and all fields.
func (v *Jobs) detailPanel(ctx *Context, j model.Job, w, h int) string {
	th := ctx.Theme
	if !j.OwnedBy(ctx.Store.User) {
		return v.publicJobPanel(ctx, j, w, h)
	}
	inner := w - 4
	d := v.detailFor(ctx, j)
	stat := v.statFor(ctx, j)
	var b []string
	add := func(s ...string) { b = append(b, s...) }
	row := func(label, value string) { add(th.Muted.Render(layout.Pad(label, 5, false, "")) + value) }

	switch j.State {
	case model.StatePending:
		est := ""
		if !j.StartTime.IsZero() {
			est = friendlyTime(ctx.Now, j.StartTime)
		}
		card := insights.WhyCard(j, est)
		style := th.Warn
		if card.Pending.Never {
			style = th.Crit
		}
		add(style.Render(th.StateIcon(themeKind(j))+" "+card.Headline), "")
		for _, l := range card.Lines {
			add(layout.Wrap(l, inner)...)
		}
		if pf, ok := insights.PriorityFor(ctx.Store.Priorities.Data, j); ok {
			add(layout.Wrap(th.Muted.Render("Priority ")+insights.PriorityLine(pf), inner)...)
		}
		add("", th.Muted.Render("Requested: ")+requested(j))
		if j.Dependency != "" {
			row("Dep", j.Dependency)
		}
	default:
		where := ""
		if len(j.NodeList) > 0 {
			where = " on " + compactNodes(j.NodeList)
		}
		limit := "no limit"
		if j.TimeLimit != nil {
			limit = units.FormatLimit(*j.TimeLimit)
		}
		add(th.StateLabel(j.State, j.Reason, false)+where+th.Muted.Render(" "+th.Sym.Separator+" "+"elapsed "+units.FormatShort(j.TimeUsed)+", limit "+limit), "")
		if j.TimeLimit != nil && *j.TimeLimit > 0 {
			frac := j.TimeUsed.Seconds() / j.TimeLimit.Seconds()
			left := ""
			if j.TimeLeft != nil {
				left = th.Muted.Render("  " + units.FormatShort(*j.TimeLeft) + " left")
			}
			row("Time", components.Gauge(th, frac, gaugeWidth(inner))+" "+components.Percent(th, frac)+left)
		}
		mem := j.MemPerNodeMB * int64(max(j.Nodes, 1))
		switch {
		case stat != nil && mem > 0:
			frac := float64(stat.MaxRSSMB) / float64(mem)
			row("Mem", units.FormatMB(float64(stat.MaxRSSMB))+" of "+units.FormatMB(float64(mem))+"  "+components.Gauge(th, frac, gaugeWidth(inner)/2)+" "+components.Percent(th, frac))
		case stat != nil:
			row("Mem", units.FormatMB(float64(stat.MaxRSSMB))+th.Muted.Render(" peak so far"))
		case j.State == model.StateRunning:
			row("Mem", th.Muted.Render("sampling "+th.Sym.Ellipsis))
		}
		if stat != nil && j.TimeUsed > 0 && j.CPUs > 0 {
			cores := stat.TotalCPU.Seconds() / j.TimeUsed.Seconds()
			row("CPU", fmt.Sprintf("%.1f of %d cores avg (%s)", cores, j.CPUs, components.Percent(th, cores/float64(j.CPUs))))
		}
		if j.GPUs > 0 {
			row("GPU", gpuCount(j)+th.Muted.Render("  press g to sample"))
		}
	}

	if d != nil {
		out, errp := LogPath(d, false), LogPath(d, true)
		if out != "" {
			row("Out", tildify(out)+" "+ctx.Mark("jobs:btn:out", th.Key.Render("[l]")))
		}
		if errp != "" && errp != out {
			row("Err", tildify(errp)+" "+ctx.Mark("jobs:btn:err", th.Key.Render("[e]")))
		}
		if d.WorkDir != "" {
			row("Dir", tildify(d.WorkDir))
		}
	} else if ctx.Store.Detail.Err != nil {
		add(th.Warn.Render("details unavailable: " + layout.FirstLine(ctx.Store.Detail.Err.Error())))
	} else {
		add(th.Muted.Render("loading details" + th.Sym.Ellipsis))
	}

	if j.OwnedBy(ctx.Store.User) {
		var btns []string
		btn := func(id, label string, danger bool) {
			btns = append(btns, ctx.Mark("jobs:btn:"+id, components.Button(th, label, false, danger)))
		}
		switch j.State {
		case model.StateRunning:
			btn("cancel", "Cancel", true)
			btn("requeue", "Requeue", false)
			btn("shell", "Shell", false)
		case model.StatePending:
			btn("cancel", "Cancel", true)
			if strings.HasPrefix(j.Reason, "JobHeld") {
				btn("release", "Release", false)
			} else {
				btn("hold", "Hold", false)
			}
		}
		btn("script", "Script", false)
		add("", strings.Join(btns, " "))
	}

	if d != nil {
		marker := th.Sym.Collapsed
		if v.allFields {
			marker = th.Sym.Expanded
		}
		add("", ctx.Mark("jobs:btn:fields", th.Muted.Render(marker+" All fields (i)")))
		if v.allFields {
			for _, k := range d.RawOrder {
				add(th.Faint.Render(k+"=") + d.Raw[k])
			}
		}
	}
	title := j.ID.Raw + " " + j.Name
	content := strings.Join(b, "\n")
	return v.pane.render(ctx, j.ID.Raw, title, "jobs:btn:close", content, w, h)
}

func themeKind(j model.Job) themeKindT { return themeKindT(kindOf(j)) }

func gaugeWidth(inner int) int { return min(max(inner-20, 6), 30) }

// requested summarises a pending job's request: "1× H200, 8 CPUs, 32G, 04:00:00".
func requested(j model.Job) string {
	var parts []string
	if j.GPUs > 0 {
		parts = append(parts, gpuCount(j))
	}
	parts = append(parts, fmt.Sprintf("%d CPUs", j.CPUs))
	if j.MemPerNodeMB > 0 {
		parts = append(parts, units.FormatMB(float64(j.MemPerNodeMB)))
	}
	if j.TimeLimit != nil {
		parts = append(parts, units.FormatLimit(*j.TimeLimit))
	}
	if j.Nodes > 1 {
		parts = append(parts, fmt.Sprintf("%d nodes", j.Nodes))
	}
	return strings.Join(parts, ", ")
}

// gpuCount renders "1 GPU", "2 GPUs" or "2× H100".
func gpuCount(j model.Job) string {
	if j.GPUType != "" {
		return fmt.Sprintf("%d× %s", j.GPUs, units.GPUDisplayNames(j.GPUType, nil))
	}
	if j.GPUs == 1 {
		return "1 GPU"
	}
	return fmt.Sprintf("%d GPUs", j.GPUs)
}

// compactNodes shortens long node lists: "gpu01 +3".
func compactNodes(nodes []string) string {
	if len(nodes) <= 2 {
		return strings.Join(nodes, ",")
	}
	return fmt.Sprintf("%s +%d", nodes[0], len(nodes)-1)
}

// friendlyTime renders "today 14:05", "tomorrow 09:00" or "Sep 30 08:00".
func friendlyTime(now, t time.Time) string {
	t, now = t.Local(), now.Local()
	day := func(x time.Time) time.Time { y, m, d := x.Date(); return time.Date(y, m, d, 0, 0, 0, 0, x.Location()) }
	switch day(t).Sub(day(now)) / (24 * time.Hour) {
	case 0:
		return "today " + t.Format("15:04")
	case 1:
		return "tomorrow " + t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

// LogPath expands the StdOut/StdErr pattern of a job.
func LogPath(d *model.JobDetail, stderr bool) string { return slurm.LogPath(d, stderr) }

// tildify shortens the home directory to "~".
func tildify(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "/" && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

func (v *Jobs) publicJobPanel(ctx *Context, j model.Job, w, h int) string {
	th := ctx.Theme
	limit := "unlimited"
	if j.TimeLimit != nil {
		limit = units.FormatLimit(*j.TimeLimit)
	}
	lines := []string{th.StateLabel(j.State, j.Reason, false), "User: " + j.User, "Partition: " + j.Partition, "Resources: " + requested(j), "Elapsed: " + units.FormatShort(j.TimeUsed) + "  Limit: " + limit}
	if len(j.NodeList) > 0 {
		lines = append(lines, "Nodes: "+strings.Join(j.NodeList, ","))
	}
	if !j.EndTime.IsZero() {
		lines = append(lines, "Estimated end: "+friendlyTime(ctx.Now, j.EndTime))
	}
	if j.State == model.StatePending {
		lines = append(lines, "Waiting: "+j.Reason)
	}
	lines = append(lines, "", th.Muted.Render("Public queue snapshot. Private features are available only for your own jobs."))
	var body []string
	for _, l := range lines {
		body = append(body, layout.Wrap(l, max(w-4, 1))...)
	}
	return v.pane.render(ctx, j.ID.Raw, j.ID.Raw+" "+j.Name, "jobs:btn:close", strings.Join(body, "\n"), w, h)
}
