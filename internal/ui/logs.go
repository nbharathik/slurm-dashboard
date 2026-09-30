package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/actions"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// Log polling: 1 s while the file exists, 5 s while it does not.
const (
	logPoll        = time.Second
	logPollMissing = 5 * time.Second
	lookupTimeout  = 15 * time.Second
	gpuTimeout     = 20 * time.Second
)

type (
	// logTargetMsg carries the resolved log paths of a job.
	logTargetMsg struct {
		job      model.Job
		stderr   bool
		pager    bool
		out, err string
		note     string
		fail     error
	}
	logChunkMsg struct {
		gen   int
		chunk logs.Chunk
	}
	logTickMsg struct{ gen int }
	scriptMsg  struct {
		job  model.Job
		text string
		err  error
	}
	gpuSampleMsg struct {
		job     model.Job
		argv    []string
		samples []actions.GPUSample
		err     error
	}
	execDoneMsg struct {
		what string
		err  error
	}
)

// infoBox is a read-only modal (GPU samples, command output).
type infoBox struct {
	title, body string
	anyKey      bool // any key closes it (the welcome card)
}

// detailOf returns the loaded scontrol detail of j, if any.
func (a *App) detailOf(j model.Job) *model.JobDetail {
	d := a.st.Detail.Data
	if !a.st.Detail.Has || d.Job == nil {
		return nil
	}
	if d.ID == j.ID.Raw || d.Job.ID.Raw == j.ID.Raw {
		return d.Job
	}
	return nil
}

// ownJobID is the job's own ID: a running array task has one that differs
// from "812_3", and srun --jobid wants it.
func (a *App) ownJobID(j model.Job) string {
	if d := a.detailOf(j); d != nil {
		if id := d.Raw["JobId"]; id != "" && actions.ValidateJobID(id) == nil {
			return id
		}
	}
	return j.ID.Raw
}

// openLogs resolves a job's log paths, then opens the viewer (or $PAGER).
func (a *App) openLogs(j model.Job, stderr, pager bool) tea.Cmd {
	if d := a.detailOf(j); d != nil {
		return a.showLog(logTargetMsg{job: j, stderr: stderr, pager: pager, out: views.LogPath(d, false), err: views.LogPath(d, true)})
	}
	src := a.opt.Sources
	if src == nil {
		a.setFlash("log paths are unknown", true)
		return nil
	}
	a.setFlash("Finding the log files of "+j.ID.Raw+a.th.Sym.Ellipsis, false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
		defer cancel()
		d, err := src.JobDetail(ctx, j.ID.Raw)
		m := logTargetMsg{job: j, stderr: stderr, pager: pager, fail: err}
		if d != nil {
			m.out, m.err = views.LogPath(d, false), views.LogPath(d, true)
		}
		return m
	}
}

// guessLogPath is Slurm's default output file for a finished job whose
// --output is no longer known.
func (a *App) guessLogPath(j model.Job) (string, bool) {
	for _, h := range a.st.History.Data.Jobs {
		if h.ID.Raw == j.ID.Raw && h.WorkDir != "" {
			name := "slurm-" + j.ID.Raw + ".out"
			if h.ID.IsArray() {
				name = "slurm-" + strconv.FormatUint(h.ID.ArrayJobID, 10) + "_" + strings.Trim(h.ID.TaskSpec, "[]") + ".out"
			}
			return filepath.Join(h.WorkDir, name), true
		}
	}
	return "", false
}

