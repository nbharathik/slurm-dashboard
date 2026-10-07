package views

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// storageColumns are the columns of the Storage table; the detailed layout
// adds the filesystem, grace period, backend and note (storageDetailed).
var storageColumns = []layout.Column{
	{ID: "label", Title: "LOCATION", Priority: 1, Min: 5, Max: 16},
	{ID: "scope", Title: "SCOPE", Priority: 1, Min: 6, Max: 6},
	{ID: "blocks", Title: "SPACE USED", Priority: 1, Min: 12, Max: 40},
	{ID: "files", Title: "FILES", Priority: 3, Min: 7, Max: 16},
	{ID: "trend", Title: "CHECK / TREND", Priority: 2, Min: 10, Max: 26},
	{ID: "path", Title: "PATH", Priority: 2, Min: 6, Max: 40, Flex: true},
	{ID: "fs", Title: "FS", Priority: 4, Min: 3, Max: 8},
	{ID: "grace", Title: "GRACE", Priority: 4, Min: 5, Max: 10},
	{ID: "backend", Title: "BACKEND", Priority: 5, Min: 5, Max: 8},
	{ID: "note", Title: "NOTE", Priority: 5, Min: 6, Max: 40},
}

var storageDetailed = []string{"fs", "grace", "backend", "note"}

// Storage is tab 6: quotas and usage per location.
type Storage struct {
	table  components.Table
	quotas map[string]model.Quota
	detail string
	pane   detailPane
}

// NewStorage builds the Storage tab.
func NewStorage(*Context) *Storage {
	return &Storage{table: components.Table{Name: "storage", Focused: true}}
}

// Name implements View.
func (v *Storage) Name() string { return "storage" }

// Title implements View.
func (v *Storage) Title() string { return "Storage" }

// Source implements View.
func (v *Storage) Source() string { return "storage" }

// Badge implements View.
func (v *Storage) Badge(*Context) string { return "" }

// Capturing implements View.
func (v *Storage) Capturing() bool { return false }

// Refresh implements View.
func (v *Storage) Refresh(ctx *Context) {
	st := ctx.Store.Storage
	v.quotas = make(map[string]model.Quota, 2*len(st.Data))
	v.table.Cols = nil
	for _, c := range storageColumns {
		if ctx.Config.Detailed() || !slices.Contains(storageDetailed, c.ID) {
			v.table.Cols = append(v.table.Cols, c)
		}
	}
	switch {
	case st.Err != nil && !st.Has:
		v.table.Empty = "Storage check failed: " + layout.FirstLine(st.Err.Error())
	case !st.Has:
		v.table.Empty = "Checking quotas" + ctx.Theme.Sym.Ellipsis
	default:
		v.table.Empty = "No storage locations found. Add [[storage]] entries to the config (see sdash config path)."
	}
	rows := make([]components.Row, 0, 2*len(st.Data))
	for _, q := range st.Data {
		id := q.Label + "\x00" + q.Path
		v.quotas[id] = q
		rows = append(rows, v.personalRow(ctx, id, q))
	}
	for _, q := range st.Data {
		id := "filesystem\x00" + q.Label + "\x00" + q.Path
		fs := filesystemQuota(q)
		v.quotas[id] = fs
		row := v.row(ctx, id, fs)
		row.Cells["scope"] = "Shared"
		rows = append(rows, row)
	}
	v.table.SetRows(rows)
	if _, ok := v.quotas[v.detail]; !ok {
		v.detail = ""
	}
}

func filesystemQuota(q model.Quota) model.Quota {
	if q.IsFilesystemTotal {
		return q
	}
	if q.Filesystem != nil {
		return *q.Filesystem
	}
	return model.Quota{Label: q.Label, Path: q.Path, FSType: q.FSType, Backend: "statfs", IsFilesystemTotal: true, Err: "filesystem usage unavailable"}
}

