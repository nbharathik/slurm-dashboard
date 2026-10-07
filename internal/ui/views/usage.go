package views

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// HistoryRanges are the selectable history windows in days.
var HistoryRanges = []int{1, 7, 30}

// historyColumns are the columns of the finished-jobs table; the detailed
// layout adds the exit code, the GPU count and the partition (see
// usageDetailed).
var historyColumns = []layout.Column{
	{ID: "id", Title: "ID", Priority: 1, Min: 5},
	{ID: "name", Title: "NAME", Priority: 1, Min: 8, Max: 32},
	{ID: "state", Title: "STATE", Priority: 1, Min: 5, Max: 16},
	{ID: "exit", Title: "EXIT", Priority: 3, Min: 4, Max: 6, Right: true},
	{ID: "elapsed", Title: "ELAPSED", Priority: 2, Min: 8, Max: 14},
	{ID: "cpu", Title: "CPU%", Priority: 2, Min: 4, Max: 5, Right: true},
	{ID: "mem", Title: "MEM%", Priority: 2, Min: 4, Max: 5, Right: true},
	{ID: "gpus", Title: "GPU", Priority: 3, Min: 3, Max: 4, Right: true},
	{ID: "waste", Title: "IDLE", Priority: 2, Min: 4, Max: 6, Right: true},
	{ID: "partition", Title: "PART", Priority: 4, Min: 4, Max: 12},
	{ID: "end", Title: "END", Priority: 3, Min: 5, Max: 12},
}

// usageDetailed are the columns only the detailed layout shows.
var usageDetailed = []string{"exit", "gpus", "partition"}

// Usage is tab 5: what your jobs used and wasted, over a summary of the
// range, with the finished jobs below.
type Usage struct {
	table    components.Table
	days     int
	filter   Filter
	input    *LineInput
	sortCol  string
	sortDesc bool
	jobs     []model.HistoryJob
	byID     map[string]model.HistoryJob
	detail   string
	pane     detailPane
	summary  historySummary
	sum      insights.Summary // of every finished job in the range, ignoring the filter
	acct     insights.Account
}

type historySummary struct {
	Jobs, Completed, Failed int
	GPUHours                float64
	MedianMem               float64 // -1 when unknown
}

// NewUsage builds the Usage tab.
func NewUsage(ctx *Context) *Usage {
	days := 7
	if d, err := strconv.Atoi(ctx.Prefs[PrefHistoryDays]); err == nil && slices.Contains(HistoryRanges, d) {
		days = d
	}
	return &Usage{days: days, table: components.Table{Name: "usage", Focused: true}}
}

// Name implements View.
func (v *Usage) Name() string { return "usage" }

// Title implements View.
func (v *Usage) Title() string { return "Usage" }

// Source implements View.
func (v *Usage) Source() string { return "history" }

// Sources are refreshed together by r on this tab: the jobs, the limits
// and the fairshare behind the summary.
func (v *Usage) Sources() []string { return []string{"history", "limits", "fairshare"} }

// Badge implements View.
func (v *Usage) Badge(*Context) string { return "" }

// Capturing implements View.
func (v *Usage) Capturing() bool { return v.input != nil }

// Days is the selected range.
func (v *Usage) Days() int { return v.days }

func rangeLabel(days int) string {
	if days == 1 {
		return "24 h"
	}
	return strconv.Itoa(days) + " days"
}

// SetRange selects a range in days (from /range).
func (v *Usage) SetRange(days int) tea.Cmd {
	if !slices.Contains(HistoryRanges, days) || days == v.days {
		return nil
	}
	v.days = days
	return tea.Batch(Emit(HistoryRangeMsg{Days: days}), Emit(PrefMsg{Key: PrefHistoryDays, Value: strconv.Itoa(days)}))
}

// SetFilter applies filter text.
func (v *Usage) SetFilter(s string) error {
	f, err := ParseFilter(s)
	if err != nil {
		return err
	}
	v.filter = f
	return nil
}

