package views

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// jobColumns lists Jobs columns in display order; priority 1 is never dropped.
var jobColumns = []layout.Column{
	{ID: "id", Title: "ID", Priority: 1, Min: 5},
	{ID: "user", Title: "USER", Priority: 2, Min: 5, Max: 12},
	{ID: "name", Title: "NAME", Priority: 1, Min: 8, Max: 32},
	{ID: "state", Title: "STATE", Priority: 1, Min: 5, Max: 16},
	{ID: "time", Title: "ELAPSED/LIMIT", Priority: 2, Min: 9, Max: 16},
	{ID: "partition", Title: "PART", Priority: 3, Min: 4, Max: 12},
	{ID: "res", Title: "RESOURCES", Priority: 2, Min: 9, Max: 16},
	{ID: "reason", Title: "WHERE", Priority: 2, Min: 8, Max: 40, Flex: true},
	{ID: "gpus", Title: "GPU", Priority: 3, Min: 3, Max: 4, Right: true},
	{ID: "cpus", Title: "CPU", Priority: 3, Min: 3, Max: 5, Right: true},
	{ID: "mem", Title: "MEM", Priority: 3, Min: 4, Max: 7, Right: true},
	{ID: "start", Title: "START", Priority: 4, Min: 5, Max: 12},
	{ID: "account", Title: "ACCOUNT", Priority: 5, Min: 6, Max: 14},
	{ID: "qos", Title: "QOS", Priority: 5, Min: 3, Max: 10},
	{ID: "submit", Title: "SUBMIT", Priority: 5, Min: 5, Max: 12},
}

// The columns the Jobs and Queue tabs show, and what the detailed layout
// adds; the rest of jobColumns can still be sorted by.
var (
	jobsShown     = []string{"id", "name", "state", "time", "res", "reason"}
	queueShown    = []string{"id", "user", "name", "state", "time", "partition", "res", "reason"}
	jobsDetailed  = []string{"partition", "mem", "start"}
	queueDetailed = []string{"account", "start"}
)

// Jobs is the Jobs tab (the user's jobs) and, in queue mode, the Queue
// tab (everyone's jobs, grouped, with a summary).
type Jobs struct {
	table       components.Table
	queue       bool
	groupBy     string          // queue: state | user | partition | none
	folded      map[string]bool // queue: group key → collapsed, when chosen
	summary     queueSummary
	filter      Filter
	input       *LineInput // filter line while typing
	sortCol     string     // "" = default order
	sortDesc    bool
	expanded    map[uint64]bool
	jobs        []model.Job
	byID        map[string]int              // row of each job in jobs; see lookup
	alloc       map[string]model.RunningJob // queue: what running jobs were allocated
	members     map[string][]model.Job      // group row → member jobs
	detail      bool
	pane        detailPane
	detailID    string
	savedCursor string
	allFields   bool
	menu        *jobMenu
	lastClick   struct {
		id string
		at time.Time
	}
}

// NewJobs builds the Jobs tab, sorted the way it was last time.
func NewJobs(ctx *Context) *Jobs {
	v := &Jobs{expanded: map[uint64]bool{}}
	defer v.restoreSort(ctx)
	v.table = components.Table{Name: "jobs", Focused: true, Empty: "No jobs in the queue. To run one again, press n on it in Usage."}
	return v
}

// Name implements View.
func (v *Jobs) Name() string {
	if v.queue {
		return model.TabQueue
	}
	return model.TabJobs
}

// ActiveDetailID identifies the private detail panel currently rendered by this view.
func (v *Jobs) ActiveDetailID(ctx *Context) string {
	if !v.detail {
		return ""
	}
	if j, ok := v.lookup(v.detailID); ok && j.OwnedBy(ctx.Store.User) {
		return j.ID.Raw
	}
	return ""
}

// Title implements View.
func (v *Jobs) Title() string {
	if v.queue {
		return "Queue"
	}
	return "Jobs"
}

