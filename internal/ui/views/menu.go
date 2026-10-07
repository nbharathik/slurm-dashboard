package views

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

type menuItem struct {
	label string
	key   string
	msg   tea.Msg
}

// jobMenu is the per-row action menu (m or right-click).
type jobMenu struct {
	title string
	items []menuItem
	sel   int
}

func zoneMenuItem(i int) string { return "jobs:menu:" + strconv.Itoa(i) }

// newJobMenu lists what can be done with the jobs.
func newJobMenu(jobs []model.Job, me string) *jobMenu {
	j := jobs[0]
	title := "Job " + j.ID.Raw
	if len(jobs) > 1 {
		title = strconv.Itoa(len(jobs)) + " jobs"
	}
	own := j.OwnedBy(me)
	m := &jobMenu{title: title}
	add := func(label, k string, msg tea.Msg) { m.items = append(m.items, menuItem{label, k, msg}) }
	action := func(id string) ActionMsg { return ActionMsg{Action: id, Jobs: jobs} }
	if own {
		switch j.State {
		case model.StateRunning:
			add("Cancel", "c", action("cancel"))
			add("Requeue", "Q", action("requeue"))
			add("Open shell", "t", ShellMsg{Job: j})
			add("Sample GPU use", "g", GPUSampleMsg{Job: j})
		case model.StatePending:
			add("Cancel", "c", action("cancel"))
			if strings.HasPrefix(j.Reason, "JobHeld") {
				add("Release", "u", action("release"))
			} else {
				add("Hold", "h", action("hold"))
			}
		}
	}
	if own {
		add("Show output log", "l", LogMsg{Job: j})
		add("Show error log", "e", LogMsg{Job: j, Stderr: true})
		add("Show batch script", "v", ScriptMsg{Job: j})
	}
	add("Copy job ID", "y", CopyMsg{Text: j.ID.Raw, What: "job ID"})
	return m
}

func (m *jobMenu) update(msg tea.KeyPressMsg) (tea.Msg, bool) {
	switch msg.String() {
	case "esc", "m", "q":
		return nil, true
	case "up", "k":
		m.sel = max(m.sel-1, 0)
	case "down", "j":
		m.sel = min(m.sel+1, len(m.items)-1)
	case "enter":
		return m.items[m.sel].msg, true
	default:
		for _, it := range m.items {
			if it.key != "" && it.key == msg.String() {
				return it.msg, true
			}
		}
	}
	return nil, false
}

func (m *jobMenu) render(ctx *Context) string {
	th := ctx.Theme
	width := 0
	for _, it := range m.items {
		width = max(width, layout.Width(it.label)+4)
	}
	var lines []string
	for i, it := range m.items {
		row := layout.Pad(it.label, width-2, false, th.Sym.Ellipsis) + " " + th.Key.Render(layout.Pad(it.key, 1, false, ""))
		if i == m.sel {
			row = th.Selected.Render(layout.Pad(it.label, width-2, false, "") + " " + layout.Pad(it.key, 1, false, ""))
		}
		lines = append(lines, ctx.Mark(zoneMenuItem(i), row))
	}
	return components.Modal(th, m.title, strings.Join(lines, "\n"), nil, width+6)
}

var _ = theme.Theme{}