// SortColumns lists sortable column IDs.
func (v *Usage) SortColumns() []string {
	var out []string
	for _, c := range historyColumns {
		out = append(out, c.ID)
	}
	return out
}

// SortBy sorts by a column.
func (v *Usage) SortBy(col string, desc bool) error {
	if !slices.Contains(v.SortColumns(), col) {
		return fmt.Errorf("no column %q (use %s)", col, strings.Join(v.SortColumns(), ", "))
	}
	v.sortCol, v.sortDesc = col, desc
	return nil
}

// Open shows a finished job's efficiency card.
func (v *Usage) Open(ctx *Context, id string) {
	v.Refresh(ctx)
	v.table.SetCursorID(id)
	v.detail = id
}

// Selected returns the job under the cursor.
func (v *Usage) Selected() (model.HistoryJob, bool) {
	j, ok := v.byID[v.table.CursorID()]
	return j, ok
}

// Refresh implements View.
func (v *Usage) Refresh(ctx *Context) {
	data := ctx.Store.History.Data
	// sacct also reports queued and running jobs; the Jobs tab has those.
	v.jobs = v.jobs[:0]
	for _, j := range data.Jobs {
		if !j.State.IsActive() {
			v.jobs = append(v.jobs, j)
		}
	}
	v.byID = make(map[string]model.HistoryJob, len(v.jobs))
	for _, j := range v.jobs {
		v.byID[j.ID.Raw] = j
	}
	var shown []model.HistoryJob
	for _, j := range v.jobs {
		if v.filter.Match(asJob(j)) {
			shown = append(shown, j)
		}
	}
	shown = v.sorted(shown)
	v.summary = summariseHistory(shown)
	v.sum = insights.Summarise(v.jobs, v.days, ctx.Store.UID)
	v.acct = insights.BuildAccount(ctx.Store.UsageInput())
	v.table.Cols = v.columns(ctx.Config.Detailed())
	v.table.SortCol, v.table.SortDesc = v.sortCol, v.sortDesc
	v.table.Empty = fmt.Sprintf("No finished jobs in the last %s.", rangeLabel(v.days))
	if !ctx.Store.Caps.HasSacct && ctx.Store.Caps.Version != "" {
		v.table.Empty = "This cluster keeps no job accounting (no slurmdbd), so there is no history to show."
	}
	if !v.filter.Empty() {
		v.table.Empty = "No jobs match the filter. Press esc to clear it."
	}
	rows := make([]components.Row, 0, len(shown))
	for _, j := range shown {
		rows = append(rows, v.row(ctx, j))
	}
	v.table.SetRows(rows)
	if _, ok := v.byID[v.detail]; !ok {
		v.detail = ""
	}
}

