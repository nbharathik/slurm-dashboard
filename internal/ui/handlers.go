package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

func init() {
	commandHandlers["shell"] = cmdShell
	commandHandlers["gpu"] = cmdGPU
	commandHandlers["why"] = cmdWhy
	commandHandlers["eff"] = cmdEff
	commandHandlers["script"] = cmdScript
	commandHandlers["config"] = cmdConfig
	commandHandlers["settings"] = func(a *App, _ []string) (tea.Cmd, error) { a.openSettings(); return nil, nil }
	commandHandlers["doctor"] = cmdDoctor
	commandHandlers["fairshare"] = cmdFairshare
	commandHandlers["rerun"] = cmdRerun
}

// oneJob resolves a single job reference. When it names an array with
// several elements in the queue, the first element that prefer accepts is
// used (nil accepts any).
func (a *App) oneJob(ref string, prefer func(model.Job) bool) (model.Job, error) {
	jobs, err := a.resolveJobs(ref)
	if err != nil {
		return model.Job{}, err
	}
	if len(jobs) == 1 {
		return jobs[0], nil
	}
	sameArray := jobs[0].ID.IsArray()
	for _, j := range jobs {
		sameArray = sameArray && j.ID.ArrayJobID == jobs[0].ID.ArrayJobID
	}
	if sameArray {
		for _, j := range jobs {
			if prefer == nil || prefer(j) {
				return j, nil
			}
		}
		ids := make([]string, len(jobs))
		for i, j := range jobs {
			ids[i] = j.ID.Raw
		}
		return model.Job{}, fmt.Errorf("job %s is an array; give one of %s", ref, strings.Join(ids, ", "))
	}
	return model.Job{}, fmt.Errorf("give one job, not %d", len(jobs))
}

func isRunning(j model.Job) bool { return j.State == model.StateRunning }
func isPending(j model.Job) bool { return j.State == model.StatePending }

// historyJob finds a finished job in the loaded history.
func (a *App) historyJob(id string) (model.HistoryJob, bool) {
	for _, h := range a.st.History.Data.Jobs {
		if h.ID.Raw == id {
			return h, true
		}
	}
	return model.HistoryJob{}, false
}

func cmdShell(a *App, args []string) (tea.Cmd, error) {
	ref, node := "", ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--node" && i+1 < len(args):
			node = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--node="):
			node = strings.TrimPrefix(args[i], "--node=")
		default:
			ref = args[i]
		}
	}
	j, err := a.oneJob(ref, isRunning)
	if err != nil {
		return nil, err
	}
	return a.openShell(j, node), nil
}

func cmdGPU(a *App, args []string) (tea.Cmd, error) {
	j, err := a.oneJob(strings.Join(args, ""), isRunning)
	if err != nil {
		return nil, err
	}
	return a.sampleGPU(j), nil
}

func cmdWhy(a *App, args []string) (tea.Cmd, error) {
	j, err := a.oneJob(strings.Join(args, ""), isPending)
	if err != nil {
		return nil, err
	}
	if j.State != model.StatePending {
		return nil, fmt.Errorf("job %s is %s, not pending", j.ID.Raw, strings.ToLower(string(j.State)))
	}
	return views.Emit(views.OpenJobMsg{ID: j.ID.Raw}), nil
}

func cmdEff(a *App, args []string) (tea.Cmd, error) {
	ref := strings.Join(args, "")
	if ref == "" || ref == "." {
		if v, ok := a.views[a.tab].(interface {
			Selected() (model.HistoryJob, bool)
		}); ok {
			if h, ok := v.Selected(); ok {
				return views.Emit(views.HistoryJobMsg{ID: h.ID.Raw}), nil
			}
		}
		return nil, errors.New("give the ID of a finished job")
	}
	if _, ok := a.historyJob(ref); ok {
		return views.Emit(views.HistoryJobMsg{ID: ref}), nil
	}
	return nil, fmt.Errorf("job %s is not in the loaded history (try /range 30d)", ref)
}

func cmdScript(a *App, args []string) (tea.Cmd, error) {
	ref := strings.Join(args, "")
	if h, ok := a.historyJob(ref); ok && ref != "" {
		return a.loadScript(model.Job{ID: h.ID, Name: h.Name, User: a.st.User}), nil
	}
	j, err := a.oneJob(ref, nil)
	if err != nil {
		return nil, err
	}
	return a.loadScript(j), nil
}

