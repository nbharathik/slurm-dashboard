package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func storageScopeFixture(t *testing.T) *fixture {
	f := newFixture(t, 0)
	q := f.quotas()[0]
	q.IsFilesystemTotal, q.AvailabilityKnown = true, true
	q.UsedBytes, q.SoftBytes, q.HardBytes = 9<<40, 0, 11<<40
	q.FilesystemBytes, q.AvailableBytes = 11<<40, 2<<40
	f.quotas = func() []model.Quota { return []model.Quota{q} }
	return f
}

func TestStorageAnalysisUpdatesTable(t *testing.T) {
	f := storageScopeFixture(t)
	a := f.app(t, 120, 30, false)
	press(a, "6")
	if !strings.Contains(screen(a), "Analyse (a)") {
		t.Fatal("personal row should initially offer analysis")
	}
	q := f.st.Storage.Data[0]
	a.st.DiskUsage = map[string]model.DiskUsage{q.Path: {Path: q.Path, Running: true}}
	a.Update(duDoneMsg{path: q.Path, usage: model.DiskUsage{Path: q.Path, Total: 1024, At: f.clock.Now()}})
	if !strings.Contains(screen(a), "1K used") {
		t.Fatal("finished analysis did not update the personal table row")
	}
}

func TestStorageScopeSnapshots(t *testing.T) {
	f := storageScopeFixture(t)
	for _, width := range []int{40, 60, 80, 100, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := f.app(t, width, 20, false)
			press(a, "6")
			got := screen(a)
			checkSize(t, got, width, 20)
			golden(t, fmt.Sprintf("storage-scopes-%d", width), got)
		})
	}
}
