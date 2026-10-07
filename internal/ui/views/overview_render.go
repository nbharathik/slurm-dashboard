package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// Render implements View: Alerts (if any), Cluster, Your jobs and Storage in one column.
func (v *Overview) Render(ctx *Context, w, h int) string {
	alertsB := block{min: min(len(v.alerts), 2) + 2, want: min(len(v.alerts), 4) + 2, render: v.alertsPanel(ctx)}
	clusterB := block{min: 4, want: capacityHeight(ctx, v.parts, w-2) + 3, render: v.clusterPanel(ctx)}
	jobsB := block{min: 4, want: min(len(v.jobs.Rows), 10) + 3, render: v.jobsPanel(ctx)}
	storageB := block{min: 3, want: max(v.count(panelStorage), 1) + 2, render: v.storagePanel(ctx)}

	var blocks []block
	if len(v.alerts) > 0 {
		blocks = append(blocks, alertsB)
	}
	blocks = append(blocks, clusterB, jobsB, storageB)
	for i := 1; i < len(blocks); i++ {
		blocks[i] = spaced(blocks[i])
	}
	return stack(blocks, w, h, -1) // sections keep their size; spare room stays empty below
}

// spaced puts a blank line above a section when there is room for one.
func spaced(b block) block {
	want, render := b.want+1, b.render
	return block{min: b.min, want: want, render: func(w, h int) string {
		if h >= want {
			return strings.Repeat(" ", w) + "\n" + render(w, h-1)
		}
		return render(w, h)
	}}
}

// heading is a section's title line, accented when focused; a click focuses the section.
func (v *Overview) heading(ctx *Context, p, title, extra string, w int) string {
	th := ctx.Theme
	style := th.Bold
	if v.focus == p {
		style = th.Accent
	}
	line := style.Render(title)
	if extra != "" {
		line += strings.Repeat(" ", max(w-layout.Width(line)-layout.Width(extra), 2)) + extra
	}
	return ctx.Mark("ov:panel:"+p, layout.Pad(line, w, false, th.Sym.Ellipsis))
}

// section puts a heading, and a thin rule under it, above the body lines of
// a section.
func (v *Overview) section(ctx *Context, p, title, extra, body string, w, h int) string {
	return layout.FitLines(v.heading(ctx, p, title, extra, w)+"\n"+ctx.Theme.HRule(w)+"\n"+body, w, h)
}

// line is one selectable line of a section, with the cursor column in front
// as in the tables, so the text of every screen starts at the same column.
func (v *Overview) line(ctx *Context, p string, i int, s string, w int) string {
	return ctx.Mark(fmt.Sprintf("ov:%s:%d", p, i), v.lineBody(ctx, p, i, s, w))
}

func (v *Overview) lineBody(ctx *Context, p string, i int, s string, w int) string {
	th := ctx.Theme
	s = layout.Pad(s, w-2, false, th.Sym.Ellipsis)
	prefix := "  "
	if v.focus == p && v.cur[p] == i {
		prefix = th.Accent.Render(th.Sym.Cursor) + " "
		s = th.Selected.Render(ansi.Strip(s))
	}
	return prefix + s
}

// note is a line of plain text in a section (loading, an error, nothing).
func note(text string) string { return "  " + text }

func scrollStart(cur, rows, n int) int {
	if rows <= 0 || n <= rows {
		return 0
	}
	return min(max(cur-rows+1, 0), n-rows)
}

func (v *Overview) alertsPanel(ctx *Context) func(w, h int) string {
	return func(w, h int) string {
		th := ctx.Theme
		rows := h - 2
		var lines []string
		start := scrollStart(v.cur[panelAlerts], rows, len(v.alerts))
		for i := start; i < len(v.alerts) && len(lines) < rows; i++ {
			a := v.alerts[i]
			icon := map[model.Level]string{model.Crit: th.Crit.Render(th.Sym.Cross), model.Warn: th.Warn.Render(th.Sym.Warn), model.Info: th.Info.Render("i")}[a.Level]
			openBtn := ""
			if a.JobID != "" || a.Tab != "" {
				openBtn = ctx.Mark(fmt.Sprintf("ov:alerts:open:%d", i), th.Key.Render("[open]"))
			}
			prefix := "  "
			text := layout.Pad(icon+" "+a.Message, w-layout.Width(openBtn)-3-2, false, th.Sym.Ellipsis)
			if v.focus == panelAlerts && v.cur[panelAlerts] == i {
				prefix = th.Accent.Render(th.Sym.Cursor) + " "
				text = th.Selected.Render(ansi.Strip(text))
			}
			lines = append(lines, ctx.Mark(fmt.Sprintf("ov:alerts:%d", i), prefix+text)+" "+openBtn)
		}
		extra := ""
		if len(v.alerts) > rows {
			extra = th.Faint.Render(fmt.Sprintf("%d in all", len(v.alerts)))
		}
		return v.section(ctx, panelAlerts, "Needs attention", extra, strings.Join(lines, "\n"), w, h)
	}
}

