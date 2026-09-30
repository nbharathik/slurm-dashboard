package views

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// doubleClick is the window for a double click on a row.
const doubleClick = 400 * time.Millisecond

// Update implements View; a changed sort is remembered.
func (v *Jobs) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	before := v.prefs()
	cmd := v.update(ctx, msg)
	return tea.Batch(cmd, prefChanges(before, v.prefs()))
}

// prefKey is where the sort of this tab is remembered.
func (v *Jobs) prefKey() string {
	if v.queue {
		return PrefQueueSort
	}
	return PrefJobsSort
}

func (v *Jobs) prefs() map[string]string {
	return map[string]string{v.prefKey(): sortPref(v.sortCol, v.sortDesc)}
}

// restoreSort applies the remembered sort when it names a column that exists.
func (v *Jobs) restoreSort(ctx *Context) {
	if col, desc := prefSort(ctx.Prefs[v.prefKey()]); col != "" && slices.Contains(v.SortColumns(), col) {
		v.sortCol, v.sortDesc = col, desc
	}
}

func (v *Jobs) update(ctx *Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return v.key(ctx, msg)
	case tea.MouseClickMsg:
		return v.click(ctx, msg)
	case tea.MouseWheelMsg:
		navigate(&v.table, ctx.Keys, msg)
	}
	return nil
}

func (v *Jobs) key(ctx *Context, msg tea.KeyPressMsg) tea.Cmd {
	k := ctx.Keys
	if v.menu != nil {
		out, done := v.menu.update(msg)
		if done {
			v.menu = nil
		}
		if out != nil {
			return Emit(out)
		}
		return nil
	}
	if v.input != nil {
		done, cancel := v.input.Update(msg)
		switch {
		case cancel:
			v.input = nil
			v.filter = Filter{}
			v.Refresh(ctx)
		case done:
			f, err := ParseFilter(v.input.Value())
			if err != nil {
				v.input.Err = err.Error()
				return nil
			}
			v.filter, v.input = f, nil
			v.Refresh(ctx)
		default:
			if f, err := ParseFilter(v.input.Value()); err == nil { // live filtering
				v.filter = f
				v.Refresh(ctx)
			}
		}
		return nil
	}

	jobs := v.SelectedJobs()
	cur, hasCur := v.cursorJob()
	if v.queue {
		if cmd, ok := v.queueKey(ctx, msg, cur, hasCur); ok {
			return cmd
		}
	}
	switch {
	case navigate(&v.table, k, msg):
	case key.Matches(msg, k.Back):
		switch {
		case v.detail:
			v.closeDetail()
			return Emit(DetailMsg{})
		case !v.filter.Empty():
			v.filter = Filter{}
			v.Refresh(ctx)
		case len(v.table.Selected) > 0:
			v.table.Selected = nil
		}
	case key.Matches(msg, k.Open):
		if v.toggleGroup(ctx) {
			return nil
		}
		if hasCur {
			return v.openDetail(cur)
		}
	case key.Matches(msg, k.Select):
		if !v.toggleGroup(ctx) {
			v.table.Toggle()
			v.table.Move(1)
		}
	case key.Matches(msg, k.Expand), key.Matches(msg, k.Collapse):
		v.setGroup(ctx, key.Matches(msg, k.Expand))
	case key.Matches(msg, k.SelectAll):
		v.table.SelectAll()
	case key.Matches(msg, k.Sort):
		v.cycleSort(ctx)
	case key.Matches(msg, k.SortReverse):
		if v.sortCol == "" {
			v.sortCol = "submit"
		}
		v.sortDesc = !v.sortDesc
		v.Refresh(ctx)
	case key.Matches(msg, k.Filter):
		v.input = NewLineInput("filter: ", v.filter.Raw)
	case key.Matches(msg, k.Scope):
		if v.queue {
			return Emit(SwitchTabMsg{Tab: model.TabJobs})
		}
		return Emit(SwitchTabMsg{Tab: model.TabQueue})
	case key.Matches(msg, k.Menu):
		if len(jobs) > 0 {
			v.menu = newJobMenu(jobs, ctx.Store.User)
		}
	case key.Matches(msg, k.Copy):
		if hasCur {
			return Emit(CopyMsg{Text: cur.ID.Raw, What: "job ID " + cur.ID.Raw})
		}
	case key.Matches(msg, k.Cancel):
		return actionOn("cancel", jobs)
	case key.Matches(msg, k.Hold):
		return actionOn("hold", jobs)
	case key.Matches(msg, k.Release):
		return actionOn("release", jobs)
	case key.Matches(msg, k.Requeue):
		return actionOn("requeue", jobs)
	case key.Matches(msg, k.Stdout), key.Matches(msg, k.Stderr):
		if hasCur {
			return Emit(LogMsg{Job: cur, Stderr: key.Matches(msg, k.Stderr)})
		}
	case key.Matches(msg, k.Pager):
		if hasCur {
			return Emit(PagerMsg{Job: cur})
		}
	case key.Matches(msg, k.Shell):
		if hasCur {
			return Emit(ShellMsg{Job: cur})
		}
	case key.Matches(msg, k.GPU):
		if hasCur {
			return Emit(GPUSampleMsg{Job: cur})
		}
	case key.Matches(msg, k.Why):
		if hasCur {
			return v.openDetail(cur)
		}
	case key.Matches(msg, k.Script):
		if hasCur {
			return Emit(ScriptMsg{Job: cur})
		}
	case msg.String() == "i" && v.detail:
		v.allFields = !v.allFields
	}
	return nil
}

