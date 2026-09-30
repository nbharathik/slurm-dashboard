// Package notify tells the user when jobs start or end: flash line, bell, OSC desktop alerts and an optional hook.
package notify

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// Kinds of events.
const (
	Started = "STARTED"
	Ended   = "ENDED" // final state unknown (no accounting)
)

// Event is a job that started or ended.
type Event struct {
	JobID    string
	Name     string
	State    string // Started, a final Slurm state such as COMPLETED, or Ended
	ExitCode string // "0:0"; empty when unknown
	Elapsed  time.Duration
}

// Wanted reports whether the user asked to hear about the event
// (notify.on). An end with an unknown state counts as COMPLETED.
func Wanted(on []string, e Event) bool {
	state := e.State
	if state == Ended {
		state = "COMPLETED"
	}
	for _, s := range on {
		if strings.EqualFold(s, state) || (strings.HasPrefix(state, "CANCELLED") && strings.EqualFold(s, "CANCELLED")) {
			return true
		}
	}
	return false
}

// Message returns a title and body for the event.
func Message(e Event) (title, body string) {
	name := clean(e.Name)
	switch e.State {
	case Started:
		return "Job started", fmt.Sprintf("%s %s is running", e.JobID, name)
	case Ended:
		return "Job ended", fmt.Sprintf("%s %s left the queue", e.JobID, name)
	}
	body = fmt.Sprintf("%s %s: %s", e.JobID, name, e.State)
	if e.ExitCode != "" && e.ExitCode != "0:0" {
		body += " (exit " + e.ExitCode + ")"
	}
	if e.Elapsed > 0 {
		body += " after " + units.FormatShort(e.Elapsed)
	}
	return "Job " + strings.ReplaceAll(strings.ToLower(e.State), "_", " "), body
}

// clean removes escape sequences and control characters (so a job name
// cannot inject terminal sequences) and the OSC field separator.
func clean(s string) string {
	return strings.ReplaceAll(textsafe.Field(s), ";", ",")
}

// Sequences returns terminal bytes for "bell", "osc9" and "osc777"; in tmux, OSC is
// DCS-wrapped (needs allow-passthrough on).
func Sequences(methods []string, e Event, inTmux bool) string {
	title, body := Message(e)
	return NoticeSequences(methods, title, body, inTmux)
}

// NoticeSequences is Sequences for a message that is not about a job
// start or end (a time-limit or idle-job warning).
func NoticeSequences(methods []string, title, body string, inTmux bool) string {
	title, body = clean(title), clean(body)
	var b strings.Builder
	for _, m := range methods {
		var seq string
		switch m {
		case "bell":
			b.WriteString("\a")
			continue
		case "osc9":
			seq = "\x1b]9;" + title + ": " + body + "\a"
		case "osc777":
			seq = "\x1b]777;notify;" + title + ";" + body + "\a"
		default:
			continue
		}
		if inTmux {
			seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
		}
		b.WriteString(seq)
	}
	return b.String()
}

// HookTimeout bounds the on_job_end hook.
const HookTimeout = 10 * time.Second

// HookEnv is the environment the hook gets: job data only in variables,
// never interpolated into the command.
func HookEnv(e Event) []string {
	return []string{
		"SDASH_JOB_ID=" + clean(e.JobID),
		"SDASH_JOB_NAME=" + clean(e.Name),
		"SDASH_JOB_STATE=" + clean(e.State),
		"SDASH_EXIT_CODE=" + clean(e.ExitCode),
		"SDASH_ELAPSED=" + strconv.Itoa(int(e.Elapsed.Seconds())),
	}
}

// RunHook runs notify.on_job_end for an event with a 10 s timeout.
func RunHook(ctx context.Context, r *execx.RealRunner, script string, e Event) error {
	if strings.TrimSpace(script) == "" || e.State == Started {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, HookTimeout)
	defer cancel()
	_, err := execx.RunUserShell(ctx, r, script, HookEnv(e))
	return err
}
