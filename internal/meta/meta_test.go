package meta

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestInfoUsesLinkerValues(t *testing.T) {
	oldV, oldC, oldD := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })
	Version, Commit, Date = "v9.9.9", "abc1234", "2026-09-28T00:00:00Z"

	info := Info()
	if info.Name != AppName || info.Version != "v9.9.9" || info.Commit != "abc1234" || info.Date != "2026-09-28T00:00:00Z" {
		t.Fatalf("Info() = %+v", info)
	}
	if !strings.HasPrefix(info.GoVersion, "go") || !strings.Contains(info.Platform, "/") {
		t.Fatalf("Info() go/platform = %q %q", info.GoVersion, info.Platform)
	}
}

func TestApplyVCSFallback(t *testing.T) {
	info := BuildInfo{Commit: "none", Date: "unknown"}
	applyVCS(&info, []debug.BuildSetting{
		{Key: "vcs.modified", Value: "true"},
		{Key: "vcs.revision", Value: "deadbeef"},
		{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
	})
	if info.Commit != "deadbeef-dirty" || info.Date != "2026-01-02T03:04:05Z" {
		t.Fatalf("applyVCS = %+v", info)
	}
}

func TestApplyVCSKeepsLinkerValues(t *testing.T) {
	info := BuildInfo{Commit: "abc1234", Date: "2026-09-28"}
	applyVCS(&info, []debug.BuildSetting{
		{Key: "vcs.revision", Value: "deadbeef"},
		{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
	})
	if info.Commit != "abc1234" || info.Date != "2026-09-28" {
		t.Fatalf("applyVCS overwrote linker values: %+v", info)
	}
}
