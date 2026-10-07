package views

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// nodeColumns are the columns of the Nodes table. The resource columns all
// read "free/total"; load and free-in belong to the detailed layout.
var nodeColumns = []layout.Column{
	{ID: "node", Title: "NODE", Priority: 1, Min: 5, Max: 24},
	{ID: "state", Title: "STATE", Priority: 1, Min: 5, Max: 10},
	{ID: "cpu", Title: "CPU FREE", Priority: 1, Min: 8, Max: 10, Right: true},
	{ID: "mem", Title: "MEM FREE", Priority: 3, Min: 8, Max: 12, Right: true},
	{ID: "gpus", Title: "GPU FREE", Priority: 1, Min: 8, Max: 10, Right: true},
	{ID: "type", Title: "GPU TYPE", Priority: 3, Min: 4, Max: 24},
	{ID: "load", Title: "LOAD", Priority: 4, Min: 4, Max: 6, Right: true},
	{ID: "jobs", Title: "JOBS", Priority: 2, Min: 8, Max: 40, Flex: true},
	{ID: "freeby", Title: "FREE IN", Priority: 5, Min: 7, Max: 10, Right: true},
}

// Node kinds that G cycles through.
var nodeKinds = []string{"all", "gpu", "cpu"}

// Nodes is the Nodes tab: every node, grouped by partition, with the
// partitions and reservations below.
type Nodes struct {
	table    components.Table
	kind     string // all | gpu | cpu
	grouped  bool
	folded   map[string]bool // partition → collapsed, when the user chose
	sortBy   string
	sortDesc bool
	filter   NodeFilter
	input    *LineInput
	usage    map[string]state.NodeUsage
	listed   []state.NodeUsage // the nodes that pass the kind and the filter
	parts    []state.PartSummary
	res      []model.Reservation
	detail   string // open node
	pane     detailPane
	anyGPU   bool
	anyMIG   bool
}

// NewNodes builds the Nodes tab, with the kind, grouping and sort that were
// chosen last time.
func NewNodes(ctx *Context) *Nodes {
	v := &Nodes{
		kind: "all", grouped: true, folded: map[string]bool{}, sortBy: "name",
		table: components.Table{Name: "nodes", Focused: true, Empty: "No nodes to show."},
	}
	if k := ctx.Prefs[PrefNodesKind]; slices.Contains(nodeKinds, k) {
		v.kind = k
	}
	v.grouped = ctx.Prefs[PrefNodesGroup] != "no"
	if col, desc := prefSort(ctx.Prefs[PrefNodesSort]); slices.Contains(state.NodeSorts, col) {
		v.sortBy, v.sortDesc = col, desc
	}
	return v
}

// prefs are the choices worth remembering.
func (v *Nodes) prefs() map[string]string {
	group := "yes"
	if !v.grouped {
		group = "no"
	}
	return map[string]string{PrefNodesKind: v.kind, PrefNodesGroup: group, PrefNodesSort: sortPref(v.sortBy, v.sortDesc)}
}

// Name implements View.
func (v *Nodes) Name() string { return model.TabNodes }

// Title implements View.
func (v *Nodes) Title() string { return "Nodes" }

// Source implements View.
func (v *Nodes) Source() string { return "nodes" }

// Sources lists the data the tab shows, for refresh.
func (v *Nodes) Sources() []string { return []string{"nodes", "cluster", "partitions", "reservations"} }

// Badge implements View.
func (v *Nodes) Badge(*Context) string { return "" }

// Capturing implements View.
func (v *Nodes) Capturing() bool { return v.input != nil }

// HasSearch marks a tab where / filters the table.
func (v *Nodes) HasSearch() {}

// ShowGPUNodes switches to GPU nodes only (/tab gpus).
func (v *Nodes) ShowGPUNodes(ctx *Context) {
	v.kind = "gpu"
	v.Refresh(ctx)
}