func (a *App) showLog(m logTargetMsg) tea.Cmd {
	if m.out == "" && m.fail == nil {
		if p, ok := a.guessLogPath(m.job); ok {
			m.out, m.err = p, p
			m.note = "guessed Slurm's default file name; the job's --output is no longer known"
		}
	}
	if m.out == "" {
		msg := "no log path known for " + m.job.ID.Raw
		if m.fail != nil {
			msg += ": " + layout.FirstLine(m.fail.Error())
		}
		a.setFlash(msg, true)
		return nil
	}
	path := m.out
	if m.stderr && m.err != "" {
		path = m.err
	}
	if m.pager {
		return a.execPager(path)
	}
	hl, _ := logs.NewHighlighter(nil, nil)
	stream := "stdout"
	if m.stderr && m.err != m.out {
		stream = "stderr"
	}
	v := views.NewLogView(fmt.Sprintf("%s %s %s %s", stream, a.th.Sym.Separator, m.job.ID.Raw, m.job.Name),
		logs.NewBuffer(logs.MaxLines, false, hl), false)
	v.Job, v.Stderr, v.Path, v.Note = m.job, m.stderr, path, m.note
	a.logView, a.logPaths = v, [2]string{m.out, m.err}
	a.flash = flash{}
	a.logGen++
	a.logReader = logs.NewReader(a.opt.LogFS, path, logs.InitialBytes)
	return a.pollLog()
}

func (a *App) pollLog() tea.Cmd {
	gen, r := a.logGen, a.logReader
	return func() tea.Msg { return logChunkMsg{gen: gen, chunk: r.Poll()} }
}

func (a *App) closeLog() {
	a.logView, a.logReader = nil, nil
	a.logGen++
}

// handleLogMsg applies log viewer messages.
func (a *App) handleLogMsg(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case logTargetMsg:
		return a.showLog(m)
	case logChunkMsg:
		if m.gen != a.logGen || a.logView == nil {
			return nil
		}
		v, c := a.logView, m.chunk
		if c.Reset {
			v.Buf.Reset()
		}
		v.Buf.Write(c.Data)
		v.State, v.Err = c.State, ""
		if c.Err != nil {
			v.Err = c.Err.Error()
		}
		switch {
		case c.More:
			return a.pollLog()
		case c.State != logs.Reading:
			return a.logTick(logPollMissing)
		}
		return a.logTick(logPoll)
	case logTickMsg:
		if m.gen == a.logGen && a.logView != nil {
			return a.pollLog()
		}
	case views.CloseLogMsg:
		a.closeLog()
	case views.LogStreamMsg:
		if a.logView == nil {
			return nil
		}
		if a.logPaths[0] == a.logPaths[1] || a.logPaths[1] == "" {
			a.setFlash("stdout and stderr go to the same file", false)
			return nil
		}
		j := a.logView.Job
		return a.showLog(logTargetMsg{job: j, stderr: m.Stderr, out: a.logPaths[0], err: a.logPaths[1]})
	case views.PagerPathMsg:
		return a.execPager(m.Path)
	case scriptMsg:
		if m.err != nil {
			a.setFlash("batch script of "+m.job.ID.Raw+": "+layout.FirstLine(m.err.Error()), true)
			return nil
		}
		buf := logs.NewBuffer(logs.MaxLines, false, nil)
		buf.SetText(m.text)
		a.closeLog()
		a.logView = views.NewLogView(fmt.Sprintf("batch script %s %s %s", a.th.Sym.Separator, m.job.ID.Raw, m.job.Name), buf, true)
		a.logView.Job = m.job
		a.flash = flash{}
	case gpuSampleMsg:
		if m.err != nil {
			a.setFlash("GPU sample of "+m.job.ID.Raw+": "+layout.FirstLine(m.err.Error()), true)
			return nil
		}
		a.info = &infoBox{title: "GPU use of " + m.job.ID.Raw + " " + m.job.Name, body: a.gpuReport(m)}
		a.flash = flash{}
	case finalTickMsg:
		return a.lookupFinal()
	case finalMsg:
		return a.handleFinal(m)
	case configEditedMsg:
		return a.reloadConfig(m)
	case settingsSavedMsg:
		a.settingsSaved(m)
	case doctorMsg:
		a.info = &infoBox{title: "sdash doctor", body: m.text}
	case execDoneMsg:
		if m.err != nil {
			a.setFlash(m.what+": "+layout.FirstLine(m.err.Error()), true)
		} else if m.what == "shell" {
			a.setFlash("Shell closed", false)
		}
	}
	return nil
}

func (a *App) logTick(d time.Duration) tea.Cmd {
	gen := a.logGen
	return schedule(d, func(time.Time) tea.Msg { return logTickMsg{gen: gen} })
}

