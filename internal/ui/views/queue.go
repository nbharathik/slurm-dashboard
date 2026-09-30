package views

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// StartEstimateMsg asks the app when a pending job should start
// (squeue --start for that one job).
type StartEstimateMsg struct{ Job model.Job }

// foldAbove is the queue size above which groups start collapsed.
const foldAbove = 1000

// NewQueue builds the Queue tab: everyone's jobs.
func NewQueue(ctx *Context) *Jobs {
	group := ctx.Prefs[PrefQueueGroup]
	if !slices.Contains(state.QueueGroups, group) {
		group = "state"
	}
	v := &Jobs{queue: true, expanded: map[uint64]bool{}, folded: map[string]bool{}, groupBy: group}
	defer v.restoreSort(ctx)
	v.table = components.Table{Name: "queue", Focused: true, Empty: "The queue is empty."}
	return v
}

// queueKey handles the keys that differ on the Queue tab: e asks for a
// start estimate, v cycles what is shown (all, mine, running, ...) and g
// cycles the grouping. Sorting is s and S, as everywhere.
func (v *Jobs) queueKey(ctx *Context, msg tea.KeyPressMsg, cur model.Job, hasCur bool) (tea.Cmd, bool) {
	k := ctx.Keys
	switch {
	case key.Matches(msg, k.Estimate):
		if !hasCur || cur.State != model.StatePending {
			return Emit(FlashMsg{Text: "start estimates are for pending jobs", Err: true}), true
		}
		return Emit(StartEstimateMsg{Job: cur}), true
	case key.Matches(msg, k.ScopeCycle):
		return v.cycleScope(ctx), true
	case key.Matches(msg, k.Group):
		groups := state.QueueGroups
		v.groupBy = groups[(slices.Index(groups, v.groupBy)+1)%len(groups)]
		v.Refresh(ctx)
		return tea.Batch(Emit(FlashMsg{Text: "Grouped by " + v.groupBy}), Emit(PrefMsg{Key: PrefQueueGroup, Value: v.groupBy})), true
	}
	return nil, false
}

// groupKey is the group a job falls in.
func (v *Jobs) groupKey(j model.Job) string {
	switch v.groupBy {
	case "user":
		return j.User
	case "partition":
		return j.Partition
	}
	switch j.State {
	case model.StateRunning, model.StateCompleting, model.StateConfiguring:
		return "running"
	case model.StatePending:
		if strings.HasPrefix(j.Reason, "JobHeld") {
			return "held"
		}
		return "pending"
	}
	return strings.ToLower(string(j.State))
}

// queueRows groups the jobs under one heading row per group.
func (v *Jobs) queueRows(ctx *Context, jobs []model.Job) []components.Row {
	buckets := map[string][]model.Job{}
	var order []string
	for _, j := range jobs {
		g := v.groupKey(j)
		if _, ok := buckets[g]; !ok {
			order = append(order, g)
		}
		buckets[g] = append(buckets[g], j)
	}
	rank := func(g string) int {
		if v.groupBy == "state" {
			if i := slices.Index([]string{"running", "pending", "held"}, g); i >= 0 {
				return i
			}
			return 3
		}
		if g == ctx.Store.User {
			return -1 // your own group first
		}
		return 0
	}
	slices.SortStableFunc(order, func(a, b string) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(len(buckets[b]), len(buckets[a])), strings.Compare(a, b))
	})
	var rows []components.Row
	open := false // the last group shows its jobs
	for _, g := range order {
		members := buckets[g]
		id := "grp:" + g
		folded, chosen := v.folded[g]
		if !chosen {
			folded = len(jobs) > foldAbove
		}
		if open { // air between groups that show their jobs
			rows = append(rows, components.Row{ID: "gap:" + g, Gap: true})
		}
		rows = append(rows, v.queueGroupRow(ctx, id, g, members, folded))
		open = !folded
		if !folded {
			rows = append(rows, v.jobRows(ctx, members, 1)...)
		}
	}
	return rows
}

