package views

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// Overview panels.
const (
	panelAlerts  = "alerts"
	panelCluster = "cluster"
	panelJobs    = "jobs"
	panelStorage = "storage"
)

// overviewJobColumns are the columns of the "My jobs" panel.
var overviewJobColumns = []string{"id", "name", "state", "time", "res", "reason"}

// Overview is tab 1 in four panels: the cluster's capacity per partition,
// my jobs, alerts, and storage with fairshare.
type Overview struct {
	focus   string
	alerts  []model.Alert
	parts   []state.PartSummary
	myJobs  []model.Job
	jobs    components.Table
	quotas  []model.Quota
	share   *model.Share
	acct    *model.Share
	failed  int // jobs failed today
	cur     map[string]int
	jobView Jobs // row rendering
}

// NewOverview builds the Overview tab.
func NewOverview(*Context) *Overview {
	v := &Overview{focus: panelJobs, cur: map[string]int{}}
	v.jobs = components.Table{Name: "ovjobs", Empty: "No jobs in the queue."}
	return v
}

// Name implements View.
func (v *Overview) Name() string { return model.TabOverview }

// Title implements View.
func (v *Overview) Title() string { return "Overview" }

// Source implements View.
func (v *Overview) Source() string { return "myjobs" }

// Sources lists the data the tab shows, for refresh.
func (v *Overview) Sources() []string {
	return []string{"myjobs", "nodes", "cluster", "partitions", "reservations", "storage", "fairshare"}
}

// Badge marks the tab when there is something to look at.
func (v *Overview) Badge(*Context) string {
	if len(v.alerts) > 0 {
		return "!"
	}
	return ""
}

// Capturing implements View.
func (v *Overview) Capturing() bool { return false }

// Alerts returns the active alerts (for the CLI and tests).
func (v *Overview) Alerts() []model.Alert { return v.alerts }

// Refresh implements View.
func (v *Overview) Refresh(ctx *Context) {
	st := ctx.Store
	v.alerts = st.Alerts(ctx.Now, ctx.Dismissed)

	var parts []model.Partition
	for _, p := range st.Partitions.Data {
		if !slices.Contains(ctx.Config.HidePartitions, p.Name) {
			parts = append(parts, p)
		}
	}
	pending, known := state.PendingByPartition(st)
	v.parts = state.PartitionSummaries(parts, state.NodeGPUUsage(st.Nodes.Data, st.Cluster.Data.Jobs, st.User), pending, known)

	v.myJobs = state.JoinJobs(st.MyJobs.Data, st.Cluster.Data.Jobs, st.QueueRank.Data)
	var cols []layout.Column
	for _, c := range jobColumns {
		if slices.Contains(overviewJobColumns, c.ID) {
			cols = append(cols, c)
		}
	}
	v.jobs.Cols = cols
	rows := make([]components.Row, 0, len(v.myJobs))
	for _, j := range sortForOverview(v.myJobs) {
		rows = append(rows, v.jobView.jobRow(ctx, j))
	}
	v.jobs.SetRows(rows)

	v.quotas = st.Storage.Data
	v.share, v.acct = nil, nil
	for i, s := range st.Fairshare.Data {
		switch {
		case s.User == st.User && v.share == nil:
			v.share = &st.Fairshare.Data[i]
		case s.User == "" && v.acct == nil:
			v.acct = &st.Fairshare.Data[i]
		}
	}
	v.failed = 0
	day := ctx.Now.Add(-24 * time.Hour)
	for _, h := range st.History.Data.Jobs {
		if h.State.IsFailure() && h.End.After(day) {
			v.failed++
		}
	}
	for p, n := range v.cur {
		v.cur[p] = min(n, max(v.count(p)-1, 0))
	}
	if !slices.Contains(v.panels(), v.focus) {
		v.focus = panelJobs
	}
}

// sortForOverview puts running jobs first, then pending, then the rest.
func sortForOverview(jobs []model.Job) []model.Job {
	rank := func(j model.Job) int {
		switch theme.KindOf(j.State, j.Reason) {
		case theme.KindRunning, theme.KindCompleting:
			return 0
		case theme.KindPending:
			return 1
		case theme.KindHeld:
			return 2
		}
		return 3
	}
	out := slices.Clone(jobs)
	slices.SortStableFunc(out, func(a, b model.Job) int {
		if c := cmp.Compare(rank(a), rank(b)); c != 0 {
			return c
		}
		if a.QueueRank != b.QueueRank && a.QueueRank > 0 && b.QueueRank > 0 {
			return cmp.Compare(a.QueueRank, b.QueueRank)
		}
		return a.SubmitTime.Compare(b.SubmitTime)
	})
	return out
}

// panels are the visible panels in focus order.
func (v *Overview) panels() []string {
	var out []string
	if len(v.alerts) > 0 {
		out = append(out, panelAlerts)
	}
	return append(out, panelJobs, panelCluster, panelStorage)
}

