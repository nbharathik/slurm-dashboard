package notify

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

func TestWanted(t *testing.T) {
	on := []string{"COMPLETED", "FAILED", "CANCELLED"}
	for state, want := range map[string]bool{"COMPLETED": true, "failed": true, "FAILED": true, "CANCELLED by 1000": true, "TIMEOUT": false, Started: false, Ended: true} {
		if got := Wanted(on, Event{State: state}); got != want {
			t.Errorf("Wanted(%s) = %v", state, got)
		}
	}
	if !Wanted([]string{"started"}, Event{State: Started}) {
		t.Fatal("STARTED is opt-in")
	}
}

func TestMessagesAndSequences(t *testing.T) {
	e := Event{JobID: "812", Name: "train\x1b]52;c;evil\a;x", State: "FAILED", ExitCode: "1:0", Elapsed: 3*time.Hour + 5*time.Minute}
	title, body := Message(e)
	if title != "Job failed" || body != "812 train,x: FAILED (exit 1:0) after 3h05m" {
		t.Fatalf("message = %q / %q", title, body)
	}
	seq := Sequences([]string{"flash", "bell", "osc9", "osc777"}, e, false)
	want := "\a\x1b]9;Job failed: 812 train,x: FAILED (exit 1:0) after 3h05m\a\x1b]777;notify;Job failed;812 train,x: FAILED (exit 1:0) after 3h05m\a"
	if seq != want {
		t.Fatalf("sequences = %q", seq)
	}
	// Exactly the ESCs we wrote: the job name's ESC was removed.
	if strings.Count(seq, "\x1b") != 2 {
		t.Fatal("job name injected an escape")
	}
	tmux := Sequences([]string{"osc9"}, Event{JobID: "1", State: "COMPLETED"}, true)
	if tmux != "\x1bPtmux;\x1b\x1b]9;Job completed: 1 : COMPLETED\a\x1b\\" {
		t.Fatalf("tmux = %q", tmux)
	}
	for _, e := range []Event{{State: Started, JobID: "1"}, {State: Ended, JobID: "2"}} {
		if title, _ := Message(e); title == "" {
			t.Fatal("empty title")
		}
	}
}

func TestHook(t *testing.T) {
	e := Event{JobID: "812", Name: "a$(touch /tmp/x)b", State: "COMPLETED", ExitCode: "0:0", Elapsed: 90 * time.Second}
	env := strings.Join(HookEnv(e), "\n")
	if !strings.Contains(env, "SDASH_JOB_NAME=a$(touch /tmp/x)b") || !strings.Contains(env, "SDASH_ELAPSED=90") {
		t.Fatalf("env = %s", env)
	}
	r := execx.NewReal(execx.Options{BaseEnv: []string{"PATH=/usr/bin:/bin"}})
	out := t.TempDir() + "/hook.out"
	script := `printf '%s|%s|%s|%s|%s' "$SDASH_JOB_ID" "$SDASH_JOB_NAME" "$SDASH_JOB_STATE" "$SDASH_EXIT_CODE" "$SDASH_ELAPSED" > ` + out
	if err := RunHook(context.Background(), r, script, e); err != nil {
		t.Fatal(err)
	}
	b, _ := readFile(out)
	if b != "812|a$(touch /tmp/x)b|COMPLETED|0:0|90" {
		t.Fatalf("hook saw %q", b)
	}
	if err := RunHook(context.Background(), r, script, Event{State: Started}); err != nil {
		t.Fatal("hooks do not run for starts")
	}
	if err := RunHook(context.Background(), r, "", e); err != nil {
		t.Fatal("empty hook")
	}
}
