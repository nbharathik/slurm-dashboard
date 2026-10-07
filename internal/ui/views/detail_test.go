package views

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/components"
	"github.com/nbharathik/slurm-dashboard/internal/ui/keys"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

func TestDetailLayout(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 120, 140, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			list := func(w, h int) string { return layout.FitLines("LIST", w, h) }
			card := func(w, h int) string { return layout.FitLines("CARD", w, h) }
			out := detailLayout(width, 30, list, card)
			lines := strings.Split(out, "\n")
			if len(lines) != 30 {
				t.Fatalf("height %d", len(lines))
			}
			for _, line := range lines {
				if layout.Width(line) != width {
					t.Fatalf("width %d: %q", layout.Width(line), line)
				}
			}
			switch {
			case width >= 136:
				if !strings.Contains(lines[0], "LIST") || !strings.Contains(lines[0], "CARD") {
					t.Fatal("wide details should be beside the list")
				}
			case width >= 80:
				if !strings.HasPrefix(lines[0], "LIST") || !strings.HasPrefix(lines[10], "CARD") {
					t.Fatal("medium details should be below the list")
				}
			default:
				if strings.Contains(out, "LIST") || !strings.HasPrefix(lines[0], "CARD") {
					t.Fatal("narrow details should fill the view")
				}
			}
		})
	}
}

func TestDetailFollowsJobsWithoutForeignPolling(t *testing.T) {
	ctx := &Context{Store: &state.Store{User: "you"}, Config: config.Default(), Keys: keys.Default()}
	owned := model.Job{ID: model.JobID{Raw: "1"}, User: "you", State: model.StateRunning}
	foreign := model.Job{ID: model.JobID{Raw: "2"}, User: "someone", State: model.StateRunning}
	v := NewJobs(ctx)
	v.byID = map[string]model.Job{"1": owned, "2": foreign}
	v.table.SetRows([]components.Row{{ID: "1"}, {ID: "2"}})
	v.detail, v.detailID = true, "1"
	cmd := v.key(ctx, tea.KeyPressMsg{Code: tea.KeyDown})
	if v.detailID != "2" || cmd == nil {
		t.Fatal("open detail did not follow selection")
	}
	if msg, ok := cmd().(DetailMsg); !ok || msg.ID != "" {
		t.Fatalf("foreign selection must stop private polling: %#v", msg)
	}
	if cmd := v.key(ctx, tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatal("unchanged selection must not request another lookup")
	}
	cmd = v.key(ctx, tea.KeyPressMsg{Code: tea.KeyUp})
	if msg, ok := cmd().(DetailMsg); !ok || msg.ID != "1" {
		t.Fatalf("owned selection did not restore its detail request: %#v", msg)
	}
}

func TestDetailScrollingAndResize(t *testing.T) {
	ctx := &Context{Config: config.Default(), Keys: keys.Default(), Theme: theme.New(theme.Dark, true, true, false)}
	p := detailPane{}
	content := strings.Repeat("long path /home/you/"+strings.Repeat("x", 80)+"\n", 12) + "last line"
	first := p.render(ctx, "one", "Long node title", "close", content, 40, 12)
	if !strings.Contains(first, "scroll") || !strings.Contains(first, "[esc]") {
		t.Fatal("overflow must offer scrolling and a visible close button")
	}
	for range 100 {
		p.update(ctx, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
	}
	last := p.render(ctx, "one", "Long node title", "close", content, 40, 12)
	if !strings.Contains(last, "last line") || p.offset == 0 {
		t.Fatal("last content must be reachable")
	}
	for _, line := range strings.Split(ansi.Strip(last), "\n") {
		if layout.Width(line) != 40 {
			t.Fatalf("wrapped content overflow: %q", line)
		}
	}
	_ = p.render(ctx, "one", "title", "close", content, 160, 80)
	if p.offset != 0 || p.maxOffset != 0 {
		t.Fatal("resize must clamp the scroll position")
	}
	_ = p.render(ctx, "one", "title", "close", content, 40, 12)
	p.update(ctx, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
	_ = p.render(ctx, "two", "title", "close", content, 40, 12)
	if p.offset != 0 {
		t.Fatal("a new selection must start at the top")
	}
}