// SetFilter applies filter text (from /filter).
func (v *Nodes) SetFilter(s string) error {
	f, err := ParseNodeFilter(s)
	if err != nil {
		return err
	}
	v.filter = f
	return nil
}

// SortColumns lists the sort orders for /sort.
func (v *Nodes) SortColumns() []string { return state.NodeSorts }

// SortBy sorts the nodes (from /sort).
func (v *Nodes) SortBy(col string, desc bool) error {
	if !slices.Contains(state.NodeSorts, col) {
		return fmt.Errorf("no sort %q (use %s)", col, strings.Join(state.NodeSorts, ", "))
	}
	v.sortBy, v.sortDesc = col, desc
	return nil
}

// Open shows a node's detail.
func (v *Nodes) Open(ctx *Context, name string) {
	v.Refresh(ctx)
	if _, ok := v.usage[name]; !ok {
		return
	}
	if !v.cursorTo(name) {
		v.kind, v.filter = "all", NodeFilter{}
		v.Refresh(ctx)
		v.cursorTo(name)
	}
	v.detail = name
}

// cursorTo moves the cursor to the first row of node name, opening its
// partition group when collapsed.
func (v *Nodes) cursorTo(name string) bool {
	for _, r := range v.table.Rows {
		if nodeOfRow(r.ID) == name {
			return v.table.SetCursorID(r.ID)
		}
	}
	return false
}

// Row IDs: "part:NAME" for a partition group, "PART/NODE" for a node in a
// group and "NODE" in the flat list.
func nodeOfRow(id string) string {
	if strings.HasPrefix(id, "part:") {
		return ""
	}
	if _, n, ok := strings.Cut(id, "/"); ok {
		return n
	}
	return id
}

func (v *Nodes) kindMatch(n model.Node) bool {
	switch v.kind {
	case "gpu":
		return n.HasGPUs()
	case "cpu":
		return !n.HasGPUs()
	}
	return true
}

// Refresh implements View.
func (v *Nodes) Refresh(ctx *Context) {
	st := ctx.Store
	all, summaries := st.Capacity()
	v.usage = make(map[string]state.NodeUsage, len(all))
	v.anyGPU, v.anyMIG = false, false
	var list []state.NodeUsage
	for _, u := range all {
		if state.Hidden(ctx.Config.HidePartitions, u.Node.Partitions) {
			continue
		}
		v.usage[u.Node.Name] = u
		v.anyGPU = v.anyGPU || u.Node.HasGPUs()
		v.anyMIG = v.anyMIG || u.Node.MIGTotal > 0
		if v.kindMatch(u.Node) && v.filter.Match(u, ctx.Config.GPUNames) {
			list = append(list, u)
		}
	}
	state.SortNodes(list, v.sortBy, v.sortDesc)
	v.listed = list

	v.parts = nil
	for _, p := range summaries {
		if !slices.Contains(ctx.Config.HidePartitions, p.Partition.Name) {
			v.parts = append(v.parts, p)
		}
	}

	v.table.Cols = v.columns(ctx.Config.Detailed())
	v.table.SortCol = map[string]string{"name": "node", "state": "state", "cpu": "cpu", "gpu": "gpus", "load": "load"}[v.sortBy]
	v.table.SortDesc = v.sortDesc
	if v.grouped && len(v.parts) > 0 {
		v.table.SetRows(v.groupedRows(ctx, list))
	} else {
		rows := make([]components.Row, 0, len(list))
		for _, u := range list {
			rows = append(rows, v.nodeRow(ctx, u))
		}
		v.table.SetRows(rows)
	}

	v.res = nil
	for _, r := range st.Reservations.Data {
		if r.End.IsZero() || r.End.After(ctx.Now) {
			v.res = append(v.res, r)
		}
	}
	slices.SortFunc(v.res, func(a, b model.Reservation) int { return a.Start.Compare(b.Start) })
	if _, ok := v.usage[v.detail]; !ok {
		v.detail = ""
	}
}