func (v *Storage) personalRow(ctx *Context, id string, q model.Quota) components.Row {
	row := v.row(ctx, id, q)
	row.Cells["scope"] = "Yours"
	if !q.IsFilesystemTotal {
		if ctx.Store.DiskUsage[q.Path].Running {
			row.Cells["trend"] = ctx.Theme.Info.Render("Analysing" + ctx.Theme.Sym.Ellipsis)
		}
		return row
	}
	row.Cells["files"], row.Cells["grace"], row.Cells["trend"], row.Cells["note"] = "-", "", "", "Directory usage; no personal quota reported"
	row.Cells["blocks"] = ctx.Mark("storage:analyse:"+id, ctx.Theme.Key.Render("Analyse (a)"))
	if u, ok := ctx.Store.DiskUsage[q.Path]; ok {
		switch {
		case u.Running:
			row.Cells["blocks"] = ctx.Theme.Info.Render("Analysing" + ctx.Theme.Sym.Ellipsis)
		case u.At.IsZero():
			row.Cells["blocks"] += ctx.Theme.Warn.Render(" retry")
		default:
			row.Cells["blocks"] = units.FormatBytes(u.Total) + " used"
			if u.Partial || u.Err != "" {
				row.Cells["blocks"] += ctx.Theme.Warn.Render(" (partial)")
			}
			row.Cells["trend"] = ctx.Theme.Faint.Render("scanned " + units.FormatShort(max(ctx.Now.Sub(u.At), 0)) + " ago")
		}
	}
	return row
}

func (v *Storage) row(ctx *Context, id string, q model.Quota) components.Row {
	q = displayQuota(q)
	th := ctx.Theme
	u := q.Usage()
	detailed := ctx.Config.Detailed()
	blocks := usageCell(ctx, u.BlocksPct, units.FormatBytes(q.UsedBytes), q.SoftBytes, q.HardBytes, units.FormatBytes, detailed)
	if q.Err != "" && q.At.IsZero() {
		blocks = th.Muted.Render("Unavailable")
	}
	count := func(n int64) string { return units.FormatCount(n) }
	files := ""
	if q.UsedFiles > 0 || q.HardFiles > 0 || q.SoftFiles > 0 {
		files = filesCell(ctx, u.FilesPct, count(q.UsedFiles), q.SoftFiles, q.HardFiles, count)
	}
	grace := q.Grace
	active := grace != "" && grace != "none" && grace != "-"
	if !detailed {
		// What the columns that are left out would have said, only when it
		// matters.
		switch {
		case q.IsFilesystemTotal && q.At.IsZero() && q.Err != "":
		case q.IsFilesystemTotal:
			if q.AvailabilityKnown {
				blocks += " " + units.FormatBytes(q.AvailableBytes) + " avail"
			}
		case active:
			blocks += th.Warn.Render("  grace " + grace)
		}
		if q.Err != "" {
			blocks += th.Warn.Render("  stale")
		}
	}
	note := q.Note
	if q.IsFilesystemTotal {
		note = th.Faint.Render("shared filesystem, no personal quota") + " " + note
	}
	if q.Err != "" {
		note = th.Warn.Render("stale: "+q.Err) + " " + note
	}
	if active {
		grace = th.Warn.Render(grace)
	} else {
		grace = ""
	}
	return components.Row{ID: id, Cells: map[string]string{
		"label": q.Label, "blocks": blocks, "files": files, "trend": trendCell(ctx, q), "path": tildify(q.Path), "fs": q.FSType,
		"grace": grace, "backend": q.Backend, "note": strings.TrimSpace(note),
	}}
}

func (v *Storage) analyse(ctx *Context) tea.Cmd {
	id := strings.TrimPrefix(v.table.CursorID(), "filesystem\x00")
	q, ok := v.quotas[id]
	if !ok {
		return nil
	}
	v.table.SetCursorID(id)
	v.detail = id
	_, cached := ctx.Store.DiskUsage[q.Path]
	return Emit(AnalyseMsg{Path: q.Path, FSType: q.FSType, Fresh: cached})
}

// displayQuota uses physical filesystem capacity without changing report fields.
func displayQuota(q model.Quota) model.Quota {
	if q.IsFilesystemTotal && q.FilesystemBytes > 0 {
		q.SoftBytes = 0
		q.HardBytes = q.FilesystemBytes
	}
	return q
}

func storageObservation(ctx *Context) (time.Time, error) {
	st := ctx.Store.Storage
	at, err := st.At, st.Err
	for _, q := range st.Data {
		if err == nil && q.Err != "" {
			err = fmt.Errorf("%s: %s", q.Label, q.Err)
		}
		if !q.At.IsZero() && (at.IsZero() || q.At.Before(at)) {
			at = q.At
		}
		if fs := q.Filesystem; fs != nil {
			if err == nil && fs.Err != "" {
				err = fmt.Errorf("%s filesystem: %s", q.Label, fs.Err)
			}
			if !fs.At.IsZero() && (at.IsZero() || fs.At.Before(at)) {
				at = fs.At
			}
		}
	}
	return at, err
}

