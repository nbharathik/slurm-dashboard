package ui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

type candidate struct{ Value, Desc string }

type paletteCommand struct {
	Name     string
	Aliases  []string
	Args     string
	Desc     string
	Complete func(a *App, arg int, prefix string) []candidate
	Run      func(a *App, args []string) (tea.Cmd, error)
}

var paletteCommands []paletteCommand

func init() {
	paletteCommands = []paletteCommand{
		{Name: "cancel", Aliases: []string{"kill", "scancel"}, Args: "<jobs>", Desc: "cancel jobs", Complete: completeJobs, Run: runJobAction("cancel")},
		{Name: "hold", Args: "<jobs>", Desc: "hold pending jobs", Complete: completeJobs, Run: runJobAction("hold")},
		{Name: "release", Args: "<jobs>", Desc: "release held jobs", Complete: completeJobs, Run: runJobAction("release")},
		{Name: "requeue", Args: "<jobs>", Desc: "requeue running jobs", Complete: completeJobs, Run: runJobAction("requeue")},
		{Name: "logs", Aliases: []string{"log", "out"}, Args: "[job] [--err]", Desc: "open the log viewer", Complete: completeJobs, Run: runLogs},
		{Name: "shell", Aliases: []string{"ssh"}, Args: "[job] [--node N]", Desc: "open a shell inside a running job", Complete: completeJobs, Run: runViewCommand("shell")},
		{Name: "gpu", Args: "[job]", Desc: "sample GPU use inside a job once", Complete: completeJobs, Run: runViewCommand("gpu")},
		{Name: "why", Args: "[job]", Desc: "why is this job pending?", Complete: completeJobs, Run: runViewCommand("why")},
		{Name: "eff", Aliases: []string{"seff"}, Args: "[job]", Desc: "efficiency and right-sizing", Complete: completeJobs, Run: runViewCommand("eff")},
		{Name: "script", Args: "[job]", Desc: "show the batch script", Complete: completeJobs, Run: runViewCommand("script")},
		{Name: "rerun", Aliases: []string{"resubmit"}, Args: "<job>", Desc: "submit one of your jobs again, with changes", Complete: completeJobs, Run: runViewCommand("rerun")},
		{Name: "filter", Args: "<expr>", Desc: "filter the table (state:R part:gpu name:~re ...)", Complete: completeFilter, Run: runFilter},
		{Name: "clear", Desc: "clear the filter", Run: func(a *App, _ []string) (tea.Cmd, error) { return runFilter(a, nil) }},
		{Name: "sort", Args: "<column> [asc|desc]", Desc: "sort the visible table", Complete: completeSort, Run: runSort},
		{Name: "mine", Desc: "your jobs (the Jobs tab)", Run: func(a *App, _ []string) (tea.Cmd, error) { a.openTab(model.TabJobs); return nil, nil }},
		{Name: "all", Aliases: []string{"queue"}, Desc: "everyone's jobs (the Queue tab)", Run: func(a *App, _ []string) (tea.Cmd, error) { a.openTab(model.TabQueue); return nil, nil }},
		{Name: "tab", Aliases: []string{"go"}, Args: "<name>", Desc: "switch tab", Complete: completeTabs, Run: runTab},
		{Name: "refresh", Args: "[source|all]", Desc: "refresh data now", Complete: completeSources, Run: runRefresh},
		{Name: "range", Args: "<1d|7d|30d>", Desc: "history range", Complete: fixed("1d", "7d", "30d"), Run: runRange},
		{Name: "theme", Args: "<auto|light|dark|high-contrast>", Desc: "switch theme for this session", Complete: fixed(theme.Auto, theme.Light, theme.Dark, theme.HighContrast), Run: runTheme},
		{Name: "settings", Aliases: []string{"set"}, Desc: "change refresh speed, theme and other settings", Run: runViewCommand("settings")},
		{Name: "config", Desc: "edit the config file", Run: runViewCommand("config")},
		{Name: "copy", Aliases: []string{"yank"}, Args: "<id|path|cmd>", Desc: "copy to the clipboard", Complete: fixed("id", "path", "cmd"), Run: runCopy},
		{Name: "doctor", Desc: "check the environment", Run: runViewCommand("doctor")},
		{Name: "fairshare", Aliases: []string{"priority", "sshare"}, Desc: "fairshare and the priority of your pending jobs", Run: runViewCommand("fairshare")},
		{Name: "help", Aliases: []string{"keys", "?"}, Args: "[command]", Desc: "show help", Complete: completeCommands, Run: runHelp},
		{Name: "quit", Aliases: []string{"exit", "q"}, Desc: "quit sdash", Run: func(a *App, _ []string) (tea.Cmd, error) { return a.quit(), nil }},
	}
}

