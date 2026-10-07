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
	if n := calls.Load(); n != MaxConcurrent {
		t.Fatalf("blocked checks multiplied: %d", n)
	}
}
