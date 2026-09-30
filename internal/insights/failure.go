package insights

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
)

// Hint explains how a finished job ended and what to do about it.
type Hint struct {
	Title  string // "Out of memory"
	Text   string // what happened and what to try
	Severe bool   // the job failed (as opposed to completed or cancelled)
}

// signalNames are the signals worth naming in hints.
var signalNames = map[int]string{6: "SIGABRT", 9: "SIGKILL", 11: "SIGSEGV", 15: "SIGTERM"}

var signalMeaning = map[int]string{
	6: "an assertion or uncaught C++ exception", 9: "often out of memory",
	11: "a segmentation fault", 15: "a cancel, preemption or the time limit",
}

// signalOf returns the signal that ended a job: the signal field, or
// 128+N in the exit code (a shell reporting a killed child).
func signalOf(code, signal int) int {
	if signal > 0 {
		return signal
	}
	if code > 128 && code < 160 {
		return code - 128
	}
	return 0
}

// nearMemLimit reports a peak within 5% of the allocation.
func nearMemLimit(h model.HistoryJob) bool {
	return h.AllocMemMB > 0 && h.PeakMemMB > 0 && float64(h.PeakMemMB) >= 0.95*float64(h.AllocMemMB)
}

// ExplainFailure explains a finished job. myUID is the
// user's numeric ID, used to say "you" for cancellations.
func ExplainFailure(h model.HistoryJob, myUID string) Hint {
	sig := signalOf(h.ExitCode, h.Signal)
	switch h.State {
	case model.StateCompleted:
		if h.ExitCode == 0 && sig == 0 {
			return Hint{}
		}
	case model.StateOOM:
		return oomHint(h)
	case model.StateTimeout:
		t := "The job reached its time limit"
		if h.TimeLimit != nil {
			t += " of " + units.FormatDuration(*h.TimeLimit)
		}
		return Hint{Title: "Time limit reached", Text: t + ". Raise --time or add checkpointing so it can resume.", Severe: true}
	case model.StateNodeFail, model.StateBootFail:
		return Hint{Title: "Node failure", Text: "A node problem, not your code. Requeue or resubmit the job.", Severe: true}
	case model.StatePreempted:
		return Hint{Title: "Preempted", Text: "A higher-priority job took the resources. Requeue it, or use a QOS that is not preemptible.", Severe: false}
	case model.StateCancelled:
		var who string
		switch h.CancelledByUID {
		case "":
		case myUID:
			who = "you"
		case "0":
			who = "root (an administrator)"
		default:
			who = "UID " + h.CancelledByUID
		}
		if who == "" {
			return Hint{Title: "Cancelled", Text: "The job was cancelled."}
		}
		return Hint{Title: "Cancelled by " + who, Text: "The job was cancelled by " + who + "."}
	case model.StateDeadline:
		return Hint{Title: "Deadline passed", Text: "The job could not finish before its --deadline.", Severe: true}
	}
	switch {
	case sig == 9 && nearMemLimit(h):
		return oomHint(h)
	case sig == 9:
		return Hint{Title: "Killed (SIGKILL)", Text: "The job was killed with signal 9, often because it ran out of memory.", Severe: true}
	case sig == 11:
		return Hint{Title: "Segmentation fault", Text: "The program crashed with signal 11 (SIGSEGV). Check stderr for the failing step.", Severe: true}
	case sig == 6:
		return Hint{Title: "Aborted", Text: "The program aborted with signal 6 (SIGABRT): an assertion or an uncaught C++ exception.", Severe: true}
	case sig == 15:
		return Hint{Title: "Terminated", Text: "The job got SIGTERM: a cancel, preemption or the time limit.", Severe: true}
	case sig > 0:
		name := signalNames[sig]
		if name == "" {
			name = fmt.Sprintf("signal %d", sig)
		}
		return Hint{Title: "Killed by " + name, Text: fmt.Sprintf("The job ended with %s. Check stderr.", name), Severe: true}
	case h.ExitCode == 127:
		return Hint{Title: "Command not found", Text: "Exit code 127: a command was not found. Check the modules you load and PATH.", Severe: true}
	case signalNames[h.ExitCode] != "":
		// Slurm 23.11 records a script's "exit 139" (128+11) as 11:0, so a
		// small code may be the signal number.
		return Hint{Title: fmt.Sprintf("Exited with code %d", h.ExitCode), Text: fmt.Sprintf(
			"Code %d often means the program was killed by signal %d (%s): %s. Open stderr for details.",
			h.ExitCode, h.ExitCode, signalNames[h.ExitCode], signalMeaning[h.ExitCode]), Severe: true}
	case h.ExitCode != 0:
		return Hint{Title: fmt.Sprintf("Exited with code %d", h.ExitCode), Text: "Open stderr; the log viewer jumps to the first highlighted error line.", Severe: true}
	}
	if h.State == model.StateFailed {
		return Hint{Title: "Failed", Text: "Slurm reports the job as failed. Open stderr for details.", Severe: true}
	}
	return Hint{}
}

func oomHint(h model.HistoryJob) Hint {
	text := "The job ran out of memory"
	if h.AllocMemMB > 0 && h.PeakMemMB > 0 {
		text += fmt.Sprintf(" (peak %s of %s)", units.FormatMB(float64(h.PeakMemMB)), units.FormatMB(float64(h.AllocMemMB)))
	}
	text += ". Raise --mem"
	if h.PeakMemMB > 0 {
		text += fmt.Sprintf(", for example --mem=%s", memSuggestion(max(h.PeakMemMB, h.AllocMemMB)*3/2))
	}
	return Hint{Title: "Out of memory", Text: text + ".", Severe: true}
}

