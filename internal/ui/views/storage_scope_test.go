package views

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/nbharathik/slurm-dashboard/internal/config"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/units"
	"github.com/nbharathik/slurm-dashboard/internal/state"
	"github.com/nbharathik/slurm-dashboard/internal/ui/layout"
	"github.com/nbharathik/slurm-dashboard/internal/ui/theme"
)

func TestStorageScopeAndExplicitAnalysis(t *testing.T) {
	now := time.Unix(1700000000, 0)
	q := model.Quota{Label: "Home", Path: "/home/alice", UsedBytes: 9 << 40, HardBytes: 10 << 40, IsFilesystemTotal: true, At: now}
	st := &state.Store{DiskUsage: map[string]model.DiskUsage{}}
	st.Storage.Has, st.Storage.Data = true, []model.Quota{q}
	ctx := &Context{Store: st, Config: config.Default(), Theme: theme.New(theme.Dark, true, true, false), Now: now}
	v := NewStorage(ctx)
	v.Refresh(ctx)
	if len(v.table.Rows) != 2 {
		t.Fatalf("want personal and shared rows, got %d", len(v.table.Rows))
	}
	yours, shared := v.table.Rows[0], v.table.Rows[1]
	if yours.Cells["scope"] != "Yours" || !strings.Contains(yours.Cells["blocks"], "Analyse (a)") || yours.Cells["files"] != "-" {
		t.Errorf("unknown personal usage should require analysis: %+v", yours.Cells)
	}
	if shared.Cells["scope"] != "Shared" || !strings.Contains(shared.Cells["blocks"], units.FormatBytes(q.UsedBytes)) {
		t.Errorf("filesystem totals should remain separate: %+v", shared.Cells)
	}
	if len(st.DiskUsage) != 0 {
		t.Fatal("rendering started a directory scan")
	}
	st.DiskUsage[q.Path] = model.DiskUsage{Path: q.Path, Running: true}
	v.Refresh(ctx)
	if !strings.Contains(v.table.Rows[0].Cells["blocks"], "Analysing") {
		t.Fatal("running analysis is missing from the personal row")
	}
	st.DiskUsage[q.Path] = model.DiskUsage{Path: q.Path, Total: 1024, At: now, Partial: true}
	v.Refresh(ctx)
	blocks := v.table.Rows[0].Cells["blocks"]
	if !strings.Contains(blocks, "1K used") || !strings.Contains(blocks, "partial") {
		t.Errorf("personal scan result missing: %q", blocks)
	}
	if v.table.Rows[1].Cells["blocks"] != shared.Cells["blocks"] {
		t.Fatal("a personal scan changed the shared filesystem total")
	}
	for _, width := range []int{40, 60, 80, 100, 160} {
		ctx.Mode = layout.ModeFor(width, 30)
		for _, line := range strings.Split(v.Render(ctx, width, 30), "\n") {
			if ansi.StringWidth(line) > width {
				t.Errorf("storage line exceeds %d columns: %q", width, line)
			}
		}
	}
	v.table.SetCursorID(shared.ID)
	msg := v.analyse(ctx)().(AnalyseMsg)
	if msg.Path != q.Path || v.detail != yours.ID || v.table.CursorID() != yours.ID {
		t.Fatalf("shared-row analysis did not select the personal path: %+v", msg)
	}
	v.detail = shared.ID
	panel := ansi.Strip(v.rawPanel(ctx, 100, 24))
	if strings.Contains(panel, "Largest directories") || !strings.Contains(panel, "Shared filesystem") || !strings.Contains(panel, "Your usage (a)") {
		t.Errorf("shared details mixed in personal scan results: %s", panel)
	}
}
