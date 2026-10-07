package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

func TestPersonalQuotaAndSharedFilesystem(t *testing.T) {
	f := execx.NewFake()
	argv := []string{"quota", "-w", "-u", "alice"}
	f.Set(argv, execx.FakeResponse{Stdout: []byte("Disk quotas for user alice:\nFilesystem blocks quota limit grace files quota limit grace\n/dev/sda 10 20 30 - 1 2 3 -\n")})
	c := checker(f)
	l := Location{Label: "Home", Path: "/home/alice", Real: "/home/alice", Backend: "quota", Mount: Mount{Device: "/dev/sda"}}
	q := c.Check(context.Background(), []Location{l})[0]
	if q.IsFilesystemTotal || q.UsedBytes != 10*1024 || q.Filesystem == nil || q.Filesystem.UsedBytes != 750 || !q.Filesystem.IsFilesystemTotal {
		t.Fatalf("personal and shared values were mixed: %+v", q)
	}
	encoded, err := json.Marshal(q)
	if err != nil || strings.Contains(string(encoded), `"Filesystem":`) {
		t.Fatalf("internal filesystem data changed the public JSON shape: %s %v", encoded, err)
	}
	c.StatFS = func(string) (FSStat, error) { return FSStat{}, errors.New("filesystem offline") }
	q = c.Check(context.Background(), []Location{l})[0]
	if q.Err != "" || q.UsedBytes != 10*1024 || q.Filesystem == nil || q.Filesystem.Err != "filesystem offline" || q.Filesystem.UsedBytes != 750 {
		t.Fatalf("filesystem failure hid the personal quota: %+v", q)
	}
	f.Set([]string{gpfsBin, "-u", "alice", "-Y", "--block-size", "1K"}, execx.FakeResponse{ExitCode: 1, Stderr: []byte("quota unavailable")})
	c = checker(f)
	l.Backend = "gpfs"
	q = c.Check(context.Background(), []Location{l})[0]
	if q.Err == "" || q.Filesystem == nil || q.Filesystem.Err != "" || q.Filesystem.UsedBytes != 750 {
		t.Fatalf("quota failure hid available shared filesystem data: %+v", q)
	}
}