// execPager runs ${PAGER:-less} (+F for less) on a file.
func (a *App) execPager(path string) tea.Cmd {
	if a.opt.Demo {
		a.setFlash("the pager is not available in demo mode: the logs are simulated", true)
		return nil
	}
	pager := strings.Fields(a.opt.Getenv("PAGER"))
	if len(pager) == 0 {
		pager = []string{"less"}
	}
	argv := pager
	if filepath.Base(pager[0]) == "less" {
		argv = append(argv, "+F")
	}
	argv = append(argv, path)
	cmd, err := execx.NewInteractive("", argv...)
	if err != nil {
		a.setFlash("pager: "+err.Error(), true)
		return nil
	}
	a.opt.Log.Info("interactive", "argv", execx.Key(argv))
	return tea.Exec(cmd, func(err error) tea.Msg { return execDoneMsg{what: "pager", err: err} })
}

// loadScript fetches a job's batch script.
func (a *App) loadScript(j model.Job) tea.Cmd {
	src := a.opt.Sources
	if src == nil {
		return nil
	}
	a.setFlash("Loading the batch script of "+j.ID.Raw+a.th.Sym.Ellipsis, false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
		defer cancel()
		text, err := src.BatchScript(ctx, j.ID.Raw)
		return scriptMsg{job: j, text: text, err: err}
	}
}

// openShell suspends the TUI and opens a shell inside a running job.
func (a *App) openShell(j model.Job, node string) tea.Cmd {
	if a.opt.Demo {
		a.setFlash("shells are not available in demo mode", true)
		return nil
	}
	if j.User != "" && a.st.User != "" && j.User != a.st.User {
		a.setFlash("you can only open a shell in your own jobs", true)
		return nil
	}
	argv, err := actions.ShellArgv(j, a.ownJobID(j), node, a.opt.Config.Shell, a.opt.Getenv("SHELL"), a.st.Caps)
	if err != nil {
		a.setFlash(err.Error(), true)
		return nil
	}
	cmd, err := actions.Shell(context.Background(), execx.Policy{}, argv)
	if err != nil {
		a.setFlash(err.Error(), true)
		return nil
	}
	a.opt.Log.Info("interactive", "argv", execx.Key(argv))
	a.setFlash("Opening "+execx.Key(argv), false)
	return tea.Exec(cmd, func(err error) tea.Msg { return execDoneMsg{what: "shell", err: err} })
}

// sampleGPU runs nvidia-smi once inside a running job.
func (a *App) sampleGPU(j model.Job) tea.Cmd {
	if j.User != "" && a.st.User != "" && j.User != a.st.User {
		a.setFlash("you can only sample your own jobs", true)
		return nil
	}
	argv, err := actions.GPUSampleArgv(j, a.ownJobID(j), "")
	if err != nil {
		a.setFlash(err.Error(), true)
		return nil
	}
	r := a.opt.Runner
	if r == nil {
		return nil
	}
	a.setFlash("Sampling GPUs (this creates a job step that shows in sacct)"+a.th.Sym.Ellipsis, false)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gpuTimeout)
		defer cancel()
		s, err := actions.SampleGPUs(ctx, r, argv)
		return gpuSampleMsg{job: j, argv: argv, samples: s, err: err}
	}
}

func (a *App) gpuReport(m gpuSampleMsg) string {
	th := a.th
	var b strings.Builder
	for _, g := range m.samples {
		mem := 0.0
		if g.MemTotalMiB > 0 {
			mem = float64(g.MemUsedMiB) / float64(g.MemTotalMiB)
		}
		fmt.Fprintf(&b, "GPU %d  %s\n", g.Index, g.Name)
		fmt.Fprintf(&b, "  util %s %s   mem %s %s of %s\n",
			components.Gauge(th, float64(g.Util)/100, 10), components.Percent(th, float64(g.Util)/100),
			components.Gauge(th, mem, 10), units.FormatMB(float64(g.MemUsedMiB)), units.FormatMB(float64(g.MemTotalMiB)))
	}
	b.WriteString("\n" + th.Muted.Render("Sampled once with: "+execx.Key(m.argv)))
	return b.String()
}