func (v *Jobs) queueGroupRow(ctx *Context, id, label string, jobs []model.Job, folded bool) components.Row {
	th := ctx.Theme
	marker := th.Sym.Expanded
	if folded {
		marker = th.Sym.Collapsed
	}
	cpus, gpus := 0, 0
	for _, j := range jobs {
		cpus += j.CPUs
		gpus += j.GPUs
	}
	head := th.Bold.Render(marker + " " + label)
	if label == ctx.Store.User {
		head = th.Accent.Render(marker + " " + label)
	}
	if label == "" {
		head = th.Bold.Render(marker + " (none)")
	}
	return components.Row{ID: id, Heading: true, Cells: map[string]string{
		"id": head, "name": th.Muted.Render(plural(len(jobs), "job")),
		"res": th.Bold.Render(resCell(model.Job{CPUs: cpus, GPUs: gpus})),
	}}
}

// isOpen reports whether a group row's jobs are shown below it.
func (v *Jobs) isOpen(id string) bool {
	for i, r := range v.table.Rows {
		if r.ID == id {
			return i+1 < len(v.table.Rows) && v.table.Rows[i+1].Indent > r.Indent
		}
	}
	return false
}

// queueSummary is the Queue tab's heading: jobs per partition, the users
// holding the most resources and why jobs wait.
type queueSummary struct {
	parts            []partCount
	users            []userUse
	reasons          []reasonCount
	total            int
	running, pending int
}

type partCount struct {
	name             string
	running, pending int
}

type userUse struct {
	user       string
	cpus, gpus int
}

type reasonCount struct {
	reason string
	n      int
}

func summariseQueue(jobs []model.Job) queueSummary {
	var s queueSummary
	parts := map[string]*partCount{}
	users := map[string]*userUse{}
	reasons := map[string]int{}
	for _, j := range jobs {
		n := taskCount(j.ID.TaskSpec)
		s.total += n
		p := parts[j.Partition]
		if p == nil {
			p = &partCount{name: j.Partition}
			parts[j.Partition] = p
		}
		switch j.State {
		case model.StateRunning, model.StateCompleting:
			p.running += n
			s.running += n
			who := j.User
			u := users[who]
			if u == nil {
				u = &userUse{user: who}
				users[who] = u
			}
			u.cpus += j.CPUs
			u.gpus += j.GPUs
		case model.StatePending:
			p.pending += n
			s.pending += n
			reasons[j.Reason] += n
		}
	}
	for _, p := range parts {
		s.parts = append(s.parts, *p)
	}
	slices.SortFunc(s.parts, func(a, b partCount) int {
		return cmp.Or(cmp.Compare(b.running+b.pending, a.running+a.pending), strings.Compare(a.name, b.name))
	})
	for _, u := range users {
		s.users = append(s.users, *u)
	}
	slices.SortFunc(s.users, func(a, b userUse) int {
		return cmp.Or(cmp.Compare(b.gpus, a.gpus), cmp.Compare(b.cpus, a.cpus), strings.Compare(a.user, b.user))
	})
	for r, n := range reasons {
		s.reasons = append(s.reasons, reasonCount{r, n})
	}
	slices.SortFunc(s.reasons, func(a, b reasonCount) int { return cmp.Or(cmp.Compare(b.n, a.n), strings.Compare(a.reason, b.reason)) })
	return s
}