func (v *Overview) count(p string) int {
	switch p {
	case panelAlerts:
		return len(v.alerts)
	case panelCluster:
		return len(v.parts)
	case panelStorage:
		n := len(v.quotas)
		if v.share != nil {
			n++
		}
		return n
	}
	return 0
}

// Update implements View.
func (v *Overview) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	k := ctx.Keys
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, k.NextFocus):
			v.cycleFocus(1)
		case key.Matches(msg, k.PrevFocus):
			v.cycleFocus(-1)
		case key.Matches(msg, k.Up):
			v.move(-1)
		case key.Matches(msg, k.Down):
			v.move(1)
		case key.Matches(msg, k.Home):
			v.move(-1 << 20)
		case key.Matches(msg, k.End):
			v.move(1 << 20)
		case key.Matches(msg, k.Open):
			return v.open()
		case key.Matches(msg, k.Dismiss) && v.focus == panelAlerts:
			if i := v.cur[panelAlerts]; i < len(v.alerts) {
				return Emit(DismissMsg{Key: v.alerts[i].Key})
			}
		case key.Matches(msg, k.Cancel, k.Hold, k.Release, k.Requeue, k.Stdout, k.Stderr, k.Shell, k.Why) && v.focus == panelJobs:
			// Job keys act on the "My jobs" cursor via the Jobs tab.
			if id := v.jobs.CursorID(); id != "" {
				return tea.Sequence(Emit(OpenJobMsg{ID: id}), Emit(msg))
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			v.move(-1)
		case tea.MouseWheelDown:
			v.move(1)
		}
	case tea.MouseClickMsg:
		return v.click(ctx, msg)
	}
	return nil
}

func (v *Overview) cycleFocus(d int) {
	ps := v.panels()
	i := slices.Index(ps, v.focus)
	v.focus = ps[(i+d+len(ps))%len(ps)]
}

func (v *Overview) move(d int) {
	if v.focus == panelJobs {
		v.jobs.Move(d)
		return
	}
	n := v.count(v.focus)
	if n == 0 {
		return
	}
	v.cur[v.focus] = min(max(v.cur[v.focus]+d, 0), n-1)
}

func (v *Overview) open() tea.Cmd {
	i := v.cur[v.focus]
	switch v.focus {
	case panelAlerts:
		if i >= len(v.alerts) {
			return nil
		}
		a := v.alerts[i]
		switch {
		case a.JobID != "" && a.Tab == model.TabJobs:
			return Emit(OpenJobMsg{ID: a.JobID})
		case a.JobID != "" && a.Tab == model.TabUsage:
			return Emit(HistoryJobMsg{ID: a.JobID})
		case a.Tab != "":
			return Emit(SwitchTabMsg{Tab: a.Tab})
		}
	case panelCluster:
		if i < len(v.parts) {
			// Nodes, filtered to the partition.
			return tea.Sequence(Emit(SwitchTabMsg{Tab: model.TabNodes}), Emit(RunCommandMsg{Line: "filter part:" + v.parts[i].Partition.Name}))
		}
		return Emit(SwitchTabMsg{Tab: model.TabNodes})
	case panelJobs:
		if id := v.jobs.CursorID(); id != "" {
			return Emit(OpenJobMsg{ID: id})
		}
	case panelStorage:
		if i == len(v.quotas) && v.share != nil {
			return Emit(RunCommandMsg{Line: "fairshare"})
		}
		return Emit(SwitchTabMsg{Tab: model.TabStorage})
	}
	return nil
}

func (v *Overview) click(ctx *Context, msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	hit := func(p string, i int) bool { return ctx.InZone(fmt.Sprintf("ov:%s:%d", p, i), msg) }
	for _, p := range []string{panelAlerts, panelCluster, panelStorage} {
		for i := range v.count(p) {
			if hit(p, i) {
				again := v.focus == p && v.cur[p] == i
				v.focus, v.cur[p] = p, i
				if again {
					return v.open()
				}
				return nil
			}
		}
	}
	for i := range v.alerts {
		if ctx.InZone(fmt.Sprintf("ov:alerts:open:%d", i), msg) {
			v.focus, v.cur[panelAlerts] = panelAlerts, i
			return v.open()
		}
	}
	for _, r := range v.jobs.Rows {
		if ctx.InZone(v.jobs.RowZone(r.ID), msg) {
			again := v.focus == panelJobs && v.jobs.CursorID() == r.ID
			v.focus = panelJobs
			v.jobs.SetCursorID(r.ID)
			if again {
				return v.open()
			}
			return nil
		}
	}
	for _, p := range v.panels() {
		if ctx.InZone("ov:panel:"+p, msg) {
			v.focus = p
		}
	}
	return nil
}

// Hints implements View.
func (v *Overview) Hints(ctx *Context) []key.Binding {
	k := ctx.Keys
	hints := []key.Binding{bind("enter", "open"), k.NextFocus}
	if v.focus == panelAlerts {
		hints = append(hints, k.Dismiss)
	}
	return hints
}