// Source implements View.
func (v *Jobs) Source() string {
	if v.queue {
		return "alljobs"
	}
	return "myjobs"
}

// Sources lists the data the tab shows, for refresh.
func (v *Jobs) Sources() []string {
	if v.queue {
		return []string{"alljobs", "jobdetail"}
	}
	return []string{"myjobs", "cluster", "queuerank", "jobdetail"}
}

// HasSearch marks a tab where / filters the table.
func (v *Jobs) HasSearch() {}

// Badge implements View: the tab shows no count.
func (v *Jobs) Badge(*Context) string { return "" }

// Capturing implements View.
func (v *Jobs) Capturing() bool { return v.input != nil || v.menu != nil }

// SetFilter applies filter text (from /filter).
func (v *Jobs) SetFilter(s string) error {
	f, err := ParseFilter(s)
	if err != nil {
		return err
	}
	v.filter = f
	return nil
}

// SortColumns lists sortable column IDs.
func (v *Jobs) SortColumns() []string {
	var out []string
	for _, c := range jobColumns {
		out = append(out, c.ID)
	}
	return out
}

// SortBy sorts by a column (from /sort or a header click).
func (v *Jobs) SortBy(col string, desc bool) error {
	if !slices.Contains(v.SortColumns(), col) {
		return fmt.Errorf("no column %q (use %s)", col, strings.Join(v.SortColumns(), ", "))
	}
	v.sortCol, v.sortDesc = col, desc
	return nil
}

// Open shows a job's detail (from the Overview or /why).
func (v *Jobs) Open(ctx *Context, id string) {
	v.Refresh(ctx)
	// Unfold a collapsed array so the cursor, and so every action, is on the task itself.
	if j, ok := v.lookup(id); ok && j.ID.IsArray() && !v.table.SetCursorID(id) {
		v.expanded[j.ID.ArrayJobID] = true
		v.Refresh(ctx)
	}
	if !v.table.SetCursorID(id) {
		for rowID, members := range v.members {
			for _, m := range members {
				if m.ID.Raw == id {
					v.table.SetCursorID(rowID)
				}
			}
		}
	}
	v.detail, v.detailID = true, id
}

// SelectedJobs returns the selected jobs, or the cursor job.
func (v *Jobs) SelectedJobs() []model.Job {
	var out []model.Job
	for _, id := range v.table.SelectedIDs() {
		if m, ok := v.members[id]; ok {
			out = append(out, m...)
		} else if j, ok := v.lookup(id); ok {
			out = append(out, j)
		}
	}
	return out
}

// ExportTable returns the table as shown, filter and sort included.
func (v *Jobs) ExportTable() ([]string, [][]string) { return v.table.Export() }

// CurrentJob is the job whose card is open, else the one under the cursor.
func (v *Jobs) CurrentJob() (model.Job, bool) {
	if v.detail {
		if j, ok := v.lookup(v.detailID); ok {
			return j, true
		}
	}
	return v.cursorJob()
}

// lookup returns a job by raw ID.
func (v *Jobs) lookup(id string) (model.Job, bool) {
	i, ok := v.byID[id]
	if !ok || i < 0 || i >= len(v.jobs) {
		return model.Job{}, false
	}
	return v.jobs[i], true
}

// cursorJob is the job under the cursor (the first member of a group).
func (v *Jobs) cursorJob() (model.Job, bool) {
	id := v.table.CursorID()
	if m, ok := v.members[id]; ok && len(m) > 0 {
		return m[0], true
	}
	return v.lookup(id)
}

// CopyText returns text for /copy.
func (v *Jobs) CopyText(ctx *Context, what string) (string, error) {
	j, ok := v.cursorJob()
	if !ok {
		return "", fmt.Errorf("no job under the cursor")
	}
	switch what {
	case "id":
		return j.ID.Raw, nil
	case "path":
		if d := v.detailFor(ctx, j); d != nil && d.StdOut != "" {
			return LogPath(d, false), nil
		}
		return "", fmt.Errorf("open the job's detail first (enter) so its log path is known")
	case "cmd":
		return "scontrol show job " + j.ID.Raw, nil
	}
	return "", fmt.Errorf("copy id, path or cmd")
}