func findCommand(name string) (paletteCommand, bool) {
	name = strings.ToLower(strings.TrimPrefix(name, "/"))
	for _, c := range paletteCommands {
		if c.Name == name || slices.Contains(c.Aliases, name) {
			return c, true
		}
	}
	return paletteCommand{}, false
}

func fixed(values ...string) func(*App, int, string) []candidate {
	return func(_ *App, arg int, _ string) []candidate {
		if arg > 0 {
			return nil
		}
		out := make([]candidate, len(values))
		for i, v := range values {
			out[i] = candidate{Value: v}
		}
		return out
	}
}

func jobCandidates(a *App) []candidate {
	out := []candidate{{Value: ".", Desc: "the job under the cursor (or the selection)"}}
	for _, j := range a.st.MyJobs.Data {
		out = append(out, candidate{Value: j.ID.Raw, Desc: j.Name + " · " + strings.ToLower(string(j.State))})
	}
	return append(out,
		candidate{Value: "@running", Desc: "all your running jobs"},
		candidate{Value: "@pending", Desc: "all your pending jobs"},
		candidate{Value: "@last", Desc: "the last job submitted from sdash"},
		candidate{Value: "@failed", Desc: "failed jobs in the history range"})
}

func completeJobs(a *App, arg int, _ string) []candidate {
	if arg > 0 {
		return nil
	}
	return jobCandidates(a)
}

func completeTabs(a *App, arg int, _ string) []candidate {
	if arg > 0 {
		return nil
	}
	var out []candidate
	for _, v := range a.views {
		out = append(out, candidate{Value: v.Name(), Desc: v.Title()})
	}
	return out
}

func completeSources(_ *App, arg int, _ string) []candidate {
	if arg > 0 {
		return nil
	}
	var out []candidate
	for _, s := range []string{"all", "myjobs", "alljobs", "cluster", "nodes", "partitions", "reservations", "history", "fairshare", "storage"} {
		out = append(out, candidate{Value: s})
	}
	return out
}

func completeCommands(_ *App, arg int, _ string) []candidate {
	if arg > 0 {
		return nil
	}
	var out []candidate
	for _, c := range paletteCommands {
		out = append(out, candidate{Value: c.Name, Desc: c.Desc})
	}
	return out
}

func completeSort(a *App, arg int, _ string) []candidate {
	if arg == 1 {
		return []candidate{{Value: "asc"}, {Value: "desc"}}
	}
	if arg > 1 {
		return nil
	}
	if s, ok := a.views[a.tab].(interface{ SortColumns() []string }); ok {
		var out []candidate
		for _, c := range s.SortColumns() {
			out = append(out, candidate{Value: c})
		}
		return out
	}
	return nil
}

func completeFilter(_ *App, _ int, _ string) []candidate {
	return []candidate{
		{Value: "state:", Desc: "R, PD, CG, F, ... (comma for several)"},
		{Value: "part:", Desc: "partition"},
		{Value: "user:", Desc: "user name"},
		{Value: "gpu:>0", Desc: "jobs with GPUs"},
		{Value: "name:~", Desc: "regular expression on the name"},
	}
}

func runJobAction(id string) func(*App, []string) (tea.Cmd, error) {
	return func(a *App, args []string) (tea.Cmd, error) {
		ref := ""
		if len(args) > 0 {
			ref = args[0]
		}
		jobs, err := a.resolveJobs(ref)
		if err != nil {
			return nil, err
		}
		return a.startAction(id, jobs, nil), nil
	}
}

func runLogs(a *App, args []string) (tea.Cmd, error) {
	ref, stderr := "", false
	for _, arg := range args {
		if arg == "--err" {
			stderr = true
		} else {
			ref = arg
		}
	}
	jobs, err := a.resolveJobs(ref)
	if err != nil {
		return nil, err
	}
	return views.Emit(views.LogMsg{Job: jobs[0], Stderr: stderr}), nil
}

// runViewCommand dispatches commands to their feature handlers.
func runViewCommand(name string) func(*App, []string) (tea.Cmd, error) {
	return func(a *App, args []string) (tea.Cmd, error) {
		if h, ok := commandHandlers[name]; ok {
			return h(a, args)
		}
		return nil, fmt.Errorf("/%s is not available in this build", name)
	}
}

