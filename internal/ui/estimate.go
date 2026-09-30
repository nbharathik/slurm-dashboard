package ui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// estimateCacheFor is how long a start estimate is reused, so holding s
// down cannot flood the controller.
const estimateCacheFor = 10 * time.Second

var plainJobID = regexp.MustCompile(`^[0-9]+$`)

type startEstimateMsg struct {
	id  string
	est parse.StartEstimate
	ok  bool
	err error
	at  time.Time
}

// startEstimate asks Slurm once when a pending job should start
// (squeue --start -j ID). Arrays are asked about by their array ID.
func (a *App) startEstimate(j model.Job) tea.Cmd {
	id := j.ID.Raw
	if j.ID.IsArray() {
		id = strconv.FormatUint(j.ID.ArrayJobID, 10)
	}
	if !plainJobID.MatchString(id) {
		a.setFlash("cannot ask for a start estimate of "+j.ID.Raw, true)
		return nil
	}
	if c, ok := a.estimates[id]; ok && a.now().Sub(c.at) < estimateCacheFor {
		a.showEstimate(c)
		return nil
	}
	src := a.opt.Sources
	if src == nil {
		a.setFlash("start estimates are not available here", true)
		return nil
	}
	a.setFlash("Asking the scheduler about "+id+a.th.Sym.Ellipsis, false)
	now := a.now()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		est, ok, err := src.StartEstimate(ctx, id)
		return startEstimateMsg{id: id, est: est, ok: ok, err: err, at: now}
	}
}

func (a *App) showEstimate(m startEstimateMsg) {
	if a.estimates == nil {
		a.estimates = map[string]startEstimateMsg{}
	}
	a.estimates[m.id] = m
	switch {
	case m.err != nil:
		a.setFlash("start estimate: "+m.err.Error(), true)
	case !m.ok:
		a.setFlash(fmt.Sprintf("%s is no longer pending", m.id), false)
	case m.est.Start.IsZero():
		a.setFlash(fmt.Sprintf("Slurm has no start estimate for %s yet (%s); it computes one on its next backfill pass", m.id, m.est.Reason), false)
	default:
		when := m.est.Start.Local().Format("Mon 15:04")
		in := units.FormatShort(max(m.est.Start.Sub(a.now()), 0))
		msg := fmt.Sprintf("%s is expected to start %s (in %s)", m.id, when, in)
		if m.est.Nodes != "" {
			msg += " on " + m.est.Nodes
		}
		a.setFlash(msg+"; estimates move as other jobs end early or new ones arrive", false)
	}
}
