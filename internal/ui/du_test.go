package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

// esc stops a running analysis at once. The real
// runner then kills du's whole process group; execx's cancellation tests
// cover that part.
func TestDuEscCancels(t *testing.T) {
	f := newFixture(t, 0)
	started := make(chan struct{})
	stopped := make(chan time.Duration, 1)
	du := execx.NewFake()
	du.Handler = func(ctx context.Context, argv []string) (execx.Result, error) {
		if argv[len(argv)-1] != "/home/you" {
			t.Errorf("argv = %q", argv)
		}
		close(started)
		begin := time.Now()
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		stopped <- time.Since(begin)
		return execx.Result{Stdout: []byte("100\t/home/you/a\n")}, ctx.Err()
	}
	f.opt.DuRunner = du
	a := f.app(t, 120, 36, false)
	press(a, "6")

	// "a" on Home (NFS: no confirmation) asks the app to analyse; the
	// returned command runs du, so run it in the background like Bubble Tea.
	_, cmd := a.Update(keyPress("a"))
	_, run := a.Update(cmd())
	if run == nil {
		t.Fatal("no du command")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- run() }()
	<-started
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	if s := screen(a); !strings.Contains(s, "Analysing /home/you") {
		t.Fatalf("no running indicator:\n%s", s)
	}

	press(a, "esc")
	select {
	case d := <-stopped:
		if d > time.Second {
			t.Errorf("du stopped after %v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("esc did not stop du")
	}
	a.Update(<-done)
	s := screen(a)
	for _, want := range []string{"finished before the scan was cancelled", "Unfinished directories are missing", "Analysis of /home/you cancelled"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	if a.duCancel != nil {
		t.Error("analysis still marked as running")
	}
}
