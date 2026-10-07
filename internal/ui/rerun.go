package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// SubmitTimeout bounds sbatch.
const SubmitTimeout = 30 * time.Second

type (
	rerunDataMsg struct {
		in  views.RerunInput
		err error
	}
	estimateMsg struct {
		text string
		err  error
	}
	submittedMsg struct {
		id, from string
		err      error
	}
	editedScriptMsg struct {
		script string
		err    error
	}
)

// rerunReq is a rerun waiting for confirmation.
type rerunReq struct {
	Argv               []string
	Script, Dir, Title string
}

// openRerun fetches what the rerun form needs: the stored script, the
// recorded command line (Slurm 23.02+) and the right-size suggestions.
func (a *App) openRerun(id string) tea.Cmd {
	src := a.opt.Sources
	if src == nil {
		return nil
	}
	in := views.RerunInput{ID: id}
	own := false
	if h, ok := a.historyJob(id); ok {
		own, in.Name, in.Dir = true, h.Name, h.WorkDir
		in.Suggest = insights.Suggested(insights.RightSize(h))
	}
	for _, j := range a.st.MyJobs.Data {
		if j.ID.Raw == id {
			own, in.Name = true, j.Name
		}
	}
	if !own {
		a.setFlash("Rerun works on your own jobs; "+id+" is not in your jobs or history", true)
		return nil
	}
	withLine := a.opt.Demo || a.st.Caps.AtLeast(23, 2)
	site := a.st.Site
	a.setFlash("Loading the batch script of "+id+a.th.Sym.Ellipsis, false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.runCtx, lookupTimeout)
		defer cancel()
		script, err := src.BatchScript(ctx, id)
		if err != nil {
			if site.NoJobScripts() {
				return rerunDataMsg{err: fmt.Errorf("this cluster does not store job scripts (AccountingStoreFlags lacks job_script) and Slurm no longer remembers job %s", id)}
			}
			return rerunDataMsg{err: fmt.Errorf("the batch script of %s is not available: %w", id, err)}
		}
		in.Script = script
		if withLine {
			if line, dir, err := src.SubmitLine(ctx, id); err == nil {
				in.Line = line
				if in.Dir == "" {
					in.Dir = dir
				}
			}
		}
		if in.Dir == "" {
			return rerunDataMsg{err: fmt.Errorf("the working directory of %s is unknown", id)}
		}
		return rerunDataMsg{in: in}
	}
}

// handleRerunMsg handles the rerun flow.
func (a *App) handleRerunMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case views.RerunMsg:
		return a.openRerun(m.ID), true
	case rerunDataMsg:
		if m.err != nil {
			a.setFlash("rerun: "+layout.FirstLine(m.err.Error()), true)
			return nil, true
		}
		a.closeLog()
		a.flash = flash{}
		a.rerun = views.NewRerun(m.in)
		a.syncVisible()
	case views.CloseRerunMsg:
		a.rerun = nil
		a.syncVisible()
	case views.RerunEstimateMsg:
		r := a.opt.Runner
		if r == nil {
			return nil, true
		}
		if a.rerun == nil {
			return nil, true
		}
		origin := a.rerun.ID
		now := a.now()
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(a.runCtx, SubmitTimeout)
			defer cancel()
			if err := a.opt.Sources.RequireOwn(ctx, origin); err != nil {
				return estimateMsg{err: err}
			}
			est, err := actions.Estimate(ctx, r, m.Script, m.Dir, m.Opts)
			if err != nil {
				return estimateMsg{err: err}
			}
			return estimateMsg{text: fmt.Sprintf("Would start %s on %s in partition %s (scheduler estimate).", friendly(now, est.Start), est.Nodes, est.Partition)}
		}, true
	case estimateMsg:
		if a.rerun != nil {
			a.rerun.Busy = false
			if m.err != nil {
				a.rerun.Estimate, a.rerun.EstErr = layout.FirstLine(m.err.Error()), true
			} else {
				a.rerun.Estimate, a.rerun.EstErr = m.text, false
			}
		}
	case views.RerunRequestMsg:
		a.confirm = &Confirm{Rerun: &rerunReq{Argv: m.Argv, Script: m.Script, Dir: m.Dir, Title: m.Title}}
	case views.RerunEditMsg:
		return a.editScript(m.ID, m.Script), true
	case editedScriptMsg:
		switch {
		case m.err != nil:
			a.setFlash("edit: "+layout.FirstLine(m.err.Error()), true)
		case a.rerun != nil:
			a.rerun.SetScript(m.script)
			a.setFlash("Using the edited script (the stored one is unchanged)", false)
		}
	case submittedMsg:
		if m.err != nil {
			a.setFlash(layout.FirstLine(m.err.Error()), true)
			return nil, true
		}
		a.rerun = nil
		a.syncVisible()
		a.state.LastSubmitted = m.id
		saveState(a.opt.StateDir, a.state)
		a.setFlash("Submitted job "+m.id+" (rerun of "+m.from+"; @last)", false)
		a.refresh("myjobs")
		return schedule(1500*time.Millisecond, func(time.Time) tea.Msg { return views.OpenJobMsg{ID: m.id} }), true
	default:
		return nil, false
	}
	return nil, true
}

// acceptRerun submits a confirmed rerun.
func (a *App) acceptRerun(req *rerunReq) tea.Cmd {
	r := a.opt.Runner
	if r == nil {
		a.setFlash("submitting is not available", true)
		return nil
	}
	from, _, _ := strings.Cut(req.Title, " ")
	a.setFlash("Submitting"+a.th.Sym.Ellipsis, false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(a.runCtx, SubmitTimeout)
		defer cancel()
		if err := a.opt.Sources.RequireOwn(ctx, from); err != nil {
			return submittedMsg{from: from, err: err}
		}
		s, err := actions.Rerun(ctx, r, req.Argv, req.Script, req.Dir)
		return submittedMsg{id: s.JobID, from: from, err: err}
	}
}

// editScript opens a private copy of the script in $VISUAL or $EDITOR.
func (a *App) editScript(id, script string) tea.Cmd {
	if a.opt.Demo {
		a.setFlash("Editing the script is off in the demo", false)
		return nil
	}
	dir := a.opt.StateDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		a.setFlash("edit: "+err.Error(), true)
		return nil
	}
	f, err := os.CreateTemp(dir, "rerun-"+id+"-*.sbatch") // mode 0600
	if err != nil {
		a.setFlash("edit: "+err.Error(), true)
		return nil
	}
	path := f.Name()
	_, werr := f.WriteString(script)
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(path)
		a.setFlash("edit: "+err.Error(), true)
		return nil
	}
	editor := strings.Fields(firstNonEmpty(a.opt.Getenv("VISUAL"), a.opt.Getenv("EDITOR"), "vi"))
	cmd, err := execx.NewInteractive("", append(editor, path)...)
	if err != nil {
		_ = os.Remove(path)
		a.setFlash(err.Error(), true)
		return nil
	}
	return tea.Exec(cmd, func(err error) tea.Msg {
		b, rerr := os.ReadFile(path) //nolint:gosec // our own temporary file
		_ = os.Remove(path)
		return editedScriptMsg{script: string(b), err: errors.Join(err, rerr)}
	})
}

func friendly(now, t time.Time) string {
	t, now = t.Local(), now.Local()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "today at " + t.Format("15:04")
	case t.Sub(now) < 48*time.Hour && t.After(now):
		return t.Format("Mon 15:04")
	}
	return t.Format("Jan 2 15:04")
}
