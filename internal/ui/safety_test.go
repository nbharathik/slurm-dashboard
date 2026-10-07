package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/logs"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

type deniedLogFS struct{ t *testing.T }

func (f deniedLogFS) Open(string) (logs.File, error) {
	f.t.Fatal("foreign operation opened a file")
	return nil, fmt.Errorf("denied")
}

func TestForeignJobEntryPoints(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 100, 40, false)
	fake := execx.NewFake()
	a.opt.Runner = fake
	a.opt.Sources = &state.Sources{Runner: fake, Cmd: f.src.Cmd}
	a.opt.LogFS = deniedLogFS{t}
	a.opt.Demo = false
	var j model.Job
	for _, candidate := range a.st.AllJobs.Data {
		if candidate.User != "" && candidate.User != a.st.User {
			j = candidate
			break
		}
	}
	if j.ID.Raw == "" {
		t.Fatal("fixture has no public foreign job")
	}
	a.st.Apply(state.Update{Source: "jobdetail", Data: state.Detail{ID: j.ID.Raw, Job: &model.JobDetail{Job: j, WorkDir: "/PRIVATE-dir", StdOut: "/PRIVATE-log", Command: "PRIVATE-command", Raw: map[string]string{"Secret": "PRIVATE-value"}, RawOrder: []string{"Secret"}}}, At: a.now()})
	press(a, "3")
	a.views[a.tab].(interface{ Open(*views.Context, string) }).Open(a.ctx, j.ID.Raw)
	got := screen(a)
	for _, want := range []string{j.User, j.Name, "Elapsed:", "Limit:", "Public queue snapshot"} {
		if !strings.Contains(got, want) {
			t.Fatalf("public field %q missing", want)
		}
	}
	if strings.Contains(got, "PRIVATE") {
		t.Fatal("foreign cached private detail reached the public card")
	}
	for _, key := range []string{"l", "e", "o", "t", "g", "c", "h", "u", "Q", "r", "i"} {
		press(a, key)
	}
	a.confirm = nil
	// Exercise the same mouse handlers even if an obsolete private button zone survives.
	a.zm.SetEnabled(true)
	for _, id := range []string{"out", "err", "script", "shell", "cancel", "requeue", "fields"} {
		zoneID := "jobs:btn:" + id
		_ = a.zm.Scan(a.ctx.Mark(zoneID, "button"))
		msg := tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}
		deadline := time.Now().Add(time.Second)
		for !a.ctx.InZone(zoneID, msg) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !a.ctx.InZone(zoneID, msg) {
			t.Fatal("mouse zone was not registered")
		}
		drive(a, a.views[a.tab].Update(a.ctx, msg), 0)
	}
	for _, msg := range []tea.Msg{views.LogMsg{Job: j}, views.PagerMsg{Job: j}, views.ScriptMsg{Job: j}, views.ShellMsg{Job: j}, views.GPUSampleMsg{Job: j}, views.RerunMsg{ID: j.ID.Raw}, views.ActionMsg{Action: "cancel", Jobs: []model.Job{j}}} {
		drive(a, a.handleRequest(msg), 0)
	}
	for _, command := range []string{"logs " + j.ID.Raw, "script " + j.ID.Raw, "shell " + j.ID.Raw, "gpu " + j.ID.Raw, "cancel " + j.ID.Raw, "rerun " + j.ID.Raw, "copy path"} {
		cmd, _ := a.runPalette(command)
		drive(a, cmd, 0)
	}
	if a.confirm != nil || a.logView != nil {
		t.Fatal("foreign private feature opened")
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("foreign private command launched: %q", fake.Calls())
	}
}

func TestPrivateActionRechecksFreshState(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 100, 40, false)
	fake := execx.NewFake()
	a.opt.Runner = fake
	// A selected row alone cannot authorise a mutation after ownership changes.
	a.opt.Sources = &state.Sources{Runner: fake, Cmd: f.src.Cmd}
	fake.Set(f.src.Cmd.MyJobs(), execx.FakeResponse{})
	j := a.st.MyJobs.Data[0]
	a.startAction("cancel", []model.Job{j}, nil)
	if a.confirm == nil {
		t.Fatal("missing existing confirmation")
	}
	drive(a, a.runAction(a.confirm.Action, a.confirm.Jobs, a.confirm.Argvs), 0)
	for _, argv := range fake.Calls() {
		if argv[0] != "squeue" {
			t.Fatalf("mutation launched: %q", argv)
		}
	}
}

func TestAppCloseCancelsBackgroundContext(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 100, 30, false)
	ctx, cancel := context.WithTimeout(a.runCtx, time.Minute)
	defer cancel()
	a.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("application background context survived close")
	}
}

