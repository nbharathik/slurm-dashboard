package insights

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// maxFailedTasks is how many failed task IDs the summary names.
const maxFailedTasks = 5

// ArraySummary is one line about a whole array: tasks running and pending
// in the queue, and done or failed in history, naming the first failed
// tasks. "" when id is not an array.
func ArraySummary(arr uint64, queued []model.Job, history []model.HistoryJob) string {
	running, pending, done := 0, 0, 0
	var failed []string
	isArray := false
	for _, j := range queued {
		if j.ID.ArrayJobID != arr || !j.ID.IsArray() {
			continue
		}
		isArray = true
		switch j.State {
		case model.StateRunning, model.StateCompleting:
			running += j.ID.Tasks()
		case model.StatePending:
			pending += j.ID.Tasks()
		}
	}
	for _, h := range history {
		if h.ID.ArrayJobID != arr || !h.ID.IsArray() || h.State.IsActive() {
			continue
		}
		isArray = true
		switch {
		case h.State.IsFailure():
			failed = append(failed, h.ID.Raw)
		default:
			done++
		}
	}
	if !isArray || running+done+len(failed) == 0 { // all pending: the row says it
		return ""
	}
	parts := []string{fmt.Sprintf("%d running", running), fmt.Sprintf("%d pending", pending), fmt.Sprintf("%d done", done)}
	if len(failed) > 0 {
		slices.Sort(failed)
		names := strings.Join(failed[:min(len(failed), maxFailedTasks)], ", ")
		if len(failed) > maxFailedTasks {
			names += fmt.Sprintf(" +%d", len(failed)-maxFailedTasks)
		}
		parts = append(parts, fmt.Sprintf("%d failed (%s)", len(failed), names))
	}
	return fmt.Sprintf("Array %d: %s", arr, strings.Join(parts, " · "))
}