// summaryLines renders the summary, at most max lines.
func (v *Jobs) summaryLines(ctx *Context, maxLines int) []string {
	th := ctx.Theme
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	s := v.summary
	var out []string
	if ctx.Store.PrivateJobs {
		out = append(out, th.Warn.Render("Other users' jobs are hidden by site policy (PrivateData=jobs); only yours are listed."))
	}
	var parts []string
	for _, p := range s.parts {
		parts = append(parts, th.Bold.Render(p.name)+" "+th.StateStyle(theme.KindRunning).Render(fmt.Sprintf("R %d", p.running))+" "+
			th.StateStyle(theme.KindPending).Render(fmt.Sprintf("PD %d", p.pending)))
	}
	if len(parts) > 0 {
		out = append(out, strings.Join(parts, sep))
	}
	var users []string
	for _, u := range s.users[:min(len(s.users), 5)] {
		label := fmt.Sprintf("%s %d CPU", u.user, u.cpus)
		if u.gpus > 0 {
			label += fmt.Sprintf(" %d GPU", u.gpus)
		}
		users = append(users, label)
	}
	if len(users) > 0 {
		out = append(out, th.Muted.Render("Running most: ")+strings.Join(users, sep))
	}
	var reasons []string
	for _, r := range s.reasons[:min(len(s.reasons), 4)] {
		reasons = append(reasons, fmt.Sprintf("%s %d", cmp.Or(r.reason, "None"), r.n))
	}
	if len(reasons) > 0 {
		out = append(out, th.Muted.Render("Waiting on: ")+strings.Join(reasons, sep))
	}
	out = append(out, v.chips(ctx))
	if len(out) > maxLines {
		out = append(out[:maxLines-1], out[len(out)-1]) // keep the chips
	}
	return out
}

// queueChips are one-click filters: label and filter text.
var queueChips = []struct{ label, filter string }{
	{"all", ""}, {"running", "state:R"}, {"pending", "state:PD"}, {"held", "state:H"}, {"mine", "user:@me"}, {"GPU jobs", "gpu:>0"},
}

func (v *Jobs) chips(ctx *Context) string {
	th := ctx.Theme
	var out []string
	for _, c := range queueChips {
		f := strings.ReplaceAll(c.filter, "@me", ctx.Store.User)
		label := " " + c.label + " "
		if v.filter.Raw == f {
			label = th.TabActive.Render("[" + c.label + "]")
		} else {
			label = th.Tab.Render(label)
		}
		out = append(out, ctx.Mark("queue:chip:"+c.label, label))
	}
	return strings.Join(out, " ")
}

// chipClick applies a clicked filter chip.
func (v *Jobs) chipClick(ctx *Context, msg tea.MouseClickMsg) bool {
	for _, c := range queueChips {
		if ctx.InZone("queue:chip:"+c.label, msg) {
			f, _ := ParseFilter(strings.ReplaceAll(c.filter, "@me", ctx.Store.User))
			v.filter = f
			v.Refresh(ctx)
			return true
		}
	}
	return false
}

// plural renders "1 GPU" or "3 GPUs".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// scopeLabel names the filter when it is one of the queueChips ("pending"),
// else "".
func (v *Jobs) scopeLabel(ctx *Context) string {
	for _, c := range queueChips {
		if v.filter.Raw == strings.ReplaceAll(c.filter, "@me", ctx.Store.User) {
			return c.label
		}
	}
	return ""
}

// cycleScope moves to the next of all / running / pending / held / mine /
// GPU jobs; from any other filter it starts at "all".
func (v *Jobs) cycleScope(ctx *Context) tea.Cmd {
	next := 0
	for i, c := range queueChips {
		if v.filter.Raw == strings.ReplaceAll(c.filter, "@me", ctx.Store.User) {
			next = (i + 1) % len(queueChips)
		}
	}
	c := queueChips[next]
	v.filter, _ = ParseFilter(strings.ReplaceAll(c.filter, "@me", ctx.Store.User))
	v.Refresh(ctx)
	name := map[string]string{"all": "all jobs", "mine": "your jobs"}[c.label]
	if name == "" {
		name = c.label
		if !strings.HasSuffix(name, "jobs") {
			name += " jobs"
		}
	}
	return Emit(FlashMsg{Text: "Showing " + name})
}