func actionOn(id string, jobs []model.Job) tea.Cmd {
	if len(jobs) == 0 {
		return Emit(FlashMsg{Text: "no job selected", Err: true})
	}
	return Emit(ActionMsg{Action: id, Jobs: jobs})
}

func (v *Jobs) openDetail(j model.Job) tea.Cmd {
	v.detail, v.detailID, v.allFields = true, j.ID.Raw, false
	return Emit(DetailMsg{ID: j.ID.Raw})
}

func (v *Jobs) closeDetail() { v.detail, v.detailID = false, "" }

// toggleGroup expands or collapses the array or queue group under the
// cursor.
func (v *Jobs) toggleGroup(ctx *Context) bool {
	id := v.table.CursorID()
	if g, ok := strings.CutPrefix(id, "grp:"); ok {
		v.folded[g] = v.isOpen(id)
		v.Refresh(ctx)
		v.table.SetCursorID(id)
		return true
	}
	members, ok := v.members[id]
	if !ok || len(members) == 0 {
		return false
	}
	arr := members[0].ID.ArrayJobID
	v.expanded[arr] = !v.expanded[arr]
	v.Refresh(ctx)
	return true
}

func (v *Jobs) setGroup(ctx *Context, open bool) {
	id := v.table.CursorID()
	if g, ok := strings.CutPrefix(id, "grp:"); ok {
		v.folded[g] = !open
		v.Refresh(ctx)
		v.table.SetCursorID(id)
		return
	}
	if members, ok := v.members[id]; ok {
		v.expanded[members[0].ID.ArrayJobID] = open
		v.Refresh(ctx)
		return
	}
	if !open {
		if j, ok := v.byID[id]; ok && j.ID.IsArray() && v.expanded[j.ID.ArrayJobID] {
			v.expanded[j.ID.ArrayJobID] = false
			v.Refresh(ctx)
			v.table.SetCursorID("arr:" + strconv.FormatUint(j.ID.ArrayJobID, 10))
		}
	}
}

func (v *Jobs) cycleSort(ctx *Context) {
	cols := v.table.VisibleColumns()
	if len(cols) == 0 {
		return
	}
	next := cols[0].ID
	for i, c := range cols {
		if c.ID == v.sortCol && i+1 < len(cols) {
			next = cols[i+1].ID
		}
	}
	if v.sortCol != "" && cols[len(cols)-1].ID == v.sortCol {
		next = "" // back to the default order
	}
	v.sortCol, v.sortDesc = next, false
	v.Refresh(ctx)
}

func (v *Jobs) click(ctx *Context, msg tea.MouseClickMsg) tea.Cmd {
	if v.menu != nil {
		for i := range v.menu.items {
			if ctx.InZone(zoneMenuItem(i), msg) {
				out := v.menu.items[i].msg
				v.menu = nil
				return Emit(out)
			}
		}
		v.menu = nil
		return nil
	}
	if v.queue && v.chipClick(ctx, msg) {
		return nil
	}
	for _, name := range []string{"cancel", "hold", "release", "requeue"} {
		if ctx.InZone("jobs:btn:"+name, msg) {
			return actionOn(name, v.SelectedJobs())
		}
	}
	if j, ok := v.cursorJob(); ok {
		switch {
		case ctx.InZone("jobs:btn:shell", msg):
			return Emit(ShellMsg{Job: j})
		case ctx.InZone("jobs:btn:script", msg):
			return Emit(ScriptMsg{Job: j})
		case ctx.InZone("jobs:btn:out", msg):
			return Emit(LogMsg{Job: j})
		case ctx.InZone("jobs:btn:err", msg):
			return Emit(LogMsg{Job: j, Stderr: true})
		case ctx.InZone("jobs:btn:fields", msg):
			v.allFields = !v.allFields
			return nil
		case ctx.InZone("jobs:btn:close", msg):
			v.closeDetail()
			return Emit(DetailMsg{})
		}
	}
	for _, c := range v.table.VisibleColumns() {
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
	for i, r := range v.table.Rows {
		if i < v.table.Offset || !ctx.InZone(v.table.RowZone(r.ID), msg) {
			continue
		}
		v.table.Cursor = i
		switch msg.Button {
		case tea.MouseRight:
			if jobs := v.SelectedJobs(); len(jobs) > 0 {
				v.menu = newJobMenu(jobs, ctx.Store.User)
			}
			return nil
		case tea.MouseLeft:
			double := v.lastClick.id == r.ID && ctx.Now.Sub(v.lastClick.at) < doubleClick
			v.lastClick.id, v.lastClick.at = r.ID, ctx.Now
			if double {
				if v.toggleGroup(ctx) {
					return nil
				}
				if j, ok := v.cursorJob(); ok {
					return v.openDetail(j)
				}
			}
		}
		return nil
	}
	return nil
}
