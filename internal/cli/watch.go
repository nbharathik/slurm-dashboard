package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// minWatch is the shortest --watch redraw period.
const minWatch = 2 * time.Second

// watchStatus keeps status up to date with the dashboard's own scheduler
// and collectors (as if the Overview were open), so it polls Slurm exactly
// as gently. It redraws at most every period: in place on a terminal, as
// one JSON document per line with asJSON.
func (a *app) watchStatus(ctx context.Context, w io.Writer, rt *slurmRuntime, every time.Duration, asJSON bool) error {
	vs := &viewState{historyDays: 7}
	sched := state.NewScheduler(state.RealClock{}, uint64(time.Now().UnixNano()))
	check := a.quotas(rt.cfg, rt.sources.Cmd.User)
	// A watch refreshes on its own even with refresh = "manual".
	live := state.NewIntervals(intervalMap(rt.cfg.Intervals()))
	addCollectors(sched, rt.sources, rt.store.Site, live, vs, func(ctx context.Context) ([]model.Quota, error) { return check(ctx), nil })
	sched.SetVisible(model.TabOverview)
	sched.Start(ctx)

	tty := isTerminal(w)
	tick := time.NewTicker(every)
	defer tick.Stop()
	dirty := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case u := <-sched.Updates():
			rt.store.Apply(u)
			dirty = true
			continue
		case <-tick.C:
		}
		if !dirty || !rt.store.MyJobs.Has && rt.store.MyJobs.Err == nil {
			continue
		}
		dirty = false
		doc := buildStatus(rt, a.clock())
		doc.Errors = storeErrors(rt)
		var err error
		switch {
		case asJSON:
			var b []byte
			if b, err = json.Marshal(doc); err == nil {
				_, err = fmt.Fprintf(w, "%s\n", b)
			}
		case tty:
			_, _ = io.WriteString(w, "\x1b[H\x1b[2J")
			if err = renderStatus(w, doc); err == nil {
				_, err = fmt.Fprintf(w, "\nupdated %s, redrawn at most every %s; Ctrl-C stops\n", a.clock().Format("15:04:05"), every)
			}
		default:
			if err = renderStatus(w, doc); err == nil {
				_, err = fmt.Fprintln(w, "--")
			}
		}
		if err != nil {
			return err
		}
	}
}

// storeErrors lists the sources whose last run failed.
func storeErrors(rt *slurmRuntime) []string {
	st := rt.store
	out := []string{}
	for _, e := range []struct {
		name string
		err  error
	}{{"myjobs", st.MyJobs.Err}, {"cluster", st.Cluster.Err}, {"nodes", st.Nodes.Err}, {"history", st.History.Err}, {"storage", st.Storage.Err}} {
		if e.err != nil {
			out = append(out, e.name+": "+e.err.Error())
		}
	}
	return out
}