// Refresh rebuilds the rows from the store.
func (v *Jobs) Refresh(ctx *Context) {
	st := ctx.Store
	if v.queue {
		// Read-only: everyone's jobs are not copied (the largest data on a
		// big cluster); nothing here changes them.
		v.jobs = st.AllJobs.Data
		v.alloc = nil
		if st.Cluster.Has {
			v.alloc = make(map[string]model.RunningJob, len(st.Cluster.Data.Jobs))
			for _, r := range st.Cluster.Data.Jobs {
				v.alloc[r.ID.Raw] = r
			}
		}
	} else {
		v.jobs = state.JoinJobs(st.MyJobs.Data, st.Cluster.Data.Jobs, st.QueueRank.Data)
	}
	v.summary = summariseQueue(v.jobs, v.held)
	v.byID = make(map[string]int, len(v.jobs))
	var arrays map[uint64]bool
	if len(v.expanded) > maxExpanded && len(v.jobs) > 0 {
		arrays = map[uint64]bool{}
	}
	for i, j := range v.jobs {
		v.byID[j.ID.Raw] = i
		if arrays != nil && j.ID.IsArray() {
			arrays[j.ID.ArrayJobID] = true
		}
	}
	for arr := range v.expanded {
		if arrays != nil && !arrays[arr] {
			delete(v.expanded, arr) // the array left the queue
		}
	}
	if v.detail && v.detailID != "" && len(v.jobs) > 0 {
		if _, ok := v.byID[v.detailID]; !ok {
			if id, ok := v.rangeNow(v.detailID); ok {
				v.detailID = id // a task started, so the pending range was renamed
			} else {
				v.closeDetail()
			}
		}
	}
	v.table.Cols = v.columns(ctx.Config.Detailed())
	v.table.SortCol, v.table.SortDesc = v.sortCol, v.sortDesc
	v.table.SetRows(v.rows(ctx))
	if v.savedCursor != "" {
		v.table.SetCursorID(v.savedCursor)
		v.savedCursor = ""
	}
}

// Invalidate releases obsolete hidden Queue rows; view choices survive reopening.
func (v *Jobs) Invalidate() {
	if !v.queue {
		return
	}
	if id := v.table.CursorID(); id != "" {
		v.savedCursor = strings.Clone(id)
	}
	v.table.ReleaseRows()
	v.jobs, v.byID, v.members, v.alloc = nil, nil, nil, nil
	v.summary = queueSummary{}
}

func (v *Jobs) columns(detailed bool) []layout.Column {
	want := slices.Clone(jobsShown)
	if v.queue {
		want = slices.Clone(queueShown)
	}
	if detailed && v.queue {
		want = append(want, queueDetailed...)
	} else if detailed {
		want = append(want, jobsDetailed...)
	}
	var out []layout.Column
	for _, c := range jobColumns {
		switch {
		case c.ID == "user":
			if v.queue {
				out = append(out, c)
			}
		case c.ID == "id" || c.ID == "name" || c.ID == "state" || slices.Contains(want, c.ID) || len(want) == 0:
			out = append(out, c)
		}
	}
	return out
}

// rows filters, groups and sorts the jobs.
func (v *Jobs) rows(ctx *Context) []components.Row {
	jobs := v.jobs // sorted copies it; filtering copies only what matches
	if !v.filter.Empty() {
		jobs = nil
		for _, j := range v.jobs {
			if v.filter.Match(v.held(j)) { // gpu: and mem: match what is shown
				jobs = append(jobs, j)
			}
		}
	}
	jobs = v.sorted(jobs)
	v.members = map[string][]model.Job{}
	if v.queue && v.groupBy != "none" {
		return v.queueRows(ctx, jobs)
	}
	return v.jobRows(ctx, jobs, 0)
}

