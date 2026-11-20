package execx

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Time spent waiting for a slot does not count against a command's own
// timeout, so a burst of job-end hooks all run.
func TestTimeoutStartsWithTheSlot(t *testing.T) {
	r := testRunner(Options{MaxConcurrent: 1})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = r.Run(context.Background(), "sh", "-c", "sleep 0.6")
	}()
	time.Sleep(100 * time.Millisecond)
	_, err := r.Run(WithTimeout(context.Background(), 500*time.Millisecond), "sh", "-c", "sleep 0.2")
	if err != nil {
		t.Fatalf("queued command lost its run time: %v", err)
	}
	wg.Wait()

	_, err = r.Run(WithTimeout(context.Background(), 100*time.Millisecond), "sh", "-c", "sleep 2")
	if err == nil {
		t.Fatal("WithTimeout did not bound the run")
	}
}
