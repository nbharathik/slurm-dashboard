package storage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBlockedStorageChecksRemainBounded(t *testing.T) {
	var calls atomic.Int32
	blocked := make(chan struct{})
	defer close(blocked)
	c := &Checker{StatFS: func(string) (FSStat, error) {
		calls.Add(1)
		<-blocked
		return FSStat{Total: 10, Free: 5, Avail: 5}, nil
	}}
	locs := []Location{{Label: "a", Path: "a", Real: "a", Backend: "statfs"}, {Label: "b", Path: "b", Real: "b", Backend: "statfs"}, {Label: "c", Path: "c", Real: "c", Backend: "statfs"}}
	for range 5 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		start := time.Now()
		got := c.Check(ctx, locs)
		cancel()
		if time.Since(start) > time.Second {
			t.Fatal("blocked filesystem prevented cancellation")
		}
		if len(got) != 3 {
			t.Fatal("lost locations")
		}
	}
	// Each hung location is read once and never again while it hangs.
	if n := calls.Load(); n != int32(len(locs)) {
		t.Fatalf("blocked checks: %d reads for %d locations", n, len(locs))
	}
}

// Two hung mounts do not stop a healthy location from being read.
func TestHealthyLocationReadWhileOthersHang(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	c := &Checker{StatFS: func(path string) (FSStat, error) {
		if path != "ok" {
			<-blocked
		}
		return FSStat{Total: 10, Free: 5, Avail: 5}, nil
	}}
	hung := []Location{{Label: "a", Path: "a", Real: "a", Backend: "statfs"}, {Label: "b", Path: "b", Real: "b", Backend: "statfs"}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_ = c.Check(ctx, hung)
	cancel()
	ok := Location{Label: "ok", Path: "ok", Real: "ok", Backend: "statfs"}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got := c.Check(ctx, append(hung, ok))
	if got[2].Err != "" || got[2].UsedBytes != 5 {
		t.Fatalf("healthy location = %+v", got[2])
	}
	if got[0].Err == "" || got[1].Err == "" {
		t.Fatalf("hung locations not reported: %+v %+v", got[0], got[1])
	}
}