// jobRows renders jobs, with arrays of more than one row under a parent
// row. indent shifts every row (inside a queue group).
func (v *Jobs) jobRows(ctx *Context, jobs []model.Job, indent int) []components.Row {
	// Group arrays with more than one row under a parent.
	count := map[uint64]int{}
	arrays := map[uint64][]model.Job{}
	for _, j := range jobs {
		if j.ID.IsArray() {
			count[j.ID.ArrayJobID]++
			arrays[j.ID.ArrayJobID] = append(arrays[j.ID.ArrayJobID], j)
		}
	}
	var rows []components.Row
	emitted := map[uint64]bool{}
	for _, j := range jobs {
		if j.ID.IsArray() && count[j.ID.ArrayJobID] > 1 {
			arr := j.ID.ArrayJobID
			if emitted[arr] {
				continue
			}
			emitted[arr] = true
			members := arrays[arr]
			gid := "arr:" + strconv.FormatUint(arr, 10)
			v.members[gid] = members
			g := v.groupRow(ctx, gid, arr, members)
			g.Indent = indent
			rows = append(rows, g)
			if v.expanded[arr] {
				for _, m := range members {
					r := v.jobRow(ctx, m)
					r.Indent = indent + 1
					rows = append(rows, r)
				}
			}
			continue
		}
		r := v.jobRow(ctx, j)
		r.Indent = indent
		rows = append(rows, r)
	}
	return rows
}

func (v *Jobs) sorted(jobs []model.Job) []model.Job {
	if v.sortCol == "" {
		return report.SortJobs(jobs)
	}
	out := slices.Clone(jobs)
	key := func(j model.Job) (string, float64) {
		switch v.sortCol {
		case "id":
			return "", float64(j.ID.ArrayJobID)
		case "user":
			return j.User, 0
		case "name":
			return strings.ToLower(j.Name), 0
		case "state":
			return string(j.State), 0
		case "time":
			return "", j.TimeUsed.Seconds()
		case "gpus":
			return "", float64(v.held(j).TotalGPUs())
		case "res":
			return "", float64(v.held(j).TotalGPUs())*1e6 + float64(j.TotalCPUs())
		case "reason":
			return j.Reason + strings.Join(j.NodeList, ","), 0
		case "partition":
			return j.Partition, 0
		case "cpus":
			return "", float64(j.TotalCPUs())
		case "mem":
			return "", float64(v.memOf(j))
		case "start":
			return "", float64(j.StartTime.Unix())
		case "account":
			return j.Account, 0
		case "qos":
			return j.QOS, 0
		case "submit":
			return "", float64(j.SubmitTime.Unix())
		}
		return "", 0
	}
	slices.SortStableFunc(out, func(a, b model.Job) int {
		sa, na := key(a)
		sb, nb := key(b)
		c := cmp.Compare(sa, sb)
		if c == 0 {
			c = cmp.Compare(na, nb)
		}
		if v.sortDesc {
			c = -c
		}
		return c
	})
	return out
}

func (v *Jobs) jobRow(ctx *Context, j model.Job) components.Row {
	th := ctx.Theme
	short := ctx.Mode <= layout.Normal
	gpus := v.held(j).TotalGPUs()
	cells := map[string]string{
		"id":        j.ID.Raw,
		"user":      v.userLabel(ctx, j.User),
		"name":      j.Name,
		"state":     th.StateLabel(j.State, j.Reason, short),
		"time":      timeCell(th, j),
		"res":       resText(j.TotalCPUs(), gpus),
		"gpus":      countCell(th, gpus),
		"reason":    whereCell(th, j),
		"partition": j.Partition,
		"cpus":      strconv.Itoa(j.TotalCPUs()),
		"mem":       memCell(th, v.memOf(j)),
		"start":     startCell(th, ctx.Now, j),
		"account":   j.Account,
		"qos":       j.QOS,
		"submit":    shortTime(ctx.Now, j.SubmitTime),
	}
	return components.Row{ID: j.ID.Raw, Cells: cells}
}