// clusterPanel shows each partition's free CPUs and GPUs (by type, MIG
// slices apart), pending jobs and nodes that are down, in aligned columns.
func (v *Overview) clusterPanel(ctx *Context) func(w, h int) string {
	return func(w, h int) string {
		th := ctx.Theme
		rows := h - 3
		var lines []string
		switch st := ctx.Store; {
		case len(v.parts) == 0 && st.Partitions.Err != nil:
			lines = append(lines, note(th.Warn.Render(layout.FirstLine(st.Partitions.Err.Error()))))
		case len(v.parts) == 0:
			lines = append(lines, note(th.Muted.Render("loading"+th.Sym.Ellipsis)))
		}
		header, table := capacityLines(ctx, v.parts, w-2)
		start := min(v.cur[panelCluster], max(len(table)-1, 0))
		used := 0
		if len(table) > 0 {
			used = len(table[start])
		}
		for start > 0 && used+len(table[start-1]) <= rows {
			start--
			used += len(table[start])
		}
		for i := start; i < len(v.parts) && len(lines) < rows; i++ {
			var group []string
			for _, line := range table[i][:min(len(table[i]), rows-len(lines))] {
				group = append(group, v.lineBody(ctx, panelCluster, i, line, w))
			}
			marked := ctx.Mark(fmt.Sprintf("ov:%s:%d", panelCluster, i), strings.Join(group, "\n"))
			lines = append(lines, strings.Split(marked, "\n")...)
		}
		extra := clusterFreshness(ctx)
		return v.section(ctx, panelCluster, "Cluster", extra, "  "+header+"\n"+strings.Join(lines, "\n"), w, h)
	}
}

func (v *Overview) jobsPanel(ctx *Context) func(w, h int) string {
	return func(w, h int) string {
		th := ctx.Theme
		c := state.CountJobs(ctx.Store.MyJobs.Data)
		var parts []string
		if c.Running > 0 {
			parts = append(parts, th.OK.Render(fmt.Sprintf("%s %d running", th.StateIcon(theme.KindRunning), c.Running)))
		}
		if c.Pending > 0 {
			parts = append(parts, th.Warn.Render(fmt.Sprintf("%s %d pending", th.StateIcon(theme.KindPending), c.Pending)))
		}
		if v.failed > 0 {
			parts = append(parts, th.Crit.Render(fmt.Sprintf("%s %d failed today", th.StateIcon(theme.KindFailed), v.failed)))
		}
		v.jobs.Focused = v.focus == panelJobs
		var body string
		switch {
		case ctx.Store.MyJobs.Err != nil && !ctx.Store.MyJobs.Has:
			body = note(th.Crit.Render(layout.FirstLine(ctx.Store.MyJobs.Err.Error())))
		case !ctx.Store.MyJobs.Has:
			body = note(th.Muted.Render("loading" + th.Sym.Ellipsis))
		default:
			// The table's own header rule closes the heading, so the
			// section has none of its own.
			if sum := v.jobsSummary(ctx); sum != "" && ctx.Config.Detailed() && h >= 5 {
				body = note(layout.Truncate(sum, w-2, th.Sym.Ellipsis)) + "\n" + v.jobs.Render(th, ctx.Zones, w, h-2)
			} else {
				body = v.jobs.Render(th, ctx.Zones, w, h-1)
			}
		}
		head := v.heading(ctx, panelJobs, "Your jobs", freshness(ctx, "myjobs", ctx.Store.MyJobs.At, ctx.Store.MyJobs.Has, ctx.Store.MyJobs.Err)+"  "+strings.Join(parts, "  "), w)
		return layout.FitLines(head+"\n"+body, w, h)
	}
}

// jobsSummary (detailed layout) is "next to end: 813 eval-bench in 4m .
// waiting: Resources 1".
func (v *Overview) jobsSummary(ctx *Context) string {
	th := ctx.Theme
	var parts []string
	var next *model.Job
	waiting := map[string]int{}
	var order []string
	for i, j := range v.myJobs {
		switch j.State {
		case model.StateRunning:
			if !j.EndTime.IsZero() && (next == nil || j.EndTime.Before(next.EndTime)) {
				next = &v.myJobs[i]
			}
		case model.StatePending:
			if waiting[j.Reason] == 0 {
				order = append(order, j.Reason)
			}
			waiting[j.Reason]++
		}
	}
	if next != nil {
		parts = append(parts, th.Muted.Render("estimated next end: ")+next.ID.Raw+" "+next.Name+th.Muted.Render(" in "+units.FormatShort(max(next.EndTime.Sub(ctx.Now), 0))))
	}
	if len(order) > 0 {
		var w []string
		for _, r := range order {
			w = append(w, fmt.Sprintf("%s %d", r, waiting[r]))
		}
		parts = append(parts, th.Muted.Render("waiting: ")+strings.Join(w, ", "))
	}
	return strings.Join(parts, th.Faint.Render(" "+th.Sym.Separator+" "))
}

