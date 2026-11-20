package ui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

func schedulerApp(t *testing.T, w, h int) (*App, *fixture) {
	t.Helper()
	f := newFixture(t, 0)
	f.opt.Config.SchedulerTab = true
	a := f.app(t, w, h, false)
	return a, f
}

func TestSchedulerTab(t *testing.T) {
	for _, size := range []struct{ w, h int }{{100, 30}, {160, 45}} {
		a, f := schedulerApp(t, size.w, size.h)
		s, err := f.src.Sdiag(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		a.Update(updateMsg(state.Update{Source: "sdiag", Data: s, At: f.clock.Now()}))
		press(a, "7")
		if a.views[a.tab].Name() != model.TabScheduler {
			t.Fatalf("7 opened %s", a.views[a.tab].Name())
		}
		golden(t, "scheduler-"+strconv.Itoa(size.w), screen(a))
	}
}

func TestSchedulerTabDenied(t *testing.T) {
	a, f := schedulerApp(t, 100, 30)
	a.Update(updateMsg(state.Update{Source: "sdiag", Err: errors.New("sdiag: error: Access/permission denied"), At: f.clock.Now()}))
	press(a, "7")
	if got := screen(a); !strings.Contains(got, "sdiag is not available") || !strings.Contains(got, "Settings") {
		t.Fatalf("denied sdiag:\n%s", got)
	}
}

// Off by default: no tab, 7 does nothing; turning it on adds it live.
func TestSchedulerTabSetting(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 100, 30, false)
	if len(a.views) != len(model.Tabs) {
		t.Fatalf("%d tabs by default", len(a.views))
	}
	press(a, "2", "7")
	if a.views[a.tab].Name() != model.TabJobs {
		t.Fatal("7 must do nothing while the Scheduler tab is off")
	}
	cfg := a.opt.Config
	cfg.SchedulerTab = true
	a.applyConfig(cfg)
	if len(a.views) != len(model.Tabs)+1 || a.views[a.tab].Name() != model.TabJobs {
		t.Fatalf("after turning it on: %d tabs, on %s", len(a.views), a.views[a.tab].Name())
	}
	press(a, "7")
	cfg.SchedulerTab = false
	a.applyConfig(cfg)
	if len(a.views) != len(model.Tabs) || a.views[a.tab].Name() != model.TabOverview {
		t.Fatalf("after turning it off on its tab: %d tabs, on %s", len(a.views), a.views[a.tab].Name())
	}
}