func (v *Jobs) userLabel(ctx *Context, u string) string {
	if u == ctx.Store.User {
		return ctx.Theme.Accent.Render(u)
	}
	return u
}

func (v *Jobs) groupRow(ctx *Context, gid string, arr uint64, members []model.Job) components.Row {
	th := ctx.Theme
	counts := map[theme.StateKind]int{}
	gpus, cpus := 0, 0
	for _, m := range members {
		// Count tasks, not queue records: "815_[4-9]" is six pending tasks.
		counts[theme.KindOf(m.State, m.Reason)] += m.ID.Tasks()
		gpus += v.held(m).TotalGPUs()
		cpus += m.TotalCPUs()
	}
	var parts []string
	for _, k := range []theme.StateKind{theme.KindRunning, theme.KindPending, theme.KindHeld, theme.KindCompleting, theme.KindFailed, theme.KindCancelled, theme.KindCompleted, theme.KindNeutral} {
		if n := counts[k]; n > 0 {
			parts = append(parts, th.StateStyle(k).Render(fmt.Sprintf("%s%d", th.StateIcon(k), n)))
		}
	}
	marker := th.Sym.Collapsed
	if v.expanded[arr] {
		marker = th.Sym.Expanded
	}
	tasks := 0
	for _, m := range members {
		tasks += m.ID.Tasks()
	}
	return components.Row{ID: gid, Cells: map[string]string{
		"id":        marker + " " + strconv.FormatUint(arr, 10),
		"user":      v.userLabel(ctx, members[0].User),
		"name":      members[0].Name,
		"state":     strings.Join(parts, " "),
		"time":      th.Muted.Render(fmt.Sprintf("%d tasks", tasks)),
		"res":       resText(cpus, gpus),
		"gpus":      countCell(th, gpus),
		"cpus":      strconv.Itoa(cpus),
		"reason":    whereCell(th, members[len(members)-1]),
		"partition": members[0].Partition, "account": members[0].Account, "qos": members[0].QOS,
		"submit": shortTime(ctx.Now, members[0].SubmitTime),
	}}
}

// MapSizes counts the entries of maps kept between refreshes (soak test).
func (v *Jobs) MapSizes() int { return len(v.expanded) + len(v.folded) }

// maxExpanded is how many unfolded arrays are kept before gone ones are dropped.
const maxExpanded = 32

// held is j with its allocation when it runs and the cluster query has it.
func (v *Jobs) held(j model.Job) model.Job {
	if r, ok := v.alloc[j.ID.Raw]; ok && j.State == model.StateRunning {
		return state.Allocated(j, r)
	}
	return j
}

func (v *Jobs) memOf(j model.Job) int64 { return v.held(j).MemPerNodeMB }

// rangeNow is the pending range of old's array that is still listed, when
// old was a pending range ("815_[3-9]" becomes "815_[4-9]").
func (v *Jobs) rangeNow(old string) (string, bool) {
	arr, spec, ok := strings.Cut(old, "_")
	if !ok || !strings.HasPrefix(spec, "[") {
		return "", false
	}
	for _, j := range v.jobs {
		if strconv.FormatUint(j.ID.ArrayJobID, 10) == arr && strings.HasPrefix(j.ID.TaskSpec, "[") {
			return j.ID.Raw, true
		}
	}
	return "", false
}

func timeCell(th theme.Theme, j model.Job) string {
	used := units.FormatShort(j.TimeUsed)
	if j.State == model.StatePending {
		used = "-"
	}
	if j.TimeLimit == nil {
		return used + th.Faint.Render("/"+th.Pick("∞", "inf"))
	}
	return used + th.Faint.Render("/"+units.FormatLimit(*j.TimeLimit))
}