// columns drops the GPU columns on clusters (or views) without GPUs, and
// the load and free-in columns unless the layout is detailed.
func (v *Nodes) columns(detailed bool) []layout.Column {
	var out []layout.Column
	for _, c := range nodeColumns {
		switch c.ID {
		case "gpus", "type":
			if (!v.anyGPU && !v.anyMIG) || v.kind == "cpu" {
				continue
			}
		case "load", "freeby":
			if !detailed {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// groupedRows puts the nodes under their partitions. A node in several
// partitions appears under each; a group whose nodes all appeared in
// earlier groups starts collapsed.
func (v *Nodes) groupedRows(ctx *Context, list []state.NodeUsage) []components.Row {
	var rows []components.Row
	seen := map[string]bool{}
	open := false // the last group shows its nodes
	for _, s := range v.parts {
		var members []state.NodeUsage
		for _, u := range list {
			if slices.Contains(u.Node.Partitions, s.Partition.Name) {
				members = append(members, u)
			}
		}
		if len(members) == 0 {
			continue
		}
		dup := true
		for _, m := range members {
			dup = dup && seen[m.Node.Name]
			seen[m.Node.Name] = true
		}
		folded, chosen := v.folded[s.Partition.Name]
		if !chosen {
			folded = dup
		}
		if open { // air between groups that show their nodes
			rows = append(rows, components.Row{ID: "gap:" + s.Partition.Name, Gap: true})
		}
		rows = append(rows, v.groupRow(ctx, s, members, folded))
		open = !folded
		if folded {
			continue
		}
		for _, m := range members {
			r := v.nodeRow(ctx, m)
			r.ID, r.Indent = s.Partition.Name+"/"+m.Node.Name, 1
			rows = append(rows, r)
		}
	}
	for _, u := range list { // nodes in no listed partition
		if !seen[u.Node.Name] {
			if open {
				rows = append(rows, components.Row{ID: "gap:none", Gap: true})
				open = false
			}
			rows = append(rows, v.nodeRow(ctx, u))
		}
	}
	return rows
}

// Update implements View; a changed kind, grouping or sort is remembered.
func (v *Nodes) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	before := v.prefs()
	cmd := v.update(ctx, msg)
	return tea.Batch(cmd, prefChanges(before, v.prefs()))
}

func (v *Nodes) update(ctx *Context, msg tea.Msg) tea.Cmd {
	if v.detail != "" && v.input == nil && v.pane.update(ctx, msg) {
		return nil
	}
	k := ctx.Keys
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if v.input != nil {
			v.updateInput(ctx, msg)
			return nil
		}
		id := v.table.CursorID()
		switch {
		case key.Matches(msg, k.Back):
			switch {
			case v.detail != "":
				v.detail = ""
			case !v.filter.Empty():
				v.filter = NodeFilter{}
				v.Refresh(ctx)
			}
		case navigate(&v.table, k, msg):
			v.followDetail()
		case key.Matches(msg, k.Open), key.Matches(msg, k.Select):
			if strings.HasPrefix(id, "part:") {
				v.toggleGroup(ctx, strings.TrimPrefix(id, "part:"))
			} else if n := nodeOfRow(id); n != "" {
				if v.detail == n {
					v.detail = ""
				} else {
					v.detail = n
				}
			}
		case key.Matches(msg, k.Expand), key.Matches(msg, k.Collapse):
			p, _, _ := strings.Cut(strings.TrimPrefix(id, "part:"), "/")
			if p != id || strings.HasPrefix(id, "part:") {
				v.folded[p] = key.Matches(msg, k.Collapse)
				v.Refresh(ctx)
				v.table.SetCursorID("part:" + p)
			}
		case key.Matches(msg, k.GPUOnly):
			v.kind = nodeKinds[(slices.Index(nodeKinds, v.kind)+1)%len(nodeKinds)]
			v.Refresh(ctx)
			return Emit(FlashMsg{Text: map[string]string{"all": "Showing all nodes", "gpu": "Showing GPU nodes", "cpu": "Showing CPU-only nodes"}[v.kind]})
		case key.Matches(msg, k.Group):
			v.grouped = !v.grouped
			v.Refresh(ctx)
		case key.Matches(msg, k.Filter):
			v.input = NewLineInput("filter: ", v.filter.Raw)
		case key.Matches(msg, k.Sort):
			v.sortBy = state.NodeSorts[(slices.Index(state.NodeSorts, v.sortBy)+1)%len(state.NodeSorts)]
			v.Refresh(ctx)
		case key.Matches(msg, k.SortReverse):
			v.sortDesc = !v.sortDesc
			v.Refresh(ctx)
		case key.Matches(msg, k.Copy):
			if n := nodeOfRow(id); n != "" {
				return Emit(CopyMsg{Text: n, What: "node name " + n})
			}
		}
	case tea.MouseWheelMsg:
		navigate(&v.table, ctx.Keys, msg)
		v.followDetail()
	case tea.MouseClickMsg:
		return v.click(ctx, msg)
	}
	return nil
}

func (v *Nodes) followDetail() {
	if v.detail != "" {
		if n := nodeOfRow(v.table.CursorID()); n != "" {
			v.detail = n
		}
	}
}

func (v *Nodes) updateInput(ctx *Context, msg tea.KeyPressMsg) {
	done, cancel := v.input.Update(msg)
	switch {
	case cancel:
		v.input, v.filter = nil, NodeFilter{}
	case done:
		f, err := ParseNodeFilter(v.input.Value())
		if err != nil {
			v.input.Err = err.Error()
			return
		}
		v.filter, v.input = f, nil
	default:
		if f, err := ParseNodeFilter(v.input.Value()); err == nil { // live filtering
			v.filter, v.input.Err = f, ""
		}
	}
	v.Refresh(ctx)
}

func (v *Nodes) toggleGroup(ctx *Context, part string) {
	folded, chosen := v.folded[part]
	if !chosen { // it is showing as the default chose; flip what is shown
		folded = !v.isOpen(part)
	}
	v.folded[part] = !folded
	v.Refresh(ctx)
	v.table.SetCursorID("part:" + part)
}

// isOpen reports whether a partition group currently shows its nodes.
func (v *Nodes) isOpen(part string) bool {
	for _, r := range v.table.Rows {
		if strings.HasPrefix(r.ID, part+"/") {
			return true
		}
	}
	return false
}

func (v *Nodes) click(ctx *Context, msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if ctx.InZone("nodes:close", msg) {
		v.detail = ""
		return nil
	}
	for col, by := range map[string]string{"node": "name", "state": "state", "cpu": "cpu", "gpus": "gpu", "load": "load"} {
		if ctx.InZone(v.table.ColZone(col), msg) {
			if v.sortBy == by {
				v.sortDesc = !v.sortDesc
			} else {
				v.sortBy, v.sortDesc = by, false
			}
			v.Refresh(ctx)
			return nil
		}
	}
	for _, r := range v.table.Rows {
		if !ctx.InZone(v.table.RowZone(r.ID), msg) {
			continue
		}
		if part, ok := strings.CutPrefix(r.ID, "part:"); ok {
			v.table.SetCursorID(r.ID)
			v.toggleGroup(ctx, part)
			return nil
		}
		n := nodeOfRow(r.ID)
		if v.table.CursorID() == r.ID || v.detail != "" {
			v.detail = n
		}
		v.table.SetCursorID(r.ID)
		return nil
	}
	return nil
}

// Hints implements View.
func (v *Nodes) Hints(ctx *Context) []key.Binding {
	k := ctx.Keys
	switch {
	case v.input != nil:
		return []key.Binding{bind("enter", "apply"), bind("esc", "clear")}
	case v.detail != "":
		return []key.Binding{bind("esc", "close"), k.GPUOnly, k.Sort}
	}
	return []key.Binding{bind("enter", "details"), k.Filter, k.GPUOnly, k.Sort, k.Group}
}