func storageFreshness(ctx *Context) string {
	at, err := storageObservation(ctx)
	return freshness(ctx, "storage", at, ctx.Store.Storage.Has, err)
}

// trendCell draws the last 30 days of a location, with a warning when the
// line says it will fill soon.
func trendCell(ctx *Context, q model.Quota) string {
	th := ctx.Theme
	series := ctx.Store.Trends.Series(insights.KeyOf(q))
	if len(series) < 2 {
		return th.Faint.Render("collecting")
	}
	line := components.Sparkline(th, insights.Points(series, ctx.Now, 10))
	f := insights.Predict(series, ctx.Now)
	if !f.Filling || f.Days >= 365 {
		return line
	}
	style := th.Muted
	switch {
	case f.Days <= 7:
		style = th.Crit
	case f.Days <= 30:
		style = th.Warn
	}
	return line + " " + style.Render("full "+fillIn(f.Days, true))
}

// fillIn says how long until full in days, weeks or months. short drops the
// "about".
func fillIn(days float64, short bool) string {
	var n int
	var unit string
	switch {
	case days < 1:
		if short {
			return "<1d"
		}
		return "within a day"
	case days < 100:
		n, unit = int(math.Round(days)), "day"
	default:
		n, unit = int(math.Round(days/30)), "month"
	}
	if short {
		if unit == "day" {
			return fmt.Sprintf("~%dd", n)
		}
		return fmt.Sprintf("~%dmo", n)
	}
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("in about %d %s", n, unit)
}

// trendLines describe a location's history in the detail panel.
func trendLines(ctx *Context, q model.Quota, w int) []string {
	th := ctx.Theme
	series := ctx.Store.Trends.Series(insights.KeyOf(q))
	if len(series) < 2 {
		return []string{th.Muted.Render("Trend    ") + th.Faint.Render("collecting: sdash records use while it runs, at most once an hour")}
	}
	pts := insights.Points(series, ctx.Now, min(30, max(w-40, 10)))
	first, last := series[0], series[len(series)-1]
	head := fmt.Sprintf("%s  %s to %s over %s", components.Sparkline(th, pts), units.FormatBytes(first.Used), units.FormatBytes(last.Used),
		units.FormatShort(last.T.Sub(first.T)))
	out := []string{th.Muted.Render("Trend    ") + head}
	f := insights.Predict(series, ctx.Now)
	pad := strings.Repeat(" ", 9)
	switch {
	case !f.OK:
		out = append(out, pad+th.Faint.Render("not enough readings yet for a forecast (needs 4 over a day)"))
	case f.Flat:
		out = append(out, pad+"steady over the last 30 days")
	case f.PerDay < 0:
		out = append(out, pad+"shrinking about "+units.FormatBytes(int64(-f.PerDay))+" a day")
	case f.Filling && f.Days < 365:
		style := th.Muted
		if f.Days <= 30 {
			style = th.Warn
		}
		out = append(out, pad+style.Render("growing about "+units.FormatBytes(int64(f.PerDay))+" a day; full "+fillIn(f.Days, false)))
	case f.Filling:
		out = append(out, pad+"growing about "+units.FormatBytes(int64(f.PerDay))+" a day; not full within a year at this rate")
	default:
		out = append(out, pad+"growing about "+units.FormatBytes(int64(f.PerDay))+" a day (no limit to fill)")
	}
	return out
}

// usageCell renders "████░░ 41G/50G" against the soft limit; the detailed
// layout marks a hard limit that differs.
func usageCell(ctx *Context, pct int, used string, soft, hard int64, format func(int64) string, showHard bool) string {
	th := ctx.Theme
	limit := soft
	if limit <= 0 {
		limit = hard
	}
	if limit <= 0 {
		if used == "0" || used == "0B" {
			return components.Dash(th)
		}
		return used + th.Faint.Render(" no limit")
	}
	warn, crit := insights.Thresholds(ctx.Config.StorageWarn, ctx.Config.StorageCrit)
	s := components.QuotaGauge(th, pct, warn, crit, 8) + " " + used + "/" + format(limit)
	if showHard && soft > 0 && hard > soft {
		s += th.Faint.Render(" (hard " + format(hard) + ")")
	}
	return s
}