// columns are the table's columns: the exit code, GPU count and partition
// only in the detailed layout.
func (v *Usage) columns(detailed bool) []layout.Column {
	var out []layout.Column
	for _, c := range historyColumns {
		if !detailed && slices.Contains(usageDetailed, c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// asJob adapts a finished job for the shared filter language.
func asJob(h model.HistoryJob) model.Job {
	return model.Job{ID: h.ID, Name: h.Name, State: h.State, Partition: h.Partition, GPUs: h.GPUs, CPUs: h.AllocCPUs, NodeList: h.NodeList}
}

func summariseHistory(jobs []model.HistoryJob) historySummary {
	s := historySummary{Jobs: len(jobs), MedianMem: -1}
	var mems []float64
	for _, j := range jobs {
		switch {
		case j.State == model.StateCompleted:
			s.Completed++
		case j.State.IsFailure():
			s.Failed++
		}
		s.GPUHours += j.Eff.GPUHours
		if j.State == model.StateCompleted && j.Eff.Mem >= 0 {
			mems = append(mems, j.Eff.Mem)
		}
	}
	if len(mems) > 0 {
		s.MedianMem = insights.Median(mems)
	}
	return s
}

func (v *Usage) sorted(jobs []model.HistoryJob) []model.HistoryJob {
	out := slices.Clone(jobs)
	col := v.sortCol
	if col == "" {
		// Default: most recently ended first.
		slices.SortStableFunc(out, func(a, b model.HistoryJob) int { return cmp.Or(b.End.Compare(a.End), cmp.Compare(b.ID.Raw, a.ID.Raw)) })
		return out
	}
	num := func(j model.HistoryJob) float64 {
		switch col {
		case "id":
			task, _ := strconv.Atoi(j.ID.TaskSpec)
			return float64(j.ID.ArrayJobID)*1e6 + float64(task)
		case "exit":
			return float64(j.ExitCode*1000 + j.Signal)
		case "elapsed":
			return j.Elapsed.Seconds()
		case "cpu":
			return j.Eff.CPU
		case "mem":
			return j.Eff.Mem
		case "gpus":
			return float64(j.GPUs)
		case "waste":
			return wasteHours(j)
		case "end":
			return float64(j.End.Unix())
		}
		return 0
	}
	str := func(j model.HistoryJob) string {
		switch col {
		case "name":
			return strings.ToLower(j.Name)
		case "state":
			return string(j.State)
		case "partition":
			return j.Partition
		}
		return ""
	}
	slices.SortStableFunc(out, func(a, b model.HistoryJob) int {
		c := cmp.Or(cmp.Compare(str(a), str(b)), cmp.Compare(num(a), num(b)))
		if v.sortDesc {
			return -c
		}
		return c
	})
	return out
}

func (v *Usage) row(ctx *Context, j model.HistoryJob) components.Row {
	th := ctx.Theme
	short := ctx.Mode <= layout.Normal
	exit := th.Faint.Render("0")
	if j.ExitCode != 0 || j.Signal != 0 {
		exit = strconv.Itoa(j.ExitCode)
		if j.Signal != 0 {
			exit += ":" + strconv.Itoa(j.Signal)
		}
		exit = th.Crit.Render(exit)
	}
	elapsed := units.FormatShort(j.Elapsed)
	if j.TimeLimit != nil {
		elapsed += th.Faint.Render("/" + units.FormatLimit(*j.TimeLimit))
	}
	state := th.StateLabel(j.State, "", short)
	if j.State == model.StateFailed && j.ExitCode != 0 && !ctx.Config.Detailed() {
		state += th.Faint.Render(" " + strconv.Itoa(j.ExitCode)) // the exit code, where it says the most
	}
	return components.Row{ID: j.ID.Raw, Cells: map[string]string{
		"id":        j.ID.Raw,
		"name":      j.Name,
		"state":     state,
		"exit":      exit,
		"elapsed":   elapsed,
		"cpu":       effCell(ctx, j.Eff.CPU),
		"mem":       effCell(ctx, j.Eff.Mem),
		"gpus":      countCell(th, j.GPUs),
		"waste":     wasteCell(th, j),
		"partition": j.Partition,
		"end":       shortTime(ctx.Now, j.End),
	}}
}

// wasteHours is the idle CPU- plus GPU-hours of a completed job, -1 when
// unknown (so unknown jobs sort last when the sort is descending).
func wasteHours(j model.HistoryJob) float64 {
	cpu, gpu, ok := insights.JobIdle(j)
	if !ok {
		return -1
	}
	return cpu + gpu
}

// wasteCell shows how much of what a completed job held sat idle.
func wasteCell(th theme.Theme, j model.HistoryJob) string {
	h := wasteHours(j)
	switch {
	case h < 0:
		return components.Dash(th)
	case h < 0.05:
		return th.Faint.Render("0")
	case h < 10:
		return fmt.Sprintf("%.1fh", h)
	case h < 100:
		return th.Warn.Render(fmt.Sprintf("%.0fh", h))
	}
	return th.Warn.Render(fmt.Sprintf("%.0fh", math.Min(h, 9999)))
}

func effCell(ctx *Context, f float64) string {
	th := ctx.Theme
	if f < 0 || math.IsNaN(f) {
		return components.Dash(th)
	}
	return th.EffLevel(f).Render(fmt.Sprintf("%d%%", int(math.Round(f*100))))
}

// Update implements View.
func (v *Usage) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	if v.detail != "" && v.input == nil && v.pane.update(ctx, msg) {
		return nil
	}
	k := ctx.Keys
	if v.input != nil {
		if m, ok := msg.(tea.KeyPressMsg); ok {
			done, cancel := v.input.Update(m)
			switch {
			case cancel:
				v.input = nil
				v.filter = Filter{}
				v.Refresh(ctx)
			case done:
				v.input = nil
			default:
				if f, err := ParseFilter(v.input.Value()); err == nil {
					v.filter, v.input.Err = f, ""
					v.Refresh(ctx)
				} else {
					v.input.Err = err.Error()
				}
			}
		}
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		j, ok := v.Selected()
		switch {
		case key.Matches(msg, k.Back):
			switch {
			case v.detail != "":
				v.detail = ""
			case !v.filter.Empty():
				v.filter = Filter{}
				v.Refresh(ctx)
			}
		case navigate(&v.table, k, msg):
			v.followDetail()
		case key.Matches(msg, k.Open):
			if ok {
				if v.detail == j.ID.Raw {
					v.detail = ""
				} else {
					v.detail = j.ID.Raw
				}
			}
		case key.Matches(msg, k.RangePrev), key.Matches(msg, k.RangeNext):
			i := slices.Index(HistoryRanges, v.days)
			if key.Matches(msg, k.RangePrev) {
				i = max(i-1, 0)
			} else {
				i = min(i+1, len(HistoryRanges)-1)
			}
			return v.SetRange(HistoryRanges[i])
		case key.Matches(msg, k.Filter):
			v.input = NewLineInput("filter: ", v.filter.Raw)
		case key.Matches(msg, k.Sort, k.SortReverse):
			v.cycleSort(key.Matches(msg, k.SortReverse))
			v.Refresh(ctx)
		case key.Matches(msg, k.Stdout) && ok:
			return Emit(LogMsg{Job: historyAsJob(ctx, j)})
		case key.Matches(msg, k.Stderr) && ok:
			return Emit(LogMsg{Job: historyAsJob(ctx, j), Stderr: true})
		case key.Matches(msg, k.Script) && ok:
			return Emit(ScriptMsg{Job: historyAsJob(ctx, j)})
		case key.Matches(msg, k.Rerun) && ok:
			return Emit(RerunMsg{ID: j.ID.Raw})
		case key.Matches(msg, k.Copy) && ok:
			if lines := rightSizeLines(j); v.detail == j.ID.Raw && lines != "" {
				return Emit(CopyMsg{Text: lines, What: "#SBATCH suggestions"})
			}
			return Emit(CopyMsg{Text: j.ID.Raw, What: "job ID " + j.ID.Raw})
		}
	case tea.MouseWheelMsg:
		navigate(&v.table, ctx.Keys, msg)
		v.followDetail()
	case tea.MouseClickMsg:
		return v.click(ctx, msg)
	}
	return nil
}

func (v *Usage) click(ctx *Context, msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	j, ok := v.Selected()
	switch {
	case ctx.InZone("usage:btn:close", msg):
		v.detail = ""
		return nil
	case ok && ctx.InZone("usage:btn:out", msg):
		return Emit(LogMsg{Job: historyAsJob(ctx, j)})
	case ok && ctx.InZone("usage:btn:err", msg):
		return Emit(LogMsg{Job: historyAsJob(ctx, j), Stderr: true})
	case ok && ctx.InZone("usage:btn:script", msg):
		return Emit(ScriptMsg{Job: historyAsJob(ctx, j)})
	case ok && ctx.InZone("usage:btn:rerun", msg):
		return Emit(RerunMsg{ID: j.ID.Raw})
	case ok && ctx.InZone("usage:btn:copy", msg):
		return Emit(CopyMsg{Text: rightSizeLines(j), What: "#SBATCH suggestions"})
	}
	for _, c := range historyColumns {
		if ctx.InZone(v.table.ColZone(c.ID), msg) {
			if v.sortCol == c.ID {
				v.sortDesc = !v.sortDesc
			} else {
				v.sortCol, v.sortDesc = c.ID, false
			}
			v.Refresh(ctx)
			return nil
		}
	}
	for _, r := range v.table.Rows {
		if ctx.InZone(v.table.RowZone(r.ID), msg) {
			if v.table.CursorID() == r.ID || v.detail != "" {
				v.detail = r.ID
			}
			v.table.SetCursorID(r.ID)
			return nil
		}
	}
	return nil
}

func (v *Usage) cycleSort(reverse bool) {
	if reverse {
		v.sortDesc = !v.sortDesc
		return
	}
	ids := v.SortColumns()
	i := slices.Index(ids, v.sortCol)
	if i+1 >= len(ids) {
		v.sortCol, v.sortDesc = "", false
		return
	}
	v.sortCol, v.sortDesc = ids[i+1], false
}

// historyAsJob gives log and script requests what they need.
func historyAsJob(ctx *Context, h model.HistoryJob) model.Job {
	j := asJob(h)
	j.User = ctx.Store.User
	return j
}

func rightSizeLines(h model.HistoryJob) string {
	var lines []string
	for _, s := range insights.RightSize(h) {
		if s.Line != "" {
			lines = append(lines, s.Line)
		}
	}
	return strings.Join(lines, "\n")
}

// Hints implements View.
func (v *Usage) Hints(ctx *Context) []key.Binding {
	k := ctx.Keys
	if v.input != nil {
		return []key.Binding{bind("enter", "apply"), bind("esc", "clear")}
	}
	hints := []key.Binding{k.Open, k.RangePrev, k.RangeNext, k.Filter}
	if v.detail != "" {
		hints = []key.Binding{bind("esc", "close"), k.Stdout, k.Stderr, k.Rerun, bind("y", "copy #SBATCH")}
	}
	return hints
}

// Render implements View.
func (v *Usage) Render(ctx *Context, w, h int) string {
	th := ctx.Theme
	var foot string
	if v.input != nil {
		foot = v.input.View(th, w)
		h--
	}
	// The summary sits above the table; the card replaces it. The clean
	// layout has three lines (jobs, idle, limits), the detailed one five.
	var panel []string
	if v.detail == "" {
		most := 3
		if ctx.Config.Detailed() {
			most = 5
		}
		panel = v.summaryLines(ctx, w, min(most, max(0, (h-6)/3)))
	}
	out := v.statusLine(ctx, w, len(panel) > 0)
	h--
	if len(panel) > 0 {
		for _, l := range panel {
			out += "\n" + l
		}
		out += "\n" + strings.Repeat(" ", w)
		h -= len(panel) + 1
	}
	var body string
	if v.detail == "" {
		body = v.tablePanel(ctx, w, h)
	} else {
		body = detailLayout(w, h, func(w, h int) string { return v.tablePanel(ctx, w, h) }, func(w, h int) string { return v.card(ctx, w, h) })
	}
	out += "\n" + body
	if foot != "" {
		out += "\n" + foot
	}
	return out
}

// summaryLines are the lines of the summary panel, most useful first; n is
// how many fit. Each part that a site hides says so in one line.
func (v *Usage) summaryLines(ctx *Context, w, n int) []string {
	if n <= 0 || !ctx.Store.History.Has || (ctx.Store.History.Data.Days != v.days && ctx.Store.History.Data.Days != 0) {
		return nil
	}
	th := ctx.Theme
	sep := " " + th.Sym.Separator + " "
	label := func(name, text string) string {
		return layout.Truncate(th.Bold.Render(fmt.Sprintf("%-9s", name))+text, w, th.Sym.Ellipsis)
	}
	s := v.sum
	var lines []string

	if s.Finished == 0 {
		lines = append(lines, label("Jobs", th.Muted.Render("nothing finished in this range")))
	} else {
		parts := []string{fmt.Sprintf("%d finished", s.Finished)}
		if s.Failed > 0 {
			f := fmt.Sprintf("%d failed (%.0f%%)", s.Failed, 100*s.FailRate)
			if s.TopFailure.Count > 0 {
				f += ", most often " + string(s.TopFailure.State)
			}
			parts = append(parts, th.Crit.Render(f))
		}
		parts = append(parts, "CPU "+insights.FormatHours(s.CPUHours))
		if s.GPUHours > 0 {
			parts = append(parts, "GPU "+insights.FormatHours(s.GPUHours))
		}
		lines = append(lines, label("Jobs", strings.Join(parts, sep)))
	}

	wst := s.Waste
	var idle []string
	if wst.CPUJobs > 0 {
		idle = append(idle, fmt.Sprintf("CPU %s of %s (%s)", insights.FormatHours(wst.IdleCPUHours), insights.FormatHours(wst.CPUHeld), insights.Percent(wst.IdleCPUHours, wst.CPUHeld)))
	}
	if wst.MemJobs > 0 {
		idle = append(idle, fmt.Sprintf("memory ~%.0f GB-h unused", wst.IdleMemGBh))
	}
	switch {
	case wst.GPUJobs > 0:
		idle = append(idle, fmt.Sprintf("GPU %s of %s (%s)", insights.FormatHours(wst.IdleGPUHours), insights.FormatHours(wst.GPUHeld), insights.Percent(wst.IdleGPUHours, wst.GPUHeld)))
	case s.GPUHours > 0:
		idle = append(idle, "GPU use is not recorded here")
	}
	if len(idle) == 0 {
		lines = append(lines, label("Idle", th.Muted.Render("no completed jobs to compare")))
	} else {
		lines = append(lines, label("Idle", strings.Join(idle, sep)))
	}

	lines = append(lines, label("Limits", v.limitsText(ctx, sep, w-9)))
	lines = append(lines, label("Priority", v.priorityText(ctx, sep)))
	if len(s.Worst) > 0 {
		var w3 []string
		for _, x := range s.Worst[:min(3, len(s.Worst))] {
			w3 = append(w3, x.Job.ID.Raw+" "+x.Job.Name)
		}
		lines = append(lines, label("Wasteful", strings.Join(w3, sep)+th.Faint.Render("  (sort by IDLE)")))
	}
	return lines[:min(n, len(lines))]
}

// limitsText shows the limits closest to being hit, or why there are none.
func (v *Usage) limitsText(ctx *Context, sep string, avail int) string {
	th := ctx.Theme
	if v.acct.LimitsNote != "" {
		return th.Muted.Render(v.acct.LimitsNote)
	}
	ls := slices.Clone(v.acct.Limits)
	slices.SortStableFunc(ls, func(a, b insights.LimitLine) int { return cmp.Compare(b.Frac, a.Frac) })
	// As many of the closest limits as fit the line.
	var out string
	for i, l := range ls {
		p := l.Owner + " " + l.What + " " + l.Text
		if l.Near() {
			p = th.Warn.Render(th.Sym.Warn + " " + p)
		}
		next := p
		if i > 0 {
			next = out + sep + p
		}
		more := ""
		if i+1 < len(ls) {
			more = th.Faint.Render(fmt.Sprintf(" +%d more", len(ls)-i-1))
		}
		if i > 0 && ansi.StringWidth(next+more) > avail {
			return out + th.Faint.Render(fmt.Sprintf(" +%d more", len(ls)-i))
		}
		out = next
	}
	return out
}

// priorityText shows your fairshare, or why there is none.
func (v *Usage) priorityText(ctx *Context, sep string) string {
	th := ctx.Theme
	if v.acct.PriorityNote != "" {
		return th.Muted.Render(v.acct.PriorityNote)
	}
	var parts []string
	for _, st := range v.acct.Standing {
		parts = append(parts, fmt.Sprintf("fairshare %.2f in %s: %s", st.FairShare, st.Account, st.Verdict))
	}
	if n := len(v.acct.Pending); n > 0 {
		parts = append(parts, fmt.Sprintf("%d pending (:fairshare)", n))
	}
	return strings.Join(parts, sep)
}

// statusLine is the range and what is listed; without the summary above
// the table it also carries the key numbers.
func (v *Usage) statusLine(ctx *Context, w int, panel bool) string {
	th := ctx.Theme
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	s := v.summary
	parts := []string{"last " + rangeLabel(v.days) + th.Faint.Render("  ([ ] changes)")}
	st := ctx.Store.History
	switch {
	case !ctx.Store.Caps.HasSacct && ctx.Store.Caps.Version != "":
		parts = append(parts, th.Muted.Render("job accounting (sacct) is not available on this cluster"))
	case st.Err != nil && !st.Has:
		parts = append(parts, th.Crit.Render(layout.FirstLine(st.Err.Error())))
	case !st.Has || (st.Data.Days != v.days && st.Data.Days != 0):
		parts = append(parts, th.Muted.Render("loading"+th.Sym.Ellipsis))
	default:
		parts = append(parts, plural(s.Jobs, "job"))
		if !panel {
			if s.Failed > 0 {
				parts = append(parts, th.Crit.Render(fmt.Sprintf("%d failed", s.Failed)))
			}
			if s.GPUHours > 0 {
				parts = append(parts, fmt.Sprintf("%.1f GPU-h", s.GPUHours))
			}
			if s.MedianMem >= 0 {
				parts = append(parts, "median mem "+th.EffLevel(s.MedianMem).Render(fmt.Sprintf("%d%%", int(math.Round(s.MedianMem*100)))))
			}
		}
		if !v.filter.Empty() {
			parts = append(parts, th.Info.Render("filter: "+v.filter.Raw))
		}
	}
	if v.days == 30 {
		parts = append(parts, th.Faint.Render("30 days is a slower query"))
	}
	return layout.Truncate(strings.Join(parts, sep), w, th.Sym.Ellipsis)
}

func (v *Usage) tablePanel(ctx *Context, w, h int) string {
	return v.table.Render(ctx.Theme, ctx.Zones, w, h)
}

// card is the efficiency card of the open job (like seff), with the
// failure hint and right-sizing suggestions.
func (v *Usage) card(ctx *Context, w, h int) string {
	th := ctx.Theme
	j, ok := v.byID[v.detail]
	if !ok {
		return components.Panel(th, "", "", w, h, false)
	}
	inner := w - 4
	var b []string
	hint := insights.ExplainFailure(j, ctx.Store.UID)
	if hint.Title != "" {
		style := th.Warn
		if hint.Severe {
			style = th.Crit
		}
		b = append(b, style.Render(hint.Title))
		b = append(b, layout.Wrap(hint.Text, inner)...)
		b = append(b, "")
	}
	b = append(b, insights.EffCard(j)...)
	if j.WorkDir != "" {
		b = append(b, "Directory:     "+tildify(j.WorkDir))
	}
	sugg := insights.RightSize(j)
	if len(sugg) > 0 {
		b = append(b, "", th.Bold.Render("Right-sizing"))
		for _, s := range sugg {
			line := th.Muted.Render(s.What+": ") + s.Reason
			b = append(b, layout.Wrap(line, inner)...)
			if s.Line != "" {
				b = append(b, "  "+th.Accent.Render(s.Line))
			}
		}
	}
	var btns []string
	btn := func(id, label string) {
		btns = append(btns, ctx.Mark("usage:btn:"+id, components.Button(th, label, false, false)))
	}
	btn("out", "Stdout")
	btn("err", "Stderr")
	btn("script", "Script")
	btn("rerun", "Rerun")
	if rightSizeLines(j) != "" {
		btn("copy", "Copy #SBATCH")
	}
	b = append(b, "", strings.Join(btns, " "))
	return v.pane.render(ctx, j.ID.Raw, j.ID.Raw+" "+j.Name, "usage:btn:close", strings.Join(b, "\n"), w, h)
}

func (v *Usage) followDetail() {
	if v.detail != "" {
		if j, ok := v.Selected(); ok {
			v.detail = j.ID.Raw
		}
	}
}
