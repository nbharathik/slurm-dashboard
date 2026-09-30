package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	osc52 "github.com/aymanbagabas/go-osc52/v2"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// ActionTimeout bounds one action's commands.
const ActionTimeout = 30 * time.Second

type (
	actionDoneMsg struct {
		res  actions.Result
		jobs int
	}
	delayedRefreshMsg struct{ sources []string }
)

var registry = actions.Default()

// handleRequest acts on messages from views and background commands.
func (a *App) handleRequest(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case views.FlashMsg:
		a.setFlash(msg.Text, msg.Err)
	case views.RefreshMsg:
		a.refresh(msg.Sources...)
	case delayedRefreshMsg:
		a.refresh(msg.sources...)
	case views.SwitchTabMsg:
		a.openTab(msg.Tab)
	case views.StartEstimateMsg:
		return a.startEstimate(msg.Job)
	case startEstimateMsg:
		a.showEstimate(msg)
	case views.OpenJobMsg:
		a.switchTab(a.tabIndex(model.TabJobs))
		if o, ok := a.views[a.tab].(interface{ Open(*views.Context, string) }); ok {
			o.Open(a.ctx, msg.ID)
		}
	case views.HistoryJobMsg:
		a.switchTab(a.tabIndex(model.TabUsage))
		if o, ok := a.views[a.tab].(interface{ Open(*views.Context, string) }); ok {
			o.Open(a.ctx, msg.ID)
		}
	case views.NodeMsg:
		a.switchTab(a.tabIndex(model.TabNodes))
		if o, ok := a.views[a.tab].(interface{ Open(*views.Context, string) }); ok {
			o.Open(a.ctx, msg.Name)
		}
	case views.PrefMsg:
		if a.state.Prefs[msg.Key] != msg.Value {
			a.state.Prefs[msg.Key] = msg.Value
			saveState(a.opt.StateDir, a.state)
		}
	case views.DismissMsg:
		a.state.Dismissed[msg.Key] = a.now().Add(insights.DismissFor)
		saveState(a.opt.StateDir, a.state)
		for _, v := range a.views {
			v.Refresh(a.ctx)
		}
		a.setFlash("Alert hidden for 24 h", false)
	case views.ActionMsg:
		return a.startAction(msg.Action, msg.Jobs, msg.Args)
	case actionDoneMsg:
		if msg.res.Err != nil {
			a.setFlash(msg.res.Summary(msg.jobs), true)
		} else {
			text := msg.res.Summary(msg.jobs)
			if !msg.res.Action.Destructive {
				text += "  (" + actions.Command(msg.res.Argvs) + ")"
			}
			a.setFlash(text, false)
		}
		// Slurm state lags: refresh now and again 2 s later.
		a.refresh("myjobs")
		return schedule(2*time.Second, func(time.Time) tea.Msg { return delayedRefreshMsg{sources: []string{"myjobs", "cluster"}} })
	case views.CopyMsg:
		a.setFlash("Copied "+msg.What, false)
		seq := osc52.New(msg.Text)
		switch {
		case a.opt.Getenv("TMUX") != "":
			seq = seq.Tmux()
		case a.opt.Getenv("STY") != "":
			seq = seq.Screen()
		}
		return tea.Raw(seq.String())
	case views.PaletteMsg:
		a.openPalette(msg.Text)
	case views.AnalyseMsg:
		return a.analyse(msg)
	case views.CancelAnalyseMsg:
		if a.duCancel != nil {
			a.duCancel()
		}
	case duDoneMsg:
		a.duDone(msg)
	case views.RunCommandMsg:
		cmd, err := a.runPalette(msg.Line)
		if err != nil {
			a.setFlash(err.Error(), true)
		}
		return cmd
	case views.HistoryRangeMsg, views.DetailMsg:
		return a.handleViewState(msg)
	case views.LogMsg:
		return a.openLogs(msg.Job, msg.Stderr, false)
	case views.PagerMsg:
		return a.openLogs(msg.Job, msg.Stderr, true)
	case views.ScriptMsg:
		return a.loadScript(msg.Job)
	case views.ShellMsg:
		return a.openShell(msg.Job, msg.Node)
	case views.GPUSampleMsg:
		return a.sampleGPU(msg.Job)
	default:
		if cmd, ok := a.handleRerunMsg(msg); ok {
			return cmd
		}
		return a.handleLogMsg(msg)
	}
	return nil
}

// startAction validates, builds and (after confirmation when destructive)
// runs an action on the user's own jobs.
func (a *App) startAction(id string, jobs []model.Job, args map[string]string) tea.Cmd {
	act, ok := registry.Get(id)
	if !ok {
		a.setFlash("unknown action "+id, true)
		return nil
	}
	var mine []model.Job
	skipped := 0
	for _, j := range jobs {
		if j.User != "" && a.st.User != "" && j.User != a.st.User {
			skipped++
			continue
		}
		if act.AppliesTo(j) {
			mine = append(mine, j)
		} else {
			skipped++
		}
	}
	if len(mine) == 0 {
		if skipped > 0 {
			a.setFlash(fmt.Sprintf("%s does not apply to the selected job(s)", strings.ToLower(act.Title)), true)
		} else {
			a.setFlash("no job selected", true)
		}
		return nil
	}
	argvs, err := act.Build(mine, actions.Args(args))
	if err != nil {
		a.setFlash(err.Error(), true)
		return nil
	}
	if act.Destructive {
		a.confirm = &Confirm{Action: act, Jobs: mine, Argvs: argvs}
		return nil
	}
	return a.runAction(act, mine, argvs)
}

func (a *App) runAction(act actions.Action, jobs []model.Job, argvs [][]string) tea.Cmd {
	r := a.opt.Runner
	if r == nil {
		a.setFlash("actions are not available", true)
		return nil
	}
	n := len(jobs)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), ActionTimeout)
		defer cancel()
		return actionDoneMsg{res: actions.Run(ctx, r, act, argvs), jobs: n}
	}
}

// handleViewState forwards view-state requests to the collectors.
func (a *App) handleViewState(msg tea.Msg) tea.Cmd {
	if a.opt.OnViewState != nil {
		a.opt.OnViewState(msg)
	}
	switch m := msg.(type) {
	case views.HistoryRangeMsg:
		a.refresh("history")
	case views.DetailMsg:
		if m.ID != "" {
			a.refresh("jobdetail")
		}
	}
	return nil
}
