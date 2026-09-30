package ui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/notify"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// Final-state lookups allow at most one sacct call every 10 s, after accounting
// has had time to record the end.
const (
	finalEvery    = 10 * time.Second
	finalMinDelay = 3 * time.Second
	finalAttempts = 3
)

type (
	finalTickMsg struct{}
	finalMsg     struct {
		jobs   []endedJob
		states []parse.FinalState
		err    error
	}
)

// endedJob is a job that left the queue and awaits its final state.
type endedJob struct {
	job      model.Job
	attempts int
}

// onTransitions turns job state changes into notifications.
func (a *App) onTransitions(ts []state.Transition) tea.Cmd {
	var events []notify.Event
	for _, t := range ts {
		switch t.Kind {
		case state.Started:
			events = append(events, notify.Event{JobID: t.Job.ID.Raw, Name: t.Job.Name, State: notify.Started})
		case state.Ended:
			a.ended = append(a.ended, endedJob{job: t.Job})
		}
	}
	return tea.Batch(a.deliver(events), a.scheduleFinal())
}

func (a *App) scheduleFinal() tea.Cmd {
	if len(a.ended) == 0 || a.finalPending {
		return nil
	}
	a.finalPending = true
	wait := max(finalMinDelay, finalEvery-a.now().Sub(a.lastFinal))
	return schedule(wait, func(time.Time) tea.Msg { return finalTickMsg{} })
}

// lookupFinal asks sacct for the final state of the jobs that ended.
func (a *App) lookupFinal() tea.Cmd {
	jobs := a.ended
	a.ended = nil
	a.lastFinal = a.now()
	src := a.opt.Sources
	if src == nil || !a.st.Caps.HasSacct {
		return func() tea.Msg { return finalMsg{jobs: jobs} }
	}
	ids := make([]string, len(jobs))
	for i, j := range jobs {
		ids[i] = j.job.ID.Raw
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
		defer cancel()
		states, err := src.FinalStates(ctx, ids)
		return finalMsg{jobs: jobs, states: states, err: err}
	}
}

// handleFinal records final states and notifies. Jobs accounting does not
// know yet are retried; after three tries they are reported as "ended".
func (a *App) handleFinal(m finalMsg) tea.Cmd {
	a.finalPending = false
	byID := map[string]parse.FinalState{}
	for _, s := range m.states {
		byID[s.ID] = s
	}
	var events []notify.Event
	for _, ej := range m.jobs {
		j := ej.job
		fs, ok := byID[j.ID.Raw]
		if ok && fs.State.IsActive() {
			ok = false // accounting has not caught up
		}
		if !ok && m.err == nil && a.st.Caps.HasSacct && ej.attempts+1 < finalAttempts {
			a.ended = append(a.ended, endedJob{job: j, attempts: ej.attempts + 1})
			continue
		}
		if !ok {
			events = append(events, notify.Event{JobID: j.ID.Raw, Name: j.Name, State: notify.Ended, Elapsed: j.TimeUsed})
			continue
		}
		h := model.HistoryJob{
			ID: j.ID, Name: j.Name, Partition: j.Partition, State: fs.State, CancelledByUID: fs.CancelledBy,
			ExitCode: fs.ExitCode, Signal: fs.Signal, Elapsed: fs.Elapsed, End: a.now(), Start: j.StartTime,
			Submit: j.SubmitTime, TimeLimit: j.TimeLimit, AllocCPUs: j.CPUs, GPUs: j.GPUs, NodeList: j.NodeList,
			Eff: model.Efficiency{CPU: -1, Mem: -1, Time: -1, GPUUtil: -1},
		}
		a.st.AddEnded(h)
		state := string(fs.State)
		if fs.State == model.StateCancelled && fs.CancelledBy != "" {
			state += " by " + fs.CancelledBy
		}
		events = append(events, notify.Event{
			JobID: j.ID.Raw, Name: j.Name, State: state, Elapsed: fs.Elapsed,
			ExitCode: strconv.Itoa(fs.ExitCode) + ":" + strconv.Itoa(fs.Signal),
		})
	}
	for _, v := range a.views {
		v.Refresh(a.ctx)
	}
	return tea.Batch(a.deliver(events), a.scheduleFinal())
}

// notifyStates are the job ends that notify; notifyMethods how, besides
// the message line.
var (
	notifyStates  = []string{"COMPLETED", "FAILED", "TIMEOUT", "OUT_OF_MEMORY", "CANCELLED", "NODE_FAIL"}
	notifyMethods = []string{"bell", "osc9"}
)

// deliver shows, rings and hooks job ends: the message, bell and desktop
// notice when the notify setting is on, and notify_command in any case.
func (a *App) deliver(events []notify.Event) tea.Cmd {
	var wanted []notify.Event
	for _, e := range events {
		if notify.Wanted(notifyStates, e) {
			wanted = append(wanted, e)
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	_, body := notify.Message(wanted[len(wanted)-1])
	if len(wanted) > 1 {
		body = fmt.Sprintf("%d job updates; latest: %s", len(wanted), body)
	}
	failed := slices.ContainsFunc(wanted, func(e notify.Event) bool {
		return e.State != notify.Started && e.State != "COMPLETED" && e.State != notify.Ended
	})
	show := a.opt.Config.Notify
	if show {
		a.setFlash(body, failed)
	}
	inTmux := a.opt.Getenv("TMUX") != ""
	var cmds []tea.Cmd
	var seq strings.Builder
	for _, e := range wanted {
		if show {
			seq.WriteString(notify.Sequences(notifyMethods, e, inTmux))
		}
		if hook := a.opt.Hook; hook != nil && e.State != notify.Started {
			cmds = append(cmds, func() tea.Msg {
				if err := hook(e); err != nil {
					a.opt.Log.Warn("notify hook failed", "job", e.JobID, "err", err)
				}
				return nil
			})
		}
	}
	if seq.Len() > 0 {
		cmds = append(cmds, tea.Raw(seq.String()))
	}
	return tea.Batch(cmds...)
}

// warnNotices announces each time-limit and idle-job alert once, like a job
// end but without running notify_command.
func (a *App) warnNotices() tea.Cmd {
	var fresh []model.Alert
	for _, al := range a.st.Alerts(a.now(), a.state.Dismissed) {
		if (al.Kind == "timelimit" || al.Kind == "waste") && !a.warned[al.Key] {
			if a.warned == nil {
				a.warned = map[string]bool{}
			}
			a.warned[al.Key] = true
			fresh = append(fresh, al)
		}
	}
	if len(fresh) == 0 || !a.opt.Config.Notify {
		return nil
	}
	last := fresh[len(fresh)-1]
	text := last.Message
	if len(fresh) > 1 {
		text = fmt.Sprintf("%d warnings; latest: %s", len(fresh), text)
	}
	a.setFlash(text, false)
	title := "Job nearly out of time"
	if last.Kind == "waste" {
		title = "Job leaves resources idle"
	}
	seq := notify.NoticeSequences(notifyMethods, title, last.Message, a.opt.Getenv("TMUX") != "")
	return tea.Raw(seq)
}
