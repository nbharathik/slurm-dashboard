package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/storage"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

type duDoneMsg struct {
	path  string
	usage model.DiskUsage
	err   error
}

// customConfirm is a yes/no question that runs a command on yes.
type customConfirm struct {
	title, body, yes string
	run              func() tea.Cmd
}

// analyse starts the disk-usage analyser, confirming first on shared
// parallel filesystems; a result under an hour old is reused unless fresh.
func (a *App) analyse(m views.AnalyseMsg) tea.Cmd {
	if a.duCancel != nil {
		a.setFlash("an analysis is already running; esc cancels it", true)
		return nil
	}
	if u, ok := a.st.DiskUsage[m.Path]; ok && !m.Fresh && u.Err == "" && a.now().Sub(u.At) < storage.DuCacheTTL {
		return nil
	}
	argv, err := storage.DuArgv(m.Path, a.opt.HasIonice)
	if err != nil {
		a.setFlash(err.Error(), true)
		return nil
	}
	start := func() tea.Cmd { return a.runDu(m.Path, argv) }
	if storage.NeedsConfirm(m.FSType) {
		a.confirm = &Confirm{Custom: &customConfirm{
			title: "Scan " + m.Path + "?",
			body: "This is a " + m.FSType + " filesystem: walking it loads the shared metadata server that everyone uses.\n" +
				"sdash runs du at the lowest priority, stops after 10 minutes, and esc cancels it.\n\nWill run:\n  " + strings.Join(argv, " "),
			yes: "Scan (y)", run: start,
		}}
		return nil
	}
	return start()
}

func (a *App) runDu(path string, argv []string) tea.Cmd {
	r := a.opt.DuRunner
	if r == nil {
		a.setFlash("the disk-usage analyser is not available", true)
		return nil
	}
	if a.st.DiskUsage == nil {
		a.st.DiskUsage = map[string]model.DiskUsage{}
	}
	prev := a.st.DiskUsage[path]
	prev.Path, prev.Running = path, true
	a.st.DiskUsage[path] = prev
	a.refreshStorageAnalysis()
	ctx, cancel := context.WithTimeout(a.runCtx, storage.DuTimeout)
	a.duCancel = cancel
	now := a.now
	started := now()
	return func() tea.Msg {
		defer cancel()
		res, err := r.Run(ctx, argv...)
		u, perr := storage.ParseDu(res.Stdout, path)
		u.At = now()
		u.Took = u.At.Sub(started)
		switch {
		case ctx.Err() != nil:
			// Cancelled or timed out: du prints each directory as it
			// finishes, so the finished ones are still worth showing.
			reason := "cancelled"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = "stopped after " + units.FormatShort(storage.DuTimeout)
			}
			if perr != nil || len(u.Entries) == 0 {
				return duDoneMsg{path: path, usage: u, err: errors.New(reason)}
			}
			if u.Total == 0 {
				for _, e := range u.Entries {
					u.Total += e.Bytes
				}
			}
			u.Partial, u.Err, err = true, reason, nil
		case err != nil && perr == nil:
			// du exits 1 when some directories are unreadable.
			u.Partial, err = true, nil
		case err == nil && perr != nil:
			err = perr
		}
		return duDoneMsg{path: path, usage: u, err: err}
	}
}

func (a *App) duDone(m duDoneMsg) {
	defer a.refreshStorageAnalysis()
	a.duCancel = nil
	u := m.usage
	if m.err != nil {
		prev := a.st.DiskUsage[m.path]
		prev.Running, prev.Err = false, layout.FirstLine(m.err.Error())
		a.st.DiskUsage[m.path] = prev
		a.setFlash("analysis of "+m.path+": "+prev.Err, true)
		return
	}
	a.st.DiskUsage[m.path] = u
	if u.Err != "" {
		a.setFlash(fmt.Sprintf("Analysis of %s %s; showing the %d directories finished so far", m.path, u.Err, len(u.Entries)), true)
		return
	}
	a.setFlash("Analysed "+m.path, false)
}

func (a *App) refreshStorageAnalysis() {
	for i, v := range a.views {
		if v.Name() != "storage" && v.Name() != "overview" {
			continue
		}
		if i == a.tab {
			v.Refresh(a.ctx)
		} else {
			a.dirty[v.Name()] = true
		}
	}
}