// cmdConfig edits the config file in $EDITOR, then reloads what can
// change while running: keys, theme and UI settings.
func cmdConfig(a *App, _ []string) (tea.Cmd, error) {
	path := a.opt.ConfigPath
	if path == "" {
		return nil, errors.New("no config file path known")
	}
	if a.opt.Demo {
		return nil, errors.New("/config is not available in demo mode")
	}
	editor := strings.Fields(firstNonEmpty(a.opt.Getenv("VISUAL"), a.opt.Getenv("EDITOR"), "vi"))
	cmd, err := execx.NewInteractive("", append(editor, path)...)
	if err != nil {
		return nil, err
	}
	return tea.Exec(cmd, func(err error) tea.Msg { return configEditedMsg{err: err} }), nil
}

type configEditedMsg struct{ err error }

func (a *App) reloadConfig(m configEditedMsg) tea.Cmd {
	if m.err != nil {
		a.setFlash("editor: "+layout.FirstLine(m.err.Error()), true)
		return nil
	}
	layers := a.opt.ConfigLayers
	if len(layers) == 0 {
		layers = []string{a.opt.ConfigPath}
	}
	cfg, issues, err := config.LoadLayers(layers, a.opt.Getenv)
	if err != nil {
		a.setFlash("config: "+err.Error(), true)
		return nil
	}
	a.applyConfig(cfg)
	if config.HasErrors(issues) {
		a.setFlash(fmt.Sprintf("config reloaded with %d problem(s); run sdash config", len(issues)), true)
	} else {
		a.setFlash("Config reloaded", false)
	}
	return nil
}

func cmdDoctor(a *App, _ []string) (tea.Cmd, error) {
	if a.opt.Doctor == nil {
		return nil, errors.New("doctor is not available here; run sdash doctor")
	}
	a.setFlash("Running checks"+a.th.Sym.Ellipsis, false)
	fn := a.opt.Doctor
	return func() tea.Msg { return doctorMsg{text: fn()} }, nil
}

type doctorMsg struct{ text string }

func cmdRerun(a *App, args []string) (tea.Cmd, error) {
	ref := strings.Join(args, "")
	if ref == "" || ref == "." {
		if v, ok := a.views[a.tab].(interface {
			Selected() (model.HistoryJob, bool)
		}); ok {
			if h, ok := v.Selected(); ok {
				ref = h.ID.Raw
			}
		}
	}
	if ref == "" || ref == "." {
		return nil, errors.New("give the ID of the job to rerun")
	}
	return views.Emit(views.RerunMsg{ID: ref}), nil
}

// cmdFairshare shows sshare and the priority factors of pending jobs.
func cmdFairshare(a *App, _ []string) (tea.Cmd, error) {
	th := a.th
	var b strings.Builder
	shares := a.st.Fairshare.Data
	switch {
	case !a.st.Caps.HasSshare:
		b.WriteString(th.Muted.Render("sshare is not available on this cluster.") + "\n")
	case len(shares) == 0:
		b.WriteString(th.Muted.Render("No fairshare data yet.") + "\n")
	}
	for _, s := range shares {
		who := s.User
		if who == "" {
			who = "(account)"
		}
		fmt.Fprintf(&b, "%-12s %-10s fairshare %.3f  usage %5.1f%%  shares %5.1f%%\n", s.Account, who, s.FairShare, 100*s.EffectiveUsage, 100*s.NormShares)
	}
	if len(shares) > 0 {
		b.WriteString(th.Muted.Render("Fairshare falls as your usage grows past your shares; 1.0 is best.") + "\n")
	}
	if pfs := a.st.Priorities.Data; len(pfs) > 0 {
		b.WriteString("\n" + th.Bold.Render("Pending jobs") + "\n")
		for _, pf := range pfs {
			fmt.Fprintf(&b, "%-10s priority %s\n", pf.JobID, insights.PriorityLine(pf))
		}
	}
	a.info = &infoBox{title: "Fairshare and priority", body: strings.TrimRight(b.String(), "\n")}
	return nil, nil
}