// Suggestion is one right-sizing proposal.
type Suggestion struct {
	What   string // "Memory"
	Reason string // "peak 3.1G of 32G (10%)"
	Line   string // "#SBATCH --mem=5G"; empty when there is nothing to copy
	Option string // "mem": the sbatch option Line sets, "" when none
	Value  string // "5G"
}

// Suggested maps each suggestion's sbatch option to its value.
func Suggested(list []Suggestion) map[string]string {
	out := map[string]string{}
	for _, s := range list {
		if s.Option != "" {
			out[s.Option] = s.Value
		}
	}
	return out
}

// RightSize proposes #SBATCH changes for a COMPLETED job.
func RightSize(h model.HistoryJob) []Suggestion {
	if h.State != model.StateCompleted {
		return nil
	}
	var out []Suggestion
	if h.AllocMemMB > 0 && h.PeakMemMB > 0 && float64(h.PeakMemMB) < 0.6*float64(h.AllocMemMB) {
		want := int64(math.Ceil(float64(h.PeakMemMB) * 1.3))
		out = append(out, Suggestion{
			What:   "Memory",
			Reason: fmt.Sprintf("peak %s of %s (%d%%)", units.FormatMB(float64(h.PeakMemMB)), units.FormatMB(float64(h.AllocMemMB)), efficiencyPercent(float64(h.PeakMemMB)/float64(h.AllocMemMB))),
			Line:   "#SBATCH --mem=" + memSuggestion(want),
			Option: "mem", Value: memSuggestion(want),
		})
	}
	if h.TimeLimit != nil && *h.TimeLimit > 0 && h.Elapsed > 0 && h.Elapsed < *h.TimeLimit/2 {
		want := time.Duration(float64(h.Elapsed) * 1.5)
		q := 15 * time.Minute
		want = max((want+q-1)/q*q, q)
		out = append(out, Suggestion{
			What:   "Time",
			Reason: fmt.Sprintf("used %s of %s (%d%%)", units.FormatDuration(h.Elapsed), units.FormatDuration(*h.TimeLimit), efficiencyPercent(h.Elapsed.Seconds()/h.TimeLimit.Seconds())),
			Line:   "#SBATCH --time=" + sbatchTime(want),
			Option: "time", Value: sbatchTime(want),
		})
	}
	if h.AllocCPUs >= 4 && h.Eff.CPU >= 0 && h.Eff.CPU < 0.4 {
		want := max(1, int(math.Ceil(float64(h.AllocCPUs)*h.Eff.CPU*1.5)))
		out = append(out, Suggestion{
			What:   "CPUs",
			Reason: fmt.Sprintf("%d%% of %d CPUs busy on average", efficiencyPercent(h.Eff.CPU), h.AllocCPUs),
			Line:   fmt.Sprintf("#SBATCH --cpus-per-task=%d", want),
			Option: "cpus-per-task", Value: strconv.Itoa(want),
		})
	}
	if h.GPUs > 0 && h.Eff.GPUUtil >= 0 && h.Eff.GPUUtil < 0.3 {
		out = append(out, Suggestion{
			What:   "GPUs",
			Reason: fmt.Sprintf("GPU utilisation %d%%: GPU mostly idle; check data loading", efficiencyPercent(h.Eff.GPUUtil)),
		})
	}
	return out
}

// memSuggestion rounds MB up to whole GB, at least 1G.
func memSuggestion(mb int64) string {
	gb := max((mb+1023)/1024, 1)
	return fmt.Sprintf("%dG", gb)
}

// sbatchTime formats a duration unambiguously for --time: HH:MM:SS, or
// D-HH:MM:SS from one day.
func sbatchTime(d time.Duration) string {
	s := int64(d.Seconds())
	days, s := s/86400, s%86400
	hms := fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
	if days > 0 {
		return fmt.Sprintf("%d-%s", days, hms)
	}
	return hms
}

func efficiencyPercent(f float64) int { return int(math.Round(f * 100)) }

// EffCard renders the seff-like summary lines of a finished job.
func EffCard(h model.HistoryJob) []string {
	var b []string
	add := func(format string, a ...any) { b = append(b, fmt.Sprintf(format, a...)) }
	state := string(h.State)
	if h.ExitCode != 0 || h.Signal != 0 {
		state += fmt.Sprintf(" (exit %d:%d)", h.ExitCode, h.Signal)
	}
	add("State:         %s", state)
	if len(h.NodeList) > 0 {
		add("Nodes:         %d (%s)", max(h.Nodes, len(h.NodeList)), strings.Join(h.NodeList, ","))
	}
	add("Cores:         %d", h.AllocCPUs)
	limit := "none"
	if h.TimeLimit != nil {
		limit = units.FormatDuration(*h.TimeLimit)
	}
	add("Elapsed:       %s of %s%s", units.FormatDuration(h.Elapsed), limit, effSuffix(h.Eff.Time))
	add("CPU used:      %s%s", units.FormatDuration(h.TotalCPU), effSuffix(h.Eff.CPU))
	if h.AllocMemMB > 0 {
		peak := "unknown"
		if h.PeakMemMB > 0 {
			peak = units.FormatMB(float64(h.PeakMemMB))
		}
		add("Memory:        %s of %s%s", peak, units.FormatMB(float64(h.AllocMemMB)), effSuffix(h.Eff.Mem))
	}
	if h.GPUs > 0 {
		util := ""
		if h.Eff.GPUUtil >= 0 {
			util = fmt.Sprintf(", utilisation %d%%", efficiencyPercent(h.Eff.GPUUtil))
		}
		add("GPUs:          %d, %.1f GPU-hours%s", h.GPUs, h.Eff.GPUHours, util)
	}
	return b
}

func effSuffix(f float64) string {
	if f < 0 {
		return ""
	}
	return fmt.Sprintf(" (%d%%)", efficiencyPercent(f))
}