// storagePanel lists the storage locations, then the fairshare line.
func (v *Overview) storagePanel(ctx *Context) func(w, h int) string {
	return func(w, h int) string {
		th := ctx.Theme
		inner := w - 4
		st := ctx.Store.Storage
		var lines []string
		switch {
		case len(v.quotas) == 0 && st.Err != nil:
			lines = append(lines, note(th.Warn.Render(layout.FirstLine(st.Err.Error()))))
		case len(v.quotas) == 0 && !st.Has:
			lines = append(lines, note(th.Muted.Render("checking quotas"+th.Sym.Ellipsis)))
		case len(v.quotas) == 0:
			lines = append(lines, note(th.Muted.Render("No storage locations found; add [[storage]] entries to the config.")))
		}
		labelW := 9
		for _, q := range v.quotas {
			labelW = max(labelW, min(layout.Width(q.Label), 14))
		}
		rows := h - 2
		start := scrollStart(v.cur[panelStorage], rows, len(v.quotas))
		for i := start; i < len(v.quotas) && len(lines) < rows; i++ {
			lines = append(lines, v.line(ctx, panelStorage, i, quotaLine(ctx, v.quotas[i], labelW, inner), w))
		}
		if s := v.share; s != nil && len(lines) < rows { // storage first when space is short
			gw := min(max(inner-labelW-24, 5), 20)
			fs := layout.Pad("Fairshare", labelW, false, "") + "  " + fairGauge(th, s.FairShare, gw) + fmt.Sprintf("  %.2f", s.FairShare)
			if ctx.Config.Detailed() {
				fs += th.Muted.Render(fmt.Sprintf("  usage %s of %s share", pctText(s.EffectiveUsage), pctText(s.NormShares)))
			}
			interval := 10 * time.Second
			if ctx.Interval != nil {
				interval = ctx.Interval("fairshare")
			}
			if f := ctx.Store.Fairshare; f.Err != nil || !f.Has || (interval > 0 && ctx.Now.Sub(f.At) > 2*interval) {
				fs = "Fairshare " + freshness(ctx, "fairshare", f.At, f.Has, f.Err)
			}
			lines = append(lines, v.line(ctx, panelStorage, len(v.quotas), fs, w))
		}
		return v.section(ctx, panelStorage, "Storage", storageFreshness(ctx), strings.Join(lines, "\n"), w, h)
	}
}

// quotaLine renders "Home  ████████░░  41/50 GB".
func quotaLine(ctx *Context, q model.Quota, labelW, w int) string {
	th := ctx.Theme
	label := layout.Pad(q.Label, labelW, false, th.Sym.Ellipsis)
	q = displayQuota(q)
	u := q.Usage()
	warn, crit := insights.Thresholds(ctx.Config.StorageWarn, ctx.Config.StorageCrit)
	limit := q.SoftBytes
	if limit <= 0 {
		limit = q.HardBytes
	}
	gw := min(max(w-labelW-24, 5), 20)
	var usage string
	switch {
	case q.Err != "" && q.At.IsZero():
		return label + "  " + th.Warn.Render(q.Err)
	case limit > 0:
		usage = components.QuotaGauge(th, u.BlocksPct, warn, crit, gw) + "  " + units.FormatBytes(q.UsedBytes) + " used / " + units.FormatBytes(limit)
		if w < 55 {
			usage = components.QuotaGauge(th, u.BlocksPct, warn, crit, 5) + " " + units.FormatBytes(q.UsedBytes) + "/" + units.FormatBytes(limit)
		}
	case q.UsedBytes > 0:
		usage = strings.Repeat(" ", gw) + "  " + units.FormatBytes(q.UsedBytes) + th.Faint.Render(" no limit")
	default:
		usage = th.Faint.Render("no quota reported")
	}
	if u.FilesPct >= warn {
		style := th.Warn
		if u.FilesPct >= crit {
			style = th.Crit
		}
		usage += "  " + style.Render(fmt.Sprintf("files %d%%", u.FilesPct))
	}
	if q.IsFilesystemTotal {
		usage += th.Faint.Render(" shared")
		if q.AvailabilityKnown {
			usage += "  " + units.FormatBytes(q.AvailableBytes) + " available"
		}
	}
	if q.Err != "" {
		usage += th.Warn.Render("  stale")
	}
	if !q.IsFilesystemTotal && limit > 0 {
		usage += th.Faint.Render(" quota")
	}
	return label + "  " + usage
}

// fairGauge fills with the fairshare factor: high is good (green).
func fairGauge(th theme.Theme, f float64, w int) string {
	n := int(f*float64(w) + 0.5)
	n = min(max(n, 0), w)
	style := th.OK
	switch {
	case f < 0.2:
		style = th.Crit
	case f < 0.5:
		style = th.Warn
	}
	return style.Render(strings.Repeat(th.Sym.Filled, n)) + th.Faint.Render(strings.Repeat(th.Sym.Empty, w-n))
}

func pctText(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }
