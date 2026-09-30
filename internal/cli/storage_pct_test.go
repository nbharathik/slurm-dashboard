package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/insights"
	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/report"
)

// `sdash status` printed "8.9T of 9.8T (91%)" while the alert said 92%:
// one rounded down, the other to nearest. Both now use Quota.Usage.
func TestStoragePercentAgrees(t *testing.T) {
	const tib = int64(1) << 40
	q := model.Quota{Label: "Home", UsedBytes: 897 * tib / 100, HardBytes: 98 * tib / 10, IsFilesystemTotal: true}
	line := quotaLine(report.FromQuota(q))
	got := insights.Compute(insights.Input{Now: time.Now(), Storage: []model.Quota{q}})
	if !strings.Contains(line, "(91%)") || len(got) != 1 || !strings.Contains(got[0].Message, " 91% ") {
		t.Fatalf("status %q, alerts %+v", line, got)
	}
	if got[0].Level != model.Info {
		t.Errorf("a shared filesystem alert is %v, want info", got[0].Level)
	}
}
