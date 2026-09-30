//go:build integration

package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/integration"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/notify"
	"github.com/nbharathik/slurm-dashboard/internal/slurm"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

// realApp is the dashboard on the throwaway cluster.
func realApp(t *testing.T, c *integration.Cluster) (*App, *state.Sources) {
	t.Helper()
	previous := schedule
	schedule = tea.Tick
	t.Cleanup(func() { schedule = previous })
	ctx := context.Background()
	user, err := slurm.User(ctx, os.Getenv, c.Runner)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := slurm.Probe(ctx, c.Runner, user)
	if err != nil {
		t.Fatal(err)
	}
	src := &state.Sources{Runner: c.Runner, Cmd: slurm.Commands{Caps: caps, User: user}}
	st := &state.Store{User: user, Caps: caps, ClusterName: integration.ThrowawayCluster}
	a := New(Options{Config: config.Default(), Store: st, Runner: c.Runner, Sources: src, Theme: "dark", StartTab: "jobs"})
	t.Cleanup(a.Close)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return a, src
}

func reload(t *testing.T, a *App, src *state.Sources) {
	t.Helper()
	jobs, err := src.MyJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.Update(updateMsg(state.Update{Source: "myjobs", Data: jobs, At: time.Now()}))
}

// TestPaletteActionsOnRealJobs drives the palette and confirmation modal
// against real Slurm: /hold, /release and /cancel on an sdash-test job.
func TestPaletteActionsOnRealJobs(t *testing.T) {
	c := integration.NewCluster(t)
	driveTimeout = 15 * time.Second
	t.Cleanup(func() { driveTimeout = defaultDriveTimeout })
	id := c.Submit("palette", "--begin=now+1hour", "-t", "10", "--wrap", "sleep 1")
	a, src := realApp(t, c)
	reload(t, a, src)

	typeLine := func(s string) {
		press(a, ":")
		for _, r := range s {
			press(a, string(r))
		}
		press(a, "enter")
	}
	waitReason := func(want func(string) bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			reload(t, a, src)
			for _, j := range a.st.MyJobs.Data {
				if j.ID.Raw == id && want(j.Reason) {
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("job %s never reached the expected reason", id)
	}

	typeLine("hold " + id)
	if !strings.Contains(a.flash.text, "1 job") || a.flash.err {
		t.Fatalf("hold flash = %+v", a.flash)
	}
	waitReason(func(r string) bool { return strings.HasPrefix(r, "JobHeld") })
	typeLine("release " + id)
	waitReason(func(r string) bool { return !strings.HasPrefix(r, "JobHeld") })

	typeLine("cancel " + id)
	got := screen(a)
	if a.confirm == nil || !strings.Contains(got, "scancel "+id) {
		t.Fatalf("no confirmation with the exact command:\n%s", got)
	}
	press(a, "y")
	if a.flash.err || !strings.Contains(a.flash.text, "Cancelled 1 job") {
		t.Fatalf("cancel flash = %+v", a.flash)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		ids := c.SqueueIDs("")
		if !strings.Contains(" "+strings.Join(ids, " ")+" ", " "+id+" ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s still queued after /cancel", id)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// TestFollowedLogShowsNewLines checks that a followed log
// shows a new line in under 2 s.
func TestFollowedLogShowsNewLines(t *testing.T) {
	c := integration.NewCluster(t)
	driveTimeout = 15 * time.Second
	t.Cleanup(func() { driveTimeout = defaultDriveTimeout })
	out := t.TempDir() + "/follow.out"
	id := c.Submit("follow", "-o", out, "-t", "5", "--wrap", "for i in $(seq 1 120); do echo tick $i; sleep 1; done")
	c.WaitState(id, "RUNNING")
	a, src := realApp(t, c)
	reload(t, a, src)
	var j model.Job
	for _, x := range a.st.MyJobs.Data {
		if x.ID.Raw == id {
			j = x
		}
	}
	drive(a, a.openLogs(j, false, false), 0)
	if a.logView == nil || a.logView.Path != out {
		t.Fatalf("log viewer did not open %s: %+v", out, a.flash)
	}
	// One poll cycle as the running app does it: tick, read, apply.
	poll := func() {
		_, cmd := a.Update(logTickMsg{gen: a.logGen})
		if cmd != nil {
			a.Update(cmd())
		}
	}
	time.Sleep(3 * time.Second)
	poll()
	b, _ := os.ReadFile(out)
	want := fmt.Sprintf("tick %d", strings.Count(string(b), "\n")+1)
	start := time.Now()
	for time.Since(start) < 5*time.Second {
		time.Sleep(logPoll)
		poll()
		if strings.Contains(screen(a), want) {
			if d := time.Since(start); d > 2*time.Second+500*time.Millisecond {
				t.Fatalf("the new line took %v to appear", d)
			}
			t.Logf("new line shown after %v", time.Since(start).Round(10*time.Millisecond))
			return
		}
	}
	t.Fatalf("%q never appeared:\n%s", want, screen(a))
}

// TestRerunAndNotifyOnRealCluster checks that a held test job is
// rerun from its stored script, Estimate shows a start time, the
// confirmation shows the exact command, the new job ID is shown and
// jumped to, and when it ends the OSC 9 bytes are written and the hook
// receives the job's variables.
func TestRerunAndNotifyOnRealCluster(t *testing.T) {
	c := integration.NewCluster(t)
	driveTimeout = 15 * time.Second
	t.Cleanup(func() { driveTimeout = defaultDriveTimeout })
	work := t.TempDir()
	orig := c.Submit("rerunui", "--hold", "-t", "5", "--wrap", "sleep 2")
	a, src := realApp(t, c)
	hookOut := work + "/hook.env"
	hr := execx.NewReal(execx.Options{})
	a.opt.Hook = func(e notify.Event) error {
		return notify.RunHook(context.Background(), hr, `env | grep ^SDASH_ | sort > `+hookOut, e)
	}
	reload(t, a, src)

	var line string
	for deadline := time.Now().Add(30 * time.Second); line == "" && time.Now().Before(deadline); time.Sleep(time.Second) {
		line, _, _ = src.SubmitLine(context.Background(), orig) // accounting lags the controller
	}
	_, cmd := a.Update(views.RerunMsg{ID: orig})
	if cmd == nil {
		t.Fatalf("rerun did not start: %+v", a.flash)
	}
	a.Update(cmd())
	if a.rerun == nil || !strings.Contains(screen(a), "Rerun "+orig+" sdash-test-rerunui") {
		t.Fatalf("form did not open (flash %+v):\n%s", a.flash, screen(a))
	}
	press(a, "s")
	if !strings.Contains(screen(a), "Would start") {
		t.Fatalf("no estimate:\n%s", screen(a))
	}
	press(a, "r")
	if a.confirm == nil || !strings.Contains(screen(a), "sbatch --parsable") || !strings.Contains(screen(a), "--job-name=sdash-test-rerunui") {
		t.Fatalf("no confirmation:\n%s", screen(a))
	}
	press(a, "y")
	id := a.state.LastSubmitted
	if id == "" || id == orig || !strings.Contains(a.flash.text, "Submitted job "+id) {
		t.Fatalf("flash = %+v", a.flash)
	}
	c.Track(id)
	// The form closed and the app jumps to the new job once it is listed.
	reload(t, a, src)
	a.Update(views.OpenJobMsg{ID: id})
	if a.rerun != nil || !strings.Contains(screen(a), id) {
		t.Fatalf("did not jump to %s:\n%s", id, screen(a))
	}

	// Wait for the job to end, then run the pipeline as the app does.
	c.WaitState(id, "RUNNING", "COMPLETING", "")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && contains(c.SqueueIDs(""), id) {
		time.Sleep(time.Second)
	}
	jobs, err := src.MyJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.Update(updateMsg(state.Update{Source: "myjobs", Data: jobs, At: time.Now()}))
	var raw strings.Builder
	for try := 0; try < 5 && (len(a.ended) > 0 || a.finalPending); try++ {
		time.Sleep(2 * time.Second)
		_, cmd := a.Update(finalTickMsg{})
		_, next := a.Update(cmd())
		collectRaw(next, &raw)
	}
	if !strings.Contains(raw.String(), "\x1b]9;Job completed: "+id+" sdash-test-rerunui: COMPLETED") {
		t.Fatalf("OSC 9 bytes = %q (flash %+v)", raw.String(), a.flash)
	}
	env, err := os.ReadFile(hookOut)
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	for _, want := range []string{"SDASH_JOB_ID=" + id, "SDASH_JOB_NAME=sdash-test-rerunui", "SDASH_JOB_STATE=COMPLETED", "SDASH_EXIT_CODE=0:0"} {
		if !strings.Contains(string(env), want) {
			t.Errorf("hook env lacks %s:\n%s", want, env)
		}
	}
}

// collectRaw runs commands and keeps what they would write to the
// terminal.
func collectRaw(cmd tea.Cmd, out *strings.Builder) {
	if cmd == nil {
		return
	}
	switch m := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range m {
			collectRaw(c, out)
		}
	case nil:
	default:
		fmt.Fprintf(out, "%v", m)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
