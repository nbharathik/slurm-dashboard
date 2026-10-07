package insights

import (
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestStorageHistorySeparatesPersonalAndFilesystem(t *testing.T) {
	now := time.Unix(1700000000, 0)
	q := model.Quota{Path: "/home/alice", UsedBytes: 10, HardBytes: 100}
	fs := model.Quota{Path: q.Path, UsedBytes: 900, HardBytes: 1000, IsFilesystemTotal: true}
	q.Filesystem = &fs
	l := New(nil, now)
	if got := l.Add([]model.Quota{q}, now); len(got) != 2 {
		t.Fatalf("want two separate storage samples, got %+v", got)
	}
	personal, shared := l.Series(KeyOf(q)), l.Series(KeyOf(fs))
	if len(personal) != 1 || len(shared) != 1 || personal[0].Used != 10 || shared[0].Used != 900 || KeyOf(q) == KeyOf(fs) {
		t.Fatalf("storage histories were mixed: %+v %+v", personal, shared)
	}
	q.Err = "quota unavailable"
	if got := l.Add([]model.Quota{q}, now.Add(MinGap)); len(got) != 1 || got[0].Key != KeyOf(fs) {
		t.Fatalf("fresh shared data was dropped with a failed personal quota: %+v", got)
	}
}