// filesCell is "212k/1M", amber or red as the file count nears its limit.
func filesCell(ctx *Context, pct int, used string, soft, hard int64, format func(int64) string) string {
	th := ctx.Theme
	limit := soft
	if limit <= 0 {
		limit = hard
	}
	if limit <= 0 {
		return used
	}
	warn, crit := insights.Thresholds(ctx.Config.StorageWarn, ctx.Config.StorageCrit)
	s := used + "/" + format(limit)
	switch {
	case pct >= crit:
		return th.Crit.Render(s)
	case pct >= warn:
		return th.Warn.Render(s)
	}
	return s
}

// Update implements View.
func (v *Storage) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	if v.detail != "" && v.pane.update(ctx, msg) {
		return nil
	}
	k := ctx.Keys
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		q, ok := v.quotas[v.table.CursorID()]
		switch {
		case key.Matches(msg, k.Back) && ok && ctx.Store.DiskUsage[q.Path].Running:
			return Emit(CancelAnalyseMsg{})
		case key.Matches(msg, k.Back) && v.detail != "":
			v.detail = ""
		case navigate(&v.table, k, msg):
			if v.detail != "" {
				v.detail = v.table.CursorID()
			}
		case key.Matches(msg, k.Open) && ok:
			if v.detail == v.table.CursorID() {
				v.detail = ""
			} else {
				v.detail = v.table.CursorID()
			}
		case key.Matches(msg, k.RefreshAll):
			return Emit(RefreshMsg{Sources: []string{"storage"}})
		case key.Matches(msg, k.Analyse) && ok:
			return v.analyse(ctx)
		case key.Matches(msg, k.Copy) && ok:
			return Emit(CopyMsg{Text: q.Path, What: "path " + q.Path})
		}
	case tea.MouseWheelMsg:
		navigate(&v.table, ctx.Keys, msg)
		if v.detail != "" {
			v.detail = v.table.CursorID()
		}
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return nil
		}
		if ctx.InZone("storage:close", msg) {
			v.detail = ""
			return nil
		}
		if ctx.InZone("storage:analyse", msg) {
			v.table.SetCursorID(v.detail)
			return v.analyse(ctx)
		}
		for _, r := range v.table.Rows {
			if ctx.InZone("storage:analyse:"+r.ID, msg) {
				v.table.SetCursorID(r.ID)
				return v.analyse(ctx)
			}
			if ctx.InZone(v.table.RowZone(r.ID), msg) {
				if v.table.CursorID() == r.ID || v.detail != "" {
					v.detail = r.ID
				}
				v.table.SetCursorID(r.ID)
				return nil
			}
		}
	}
	return nil
}

// Hints implements View.
func (v *Storage) Hints(*Context) []key.Binding {
	if v.detail != "" {
		return []key.Binding{bind("esc", "close"), bind("a", "analyse yours")}
	}
	return []key.Binding{bind("enter", "details"), bind("R", "refresh storage"), bind("a", "analyse yours")}
}

// Render implements View.
func (v *Storage) Render(ctx *Context, w, h int) string {
	th := ctx.Theme
	table := func(w, h int) string {
		status := plural(len(ctx.Store.Storage.Data), "location")
		at, err := storageObservation(ctx)
		if err != nil {
			status = "storage: stale/error " + th.Sym.Separator + " " + status
		}
		if ctx.Store.Storage.Has && !at.IsZero() {
			status += " " + th.Sym.Separator + " checked " + shortTime(ctx.Now, at)
		}
		head := th.Muted.Render(layout.Truncate(status, w, th.Sym.Ellipsis))
		return head + "\n" + v.table.Render(th, ctx.Zones, w, h-1)
	}
	if v.detail == "" {
		return table(w, h)
	}
	return detailLayout(w, h, table, func(w, h int) string { return v.rawPanel(ctx, w, h) })
}