func TestHiddenQueueInvalidationReleasesDerivedRows(t *testing.T) {
	a := bigApp(t, 1000)
	press(a, "j")
	queue := a.views[a.tab].(interface{ SelectedJobs() []model.Job })
	selected := queue.SelectedJobs()
	if len(selected) == 0 {
		t.Fatal("missing queue cursor")
	}
	j := selected[0]
	press(a, "1")
	a.applyUpdate(state.Update{Source: "alljobs", Data: a.st.AllJobs.Data, At: a.now()})
	if !a.dirty["queue"] {
		t.Fatal("hidden queue was not invalidated")
	}
	if len(queue.SelectedJobs()) != 0 {
		t.Fatal("hidden Queue retained obsolete derived jobs")
	}
	press(a, "3")
	got := queue.SelectedJobs()
	if len(got) == 0 || got[0].ID.Raw != j.ID.Raw {
		t.Fatalf("cursor changed across invalidation: %s -> %v", j.ID.Raw, got)
	}
}

func TestDetailPollingFollowsTheActivePanel(t *testing.T) {
	f := newFixture(t, 0)
	var active string
	f.opt.OnViewState = func(msg tea.Msg) {
		if m, ok := msg.(views.DetailMsg); ok {
			active = m.ID
		}
	}
	a := f.app(t, 100, 40, false)
	press(a, "2", "enter")
	if active == "" {
		t.Fatal("owned detail did not activate polling")
	}
	ownedID := active
	press(a, "3")
	if active != "" {
		t.Fatal("Queue without a detail panel kept Jobs polling")
	}
	press(a, "2")
	if active != ownedID {
		t.Fatal("reopening Jobs did not restore its detail poll")
	}
	press(a, "1")
	if active != "" {
		t.Fatal("Overview kept hidden detail polling")
	}
	a.handleViewState(views.DetailMsg{ID: ownedID})
	if active != "" {
		t.Fatal("a delayed detail message reactivated a hidden panel")
	}
}

func TestWrappedCapacityMouseNavigation(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 40, 45, false)
	a.zm.SetEnabled(true)
	_ = a.View()
	id := "ov:cluster:0"
	deadline := time.Now().Add(time.Second)
	z := a.zm.Get(id)
	for (z == nil || z.EndY <= z.StartY) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		z = a.zm.Get(id)
	}
	if z == nil || z.EndY <= z.StartY {
		t.Fatal("wrapped resource group has no multiline mouse zone")
	}
	msg := tea.MouseClickMsg{X: z.StartX, Y: z.StartY, Button: tea.MouseLeft}
	drive(a, a.views[a.tab].Update(a.ctx, msg), 0)
	press(a, "enter")
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if a.views[a.tab].Name() != "nodes" || !strings.Contains(screen(a), "filter: part:gpu") {
		t.Fatal("clicking the first capacity line lost partition navigation")
	}
}

func TestSharedStorageCapacityAndFreshness(t *testing.T) {
	f := newFixture(t, 0)
	const gib = int64(1 << 30)
	q := model.Quota{
		Label: "Shared", Path: "/shared", IsFilesystemTotal: true,
		UsedBytes: 60 * gib, HardBytes: 90 * gib, FilesystemBytes: 100 * gib,
		AvailableBytes: 30 * gib, AvailabilityKnown: true,
		At: f.clock.Now().Add(-time.Hour), Err: "previous check blocked",
	}
	a := f.app(t, 160, 45, false)
	a.applyUpdate(state.Update{Source: "storage", Data: []model.Quota{q}, At: f.clock.Now()})
	for _, want := range []string{"update failed", "60G used / 100G", "30G available"} {
		if !strings.Contains(screen(a), want) {
			t.Fatalf("Overview is missing %q", want)
		}
	}
	press(a, "6")
	for _, want := range []string{"storage: stale/error", "60G/100G"} {
		if !strings.Contains(screen(a), want) {
			t.Fatalf("Storage is missing %q", want)
		}
	}
	press(a, "enter")
	if !strings.Contains(screen(a), "Your directory") {
		t.Fatal("first storage row should describe personal directory usage")
	}
	press(a, "esc", "j", "enter")
	for _, want := range []string{"60G / 100G shared filesystem", "30G reported by filesystem"} {
		if !strings.Contains(screen(a), want) {
			t.Fatalf("Storage detail is missing %q", want)
		}
	}
	if got := a.st.Storage.Data[0].HardBytes; got != 90*gib {
		t.Fatalf("UI changed legacy report limit: %d", got)
	}
}