// commandHandlers are filled in by the files that implement each feature.
var commandHandlers = map[string]func(*App, []string) (tea.Cmd, error){}

func runFilter(a *App, args []string) (tea.Cmd, error) {
	f, ok := a.views[a.tab].(interface{ SetFilter(string) error })
	if !ok {
		f, ok = a.viewByName(model.TabJobs).(interface{ SetFilter(string) error })
		a.switchTab(a.tabIndex(model.TabJobs))
	}
	if !ok {
		return nil, errors.New("this view has no filter")
	}
	err := f.SetFilter(strings.Join(args, " "))
	a.views[a.tab].Refresh(a.ctx)
	return nil, err
}

func runSort(a *App, args []string) (tea.Cmd, error) {
	s, ok := a.views[a.tab].(interface {
		SortBy(col string, desc bool) error
	})
	if !ok {
		return nil, errors.New("this view cannot be sorted")
	}
	if len(args) == 0 {
		return nil, errors.New("usage: /sort <column> [asc|desc]")
	}
	desc := len(args) > 1 && args[1] == "desc"
	err := s.SortBy(args[0], desc)
	a.views[a.tab].Refresh(a.ctx)
	return nil, err
}

func runTab(a *App, args []string) (tea.Cmd, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: /tab <name>")
	}
	if _, _, ok := model.ResolveTab(args[0]); ok {
		a.openTab(args[0])
		return nil, nil
	}
	for i, v := range a.views {
		if strings.EqualFold(v.Title(), args[0]) || args[0] == strconv.Itoa(i+1) {
			a.switchTab(i)
			return nil, nil
		}
	}
	return nil, fmt.Errorf("no tab %q (use %s)", args[0], strings.Join(model.AcceptedTabNames(), ", "))
}

func runRefresh(a *App, args []string) (tea.Cmd, error) {
	if len(args) == 0 {
		a.refresh(a.visibleSources()...)
	} else if args[0] == "all" {
		a.refresh()
	} else {
		a.refresh(args...)
	}
	a.setFlash("Refreshing", false)
	return nil, nil
}

func runRange(a *App, args []string) (tea.Cmd, error) {
	days := map[string]int{"1d": 1, "7d": 7, "30d": 30}
	if len(args) == 0 {
		return nil, errors.New("usage: /range <1d|7d|30d>")
	}
	d, ok := days[args[0]]
	if !ok {
		return nil, fmt.Errorf("range %q: use 1d, 7d or 30d", args[0])
	}
	a.switchTab(a.tabIndex(model.TabUsage))
	if r, ok := a.viewByName(model.TabUsage).(interface{ SetRange(*views.Context, int) }); ok {
		r.SetRange(a.ctx, d)
	}
	return views.Emit(views.HistoryRangeMsg{Days: d}), nil
}

func runTheme(a *App, args []string) (tea.Cmd, error) {
	if len(args) == 0 || !slices.Contains([]string{theme.Auto, theme.Light, theme.Dark, theme.HighContrast}, args[0]) {
		return nil, errors.New("usage: /theme <auto|light|dark|high-contrast>")
	}
	a.themeName = args[0]
	a.rebuildTheme()
	a.setFlash("Theme: "+args[0], false)
	if args[0] == theme.Auto {
		return tea.RequestBackgroundColor, nil
	}
	return nil, nil
}

func runCopy(a *App, args []string) (tea.Cmd, error) {
	what := "id"
	if len(args) > 0 {
		what = args[0]
	}
	c, ok := a.views[a.tab].(interface {
		CopyText(ctx *views.Context, what string) (string, error)
	})
	if !ok {
		c, ok = a.viewByName("jobs").(interface {
			CopyText(ctx *views.Context, what string) (string, error)
		})
	}
	if !ok {
		return nil, errors.New("nothing to copy here")
	}
	text, err := c.CopyText(a.ctx, what)
	if err != nil {
		return nil, err
	}
	return views.Emit(views.CopyMsg{Text: text, What: what}), nil
}

func runHelp(a *App, args []string) (tea.Cmd, error) {
	if len(args) > 0 {
		c, ok := findCommand(args[0])
		if !ok {
			return nil, fmt.Errorf("no command /%s", args[0])
		}
		a.setFlash(fmt.Sprintf("/%s %s: %s", c.Name, c.Args, c.Desc), false)
		return nil, nil
	}
	a.overlay = overlayHelp
	return nil, nil
}