// duLines renders an analysis: the largest subdirectories with bars.
func duLines(ctx *Context, u model.DiskUsage, w int) []string {
	th := ctx.Theme
	switch {
	case u.Running:
		return []string{th.Info.Render("Analysing " + u.Path + th.Sym.Ellipsis + " (esc cancels; runs at the lowest priority)")}
	case u.Err != "" && len(u.Entries) == 0:
		return []string{th.Warn.Render("Analysis stopped: " + u.Err)}
	}
	took := ""
	if u.Took >= time.Second {
		took = " in " + units.FormatShort(u.Took)
	}
	head := fmt.Sprintf("Largest directories (%s total, analysed %s ago%s)", units.FormatBytes(u.Total),
		units.FormatShort(ctx.Now.Sub(u.At)), took)
	if u.Err != "" {
		head = fmt.Sprintf("Largest directories finished before the scan was %s (at least %s)", u.Err, units.FormatBytes(u.Total))
	}
	out := []string{th.Bold.Render(head)}
	// Names as wide as the longest (within reason), so the bars sit next
	// to them.
	nameW := 10
	for _, e := range u.Entries {
		nameW = max(nameW, ansi.StringWidth(strings.TrimPrefix(strings.TrimPrefix(e.Path, u.Path), "/")))
	}
	nameW = min(nameW, 40, max(w-20, 10))
	for i, e := range u.Entries {
		if i == 12 {
			out = append(out, th.Muted.Render(fmt.Sprintf("%s and %d more", th.Sym.Ellipsis, len(u.Entries)-12)))
			break
		}
		frac := 0.0
		if u.Total > 0 {
			frac = float64(e.Bytes) / float64(u.Total)
		}
		name := strings.TrimPrefix(strings.TrimPrefix(e.Path, u.Path), "/")
		out = append(out, layout.Pad(name, nameW, false, th.Sym.Ellipsis)+" "+layout.Pad(units.FormatBytes(e.Bytes), 7, true, "")+" "+components.Gauge(th, frac, 10))
	}
	switch {
	case u.Err != "":
		out = append(out, th.Warn.Render("Unfinished directories are missing; press a to scan again."))
	case u.Partial:
		out = append(out, th.Warn.Render("Some directories could not be read; sizes are a lower bound."))
	}
	return out
}

func (v *Storage) rawPanel(ctx *Context, w, h int) string {
	th := ctx.Theme
	q := v.quotas[v.detail]
	shared := strings.HasPrefix(v.detail, "filesystem\x00")
	var b []string
	b = append(b, th.Muted.Render("Path     ")+q.Path)
	if shared {
		display := displayQuota(q)
		b = append(b, th.Muted.Render("Scope    ")+"Shared filesystem containing this path")
		if q.At.IsZero() && q.Err != "" {
			b = append(b, th.Muted.Render("Used     ")+"Unavailable")
		} else {
			b = append(b, th.Muted.Render("Used     ")+units.FormatBytes(q.UsedBytes)+" / "+units.FormatBytes(display.HardBytes)+" shared filesystem")
		}
		if q.AvailabilityKnown {
			b = append(b, th.Muted.Render("Available")+" "+units.FormatBytes(q.AvailableBytes)+" reported by filesystem")
		}
	} else if q.IsFilesystemTotal {
		b = append(b, th.Muted.Render("Scope    ")+"Your directory", th.Muted.Render("Usage    ")+"Analyse your usage below; the scan covers this path only.")
	} else {
		b = append(b, th.Muted.Render("Scope    ")+"Your quota")
	}
	b = append(b, th.Muted.Render("Backend  ")+q.Backend+th.Faint.Render("  ("+q.FSType+")"))
	if q.Note != "" {
		b = append(b, th.Muted.Render("Note     ")+q.Note)
	}
	if q.Err != "" {
		b = append(b, th.Warn.Render("Last error: "+q.Err))
	}
	if shared || !q.IsFilesystemTotal {
		b = append(b, trendLines(ctx, q, w)...)
	}
	if u, ok := ctx.Store.DiskUsage[q.Path]; ok && !shared {
		b = append(b, "")
		b = append(b, duLines(ctx, u, w-4)...)
	}
	raw := strings.TrimRight(q.Raw, "\n")
	if raw != "" {
		b = append(b, "", th.Bold.Render("Backend output"))
		b = append(b, strings.Split(raw, "\n")...)
	}
	label := "Analyse your usage (a)"
	if _, ok := ctx.Store.DiskUsage[q.Path]; ok {
		label = "Analyse yours again (a)"
	}
	if shared {
		label = "Your usage (a)"
	}
	b = append(b, "", ctx.Mark("storage:analyse", components.Button(th, label, false, false)))
	scope := "Yours"
	if shared {
		scope = "Shared"
	}
	return v.pane.render(ctx, v.detail, q.Label+" / "+scope, "storage:close", strings.Join(b, "\n"), w, h)
}
