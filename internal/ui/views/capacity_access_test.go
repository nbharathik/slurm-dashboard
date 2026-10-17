package views

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

// A closed partition is marked instead of offering free capacity; an
// unknown one looks like any other.
func TestCapacityMarksClosedPartitions(t *testing.T) {
	st := &state.Store{}
	st.Nodes.Has = true
	ctx := &Context{Store: st, Config: config.Default(), Theme: theme.New(theme.Dark, true, true, false)}
	parts := []state.PartSummary{
		{Partition: model.Partition{Name: "open", State: "UP"}, CPUFree: 16, CPUTotal: 16, Access: state.AccessAllowed},
		{Partition: model.Partition{Name: "closed", State: "UP"}, CPUFree: 16, CPUTotal: 16, Access: state.AccessDenied, AccessWhy: "AllowGroups"},
		{Partition: model.Partition{Name: "maybe", State: "UP"}, CPUFree: 16, CPUTotal: 16, AccessWhy: "AllowQos"},
	}
	_, rows := capacityLines(ctx, parts, 100)
	line := func(i int) string { return ansi.Strip(strings.Join(rows[i], "\n")) }
	closed := line(1)
	if !strings.Contains(closed, "no access") || !strings.Contains(closed, "closed to you (AllowGroups)") || strings.Contains(closed, "[") {
		t.Errorf("closed partition: %q", closed)
	}
	for _, i := range []int{0, 2} {
		if l := line(i); strings.Contains(l, "no access") || strings.Contains(l, "closed to you") || !strings.Contains(l, "[") {
			t.Errorf("partition %d: %q", i, l)
		}
	}
}
