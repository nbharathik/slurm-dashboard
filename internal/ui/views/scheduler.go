package views

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
)

// Scheduler is the optional Scheduler tab: sdiag's view of the controller.
type Scheduler struct {
	offset int
	lines  int
}

// NewScheduler builds the Scheduler tab.
func NewScheduler(*Context) *Scheduler { return &Scheduler{} }

// Name implements View.
func (v *Scheduler) Name() string { return model.TabScheduler }

// Title implements View.
func (v *Scheduler) Title() string { return "Scheduler" }

// Source implements View.
func (v *Scheduler) Source() string { return "sdiag" }

// Badge implements View.
func (v *Scheduler) Badge(*Context) string { return "" }

// Capturing implements View.
func (v *Scheduler) Capturing() bool { return false }

// Refresh implements View.
func (v *Scheduler) Refresh(*Context) {}

// Hints implements View.
func (v *Scheduler) Hints(ctx *Context) []key.Binding {
	return []key.Binding{ctx.Keys.Up, ctx.Keys.Down, ctx.Keys.Refresh}
}

// Update implements View: the arrows scroll.
func (v *Scheduler) Update(ctx *Context, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(k, ctx.Keys.Down):
		v.offset = min(v.offset+1, max(v.lines-1, 0))
	case key.Matches(k, ctx.Keys.Up):
		v.offset = max(v.offset-1, 0)
	}
	return nil
}

// Render implements View.
func (v *Scheduler) Render(ctx *Context, w, h int) string {
	th := ctx.Theme
	src := ctx.Store.Sched
	var out []string
	add := func(s ...string) { out = append(out, s...) }
	switch {
	case !src.Has && src.Err != nil:
		add("", " "+th.Warn.Render("sdiag is not available: "+layout.FirstLine(src.Err.Error())),
			" "+th.Muted.Render("Some sites restrict it to administrators. The other tabs are not affected;"),
			" "+th.Muted.Render("turn this tab off in Settings (,) if it stays unavailable."))
		return layout.FitLines(strings.Join(out, "\n"), w, h)
	case !src.Has:
		add("", " "+th.Muted.Render("Loading scheduler statistics"+th.Sym.Ellipsis))
		return layout.FitLines(strings.Join(out, "\n"), w, h)
	}
	s := src.Data
	mu := th.Pick("µ", "u")
	sep := th.Faint.Render(" " + th.Sym.Separator + " ")
	section := func(title, right string) {
		if len(out) > 0 {
			add("")
		}
		head := " " + th.Bold.Render(title)
		if right != "" {
			head += "  " + right
		}
		add(head, " "+th.HRule(max(w-2, 0)))
	}
	row := func(label, value string) {
		add("   " + th.Muted.Render(fmt.Sprintf("%-11s", label)) + " " + value)
	}

	section("Scheduler health", v.verdict(ctx, s))
	row("Jobs", fmt.Sprintf("%d running", s.Running)+sep+fmt.Sprintf("%d pending", s.Pending))
	if !s.Since.IsZero() {
		row("Since "+s.Since.Local().Format("15:04"), fmt.Sprintf("%d submitted", s.Submitted)+sep+fmt.Sprintf("%d started", s.Started)+sep+
			fmt.Sprintf("%d completed", s.Completed)+sep+fmt.Sprintf("%d failed", s.Failed)+sep+fmt.Sprintf("%d cancelled", s.Cancelled))
	}

	section("Main scheduler", th.Muted.Render("starts jobs at the front of the queue"))
	row("Cycle", "last "+micros(mu, s.Main.LastUS)+sep+"mean "+micros(mu, s.Main.MeanUS)+sep+"max "+micros(mu, s.Main.MaxUS))
	row("Pace", fmt.Sprintf("%d cycles a minute", s.Main.PerMinute)+sep+fmt.Sprintf("looks at %d jobs on average", s.Main.MeanDepth))

	section("Backfill", th.Muted.Render("starts smaller jobs in gaps, if they end in time"))
	if !s.Backfill.LastAt.IsZero() {
		row("Last run", units.FormatShort(max(ctx.Now.Sub(s.Backfill.LastAt), 0))+" ago, took "+micros(mu, s.Backfill.LastUS)+
			sep+"mean "+micros(mu, s.Backfill.MeanUS)+sep+"max "+micros(mu, s.Backfill.MaxUS))
	}
	row("Depth", fmt.Sprintf("looked at %d of %d jobs, tried %d", s.Backfill.LastDepth, s.Backfill.QueueLength, s.Backfill.LastTried))
	row("Started", fmt.Sprintf("%d jobs since the last reset, %d since slurmctld started", s.Backfill.Jobs, s.Backfill.JobsTotal))

	section("Controller load", "")
	row("Queues", fmt.Sprintf("%d server threads", s.ServerThreads)+sep+fmt.Sprintf("agent queue %d", s.AgentQueue)+sep+fmt.Sprintf("database queue %d", s.DBDQueue))
	for i, r := range s.RPCTypes[:min(len(s.RPCTypes), 5)] {
		label := ""
		if i == 0 {
			label = "Requests"
		}
		row(label, layout.Truncate(fmt.Sprintf("%-28s %9d  avg %s", strings.TrimPrefix(r.Name, "REQUEST_"), r.Count, micros(mu, r.AveUS)), max(w-16, 10), th.Sym.Ellipsis))
	}
	var total int64
	for _, r := range s.RPCUsers {
		total += r.Count
	}
	for i, r := range s.RPCUsers[:min(len(s.RPCUsers), 5)] {
		label := ""
		if i == 0 {
			label = "By user"
		}
		name := fmt.Sprintf("%-16s", r.Name)
		if r.Name == ctx.Store.User {
			name = th.Accent.Render(name)
		}
		share := ""
		if total > 0 {
			share = th.Muted.Render(fmt.Sprintf("  %d%%", r.Count*100/total))
		}
		row(label, name+fmt.Sprintf(" %9d", r.Count)+share)
	}

	v.lines = len(out)
	v.offset = min(v.offset, max(len(out)-h, 0))
	return layout.FitLines(strings.Join(out[v.offset:], "\n"), w, h)
}

// verdict is one plain sentence on how the scheduler is doing.
func (v *Scheduler) verdict(ctx *Context, s model.SchedStats) string {
	th := ctx.Theme
	switch {
	case !s.Backfill.LastAt.IsZero() && ctx.Now.Sub(s.Backfill.LastAt) > 10*time.Minute:
		return th.Warn.Render("backfill has not run for " + units.FormatShort(ctx.Now.Sub(s.Backfill.LastAt)) + ": small jobs may wait longer")
	case s.Backfill.LastUS > 60_000_000 || s.Main.MeanUS > 1_000_000:
		return th.Warn.Render("busy: scheduling cycles are slow, jobs may start later than usual")
	case s.AgentQueue > 100 || s.DBDQueue > 1000:
		return th.Warn.Render("the controller is backed up; commands may be slow")
	}
	return th.OK.Render("normal")
}

// micros renders sdiag's microseconds: "870 µs", "8.7 ms", "2.3 s".
func micros(mu string, us int64) string {
	switch {
	case us < 1000:
		return fmt.Sprintf("%d %ss", us, mu)
	case us < 1_000_000:
		return fmt.Sprintf("%.1f ms", float64(us)/1000)
	}
	return fmt.Sprintf("%.1f s", float64(us)/1_000_000)
}
