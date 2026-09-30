package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

func (a *App) togglePause() {
	if a.sched == nil {
		return
	}
	paused := !a.sched.Paused()
	a.sched.SetPaused(paused)
	if paused {
		a.setFlash("Refresh paused; r still refreshes, p resumes", false)
	} else {
		a.setFlash("Refresh resumed", false)
	}
}

// applyRefresh puts the refresh setting into effect: the intervals, and
// manual refresh on or off.
func (a *App) applyRefresh() {
	if a.opt.ApplyRefresh != nil {
		a.opt.ApplyRefresh(a.opt.Config.Intervals())
	}
}

// refreshBadge is "manual" or "‖ paused"; the ordinary automatic refresh
// shows nothing (the header says how fresh the data is).
func (a *App) refreshBadge() string {
	th := a.th
	if a.sched == nil {
		return ""
	}
	switch {
	case a.sched.Paused():
		return th.Warn.Render(th.Sym.Paused + " paused")
	case a.sched.Manual():
		return th.Muted.Render("manual")
	}
	return ""
}

// slurmTools are the commands counted in the calls-per-minute figure.
var slurmTools = map[string]bool{
	"squeue": true, "sinfo": true, "scontrol": true, "sacct": true, "sstat": true,
	"sshare": true, "sprio": true, "srun": true, "sbatch": true, "scancel": true,
}

// callRate is the Slurm commands per minute over the last ten minutes of
// history (or what history covers), and the count in the last minute.
func callRate(hist []execx.CallRecord, now time.Time) (perMin float64, lastMin int) {
	var n int
	oldest := now
	for _, r := range hist {
		if len(r.Argv) == 0 || !slurmTools[r.Argv[0]] || now.Sub(r.Start) > 10*time.Minute {
			continue
		}
		n++
		if r.Start.Before(oldest) {
			oldest = r.Start
		}
		if now.Sub(r.Start) <= time.Minute {
			lastMin++
		}
	}
	span := max(now.Sub(oldest), time.Minute)
	return float64(n) / span.Minutes(), lastMin
}

// rateLine is the debug overlay's summary of the load sdash puts on Slurm.
func (a *App) rateLine(hist []execx.CallRecord) string {
	per, last := callRate(hist, a.now())
	parts := []string{fmt.Sprintf("Slurm calls: %.1f per minute (last 10 min), %d in the last minute", per, last)}
	if a.sched != nil {
		switch {
		case a.sched.Paused():
			parts = append(parts, "paused")
		case a.sched.Manual():
			parts = append(parts, "manual refresh")
		default:
			parts = append(parts, "refresh "+a.opt.Config.Refresh)
		}
	}
	return strings.Join(parts, " "+a.th.Sym.Separator+" ")
}
