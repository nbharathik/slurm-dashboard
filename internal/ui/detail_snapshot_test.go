package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nbharathik/slurm-dashboard/internal/ui/views"
)

func TestResponsiveDetails(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 160} {
		for _, tab := range []string{"jobs", "queue", "nodes", "usage", "storage"} {
			t.Run(fmt.Sprintf("%s-%d", tab, width), func(t *testing.T) {
				f := newFixture(t, 0)
				a := f.app(t, width, 35, false)
				a.openTab(tab)
				if tab == "nodes" {
					a.views[a.tab].(*views.Nodes).Open(a.ctx, "gpu01")
				} else {
					if tab == "queue" {
						press(a, "j")
					}
					press(a, "enter")
				}
				got := screen(a)
				checkSize(t, got, width, 35)
				if !strings.Contains(got, "[esc]") {
					t.Fatal("detail card did not open")
				}
				if width == 100 || width == 160 {
					golden(t, fmt.Sprintf("detail-%s-%d", tab, width), got)
				}
				for _, next := range []int{60, 160, 100} {
					a.Update(tea.WindowSizeMsg{Width: next, Height: 35})
					checkSize(t, screen(a), next, 35)
				}
				press(a, "esc")
				if strings.Contains(screen(a), "[esc]") {
					t.Fatal("detail card did not close after resize")
				}
			})
		}
	}
}

func TestNodeDetailMouseAndSelection(t *testing.T) {
	f := newFixture(t, 0)
	a := f.app(t, 160, 20, false)
	a.openTab("nodes")
	a.views[a.tab].(*views.Nodes).Open(a.ctx, "gpu01")
	a.zm.SetEnabled(true)
	_ = a.View()
	zoneID := "nodes:close:pane"
	deadline := time.Now().Add(time.Second)
	z := a.zm.Get(zoneID)
	for (z == nil || z.EndY <= z.StartY) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		z = a.zm.Get(zoneID)
	}
	if z == nil || z.StartX < 80 {
		t.Fatal("detail mouse zone must be on the right")
	}
	before := screen(a)
	if !strings.Contains(before, "scroll") {
		t.Fatal("fixture must overflow the detail pane")
	}
	a.Update(tea.MouseWheelMsg{X: z.StartX + 2, Y: z.StartY + 2, Button: tea.MouseWheelDown})
	if after := screen(a); after == before || !strings.Contains(after, "gpu01 [esc]") {
		t.Fatal("wheel over the card must scroll details without moving the selected node")
	}
	press(a, "j")
	if !strings.Contains(screen(a), "gpu02 [esc]") {
		t.Fatal("keyboard navigation must update the open node card")
	}
	closeZone := a.zm.Get("nodes:close")
	if closeZone == nil || closeZone.StartX < 80 {
		t.Fatal("close button must remain inside the detail pane")
	}
	a.Update(tea.MouseClickMsg{X: closeZone.StartX, Y: closeZone.StartY, Button: tea.MouseLeft})
	if strings.Contains(screen(a), "[esc]") {
		t.Fatal("close button did not dismiss the detail pane")
	}
}
