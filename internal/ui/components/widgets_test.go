package components

import (
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

func TestPanelExactSize(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		th := theme.New(theme.Dark, true, false, ascii)
		p := Panel(th, "Alerts", "line one\nline two that is quite a bit longer than the panel", 30, 6, false)
		w, h := lipgloss.Size(p)
		if w != 30 || h != 6 {
			t.Fatalf("ascii=%v panel is %dx%d, want 30x6:\n%s", ascii, w, h, p)
		}
		if !strings.Contains(ansi.Strip(p), "Alerts") {
			t.Fatalf("title missing:\n%s", ansi.Strip(p))
		}
	}
}

// In ASCII mode, free and unavailable GPUs must look different.
func TestGPUCellsASCII(t *testing.T) {
	th := theme.New("dark", true, true, true)
	got := ansi.Strip(GPUCells(th, 1, 1, 1, 4, 8))
	if got != "Yo.x" {
		t.Errorf("GPUCells = %q, want %q", got, "Yo.x")
	}
}

func TestSparkline(t *testing.T) {
	th := theme.New(theme.Dark, true, true, false)
	if got := Sparkline(th, []float64{1, 2, 3, 4, 5, 6, 7, 8}); got != "▁▂▃▄▅▆▇█" {
		t.Errorf("ramp = %q", got)
	}
	if got := Sparkline(th, []float64{math.NaN(), 3, 3, math.NaN()}); got != " ▅▅ " {
		t.Errorf("flat with gaps = %q", got)
	}
	if got := Sparkline(th, nil); got != "" {
		t.Errorf("empty = %q", got)
	}
	ascii := theme.New(theme.Dark, true, true, true)
	if got := Sparkline(ascii, []float64{0, 10}); got != "_@" {
		t.Errorf("ascii = %q", got)
	}
}