// resText is what a row holds: "4 GPU 32 CPU", or "16 CPU" without GPUs.
func resText(cpus, gpus int) string {
	s := strconv.Itoa(cpus) + " CPU"
	if gpus > 0 {
		s = strconv.Itoa(gpus) + " GPU " + s
	}
	return s
}

func countCell(th theme.Theme, n int) string {
	if n == 0 {
		return th.Faint.Render(th.Sym.Dot)
	}
	return strconv.Itoa(n)
}

func memCell(th theme.Theme, mb int64) string {
	if mb <= 0 {
		return th.Faint.Render("node")
	}
	return units.FormatMB(float64(mb))
}

// whereCell shows nodes for running jobs and the reason (with queue rank)
// for pending ones.
func whereCell(th theme.Theme, j model.Job) string {
	if j.State == model.StatePending {
		r := j.Reason
		if j.QueueRank > 0 && !strings.HasPrefix(r, "JobHeld") {
			return th.Muted.Render(fmt.Sprintf("#%d/%d ", j.QueueRank, j.QueueTotal)) + r
		}
		return r
	}
	switch len(j.NodeList) {
	case 0:
		return th.Faint.Render(j.Reason)
	case 1:
		return j.NodeList[0]
	}
	return fmt.Sprintf("%s +%d", j.NodeList[0], len(j.NodeList)-1)
}

func startCell(th theme.Theme, now time.Time, j model.Job) string {
	if j.StartTime.IsZero() {
		return th.Faint.Render(th.Sym.Dot)
	}
	if j.State == model.StatePending {
		if d := j.StartTime.Sub(now); d > 0 {
			return th.Muted.Render("in " + units.FormatShort(d))
		}
	}
	return shortTime(now, j.StartTime)
}

// shortTime renders "14:05" today, "Sep 27" otherwise.
func shortTime(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	if y1, m1, d1 := now.Local().Date(); true {
		if y2, m2, d2 := t.Date(); y1 == y2 && m1 == m2 && d1 == d2 {
			return t.Format("15:04")
		}
	}
	return t.Format("Jan 2")
}

// detailFor returns the scontrol detail of j if it is loaded.
func (v *Jobs) detailFor(ctx *Context, j model.Job) *model.JobDetail {
	if !j.OwnedBy(ctx.Store.User) {
		return nil
	}
	d := ctx.Store.Detail.Data
	if d.Job == nil || !ctx.Store.Detail.Has || !d.Job.OwnedBy(ctx.Store.User) {
		return nil
	}
	if d.ID == j.ID.Raw || d.Job.ID.Raw == j.ID.Raw {
		return d.Job
	}
	return nil
}

func (v *Jobs) statFor(ctx *Context, j model.Job) *model.JobStat {
	if !j.OwnedBy(ctx.Store.User) {
		return nil
	}
	d := ctx.Store.Detail.Data
	if d.Job != nil && d.Job.OwnedBy(ctx.Store.User) && d.Stat != nil && (d.ID == j.ID.Raw) {
		return d.Stat
	}
	return nil
}

// Hints implements View.
func (v *Jobs) Hints(ctx *Context) []key.Binding {
	k := ctx.Keys
	switch {
	case v.input != nil:
		return []key.Binding{bind("enter", "apply"), bind("esc", "clear")}
	case v.menu != nil:
		return []key.Binding{bind("enter", "run"), bind("esc", "close")}
	}
	j, ok := v.cursorJob()
	hints := []key.Binding{k.Open}
	if v.queue {
		if ok && j.State == model.StatePending {
			hints = append(hints, k.Estimate)
		}
		return append(hints, k.Filter, k.ScopeCycle, k.Sort, k.Group)
	}
	if ok {
		switch j.State {
		case model.StateRunning:
			hints = append(hints, k.Cancel, k.Stdout)
		case model.StatePending:
			hints = append(hints, k.Why, k.Cancel)
		}
	}
	return append(hints, k.Filter, k.Menu, k.Scope)
}

func bind(k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
}
