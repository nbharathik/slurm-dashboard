package parse

import (
	"bytes"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// bigQueue is the squeue output of a large cluster: n jobs over 200 users,
// 8 partitions, 20 accounts and 5,000 nodes.
func bigQueue(n int) []byte {
	var b bytes.Buffer
	for i := range n {
		id := strconv.Itoa(1_000_000 + i)
		f := []string{
			id, id, "N/A", "part" + strconv.Itoa(i%8), "normal", "acct" + strconv.Itoa(i%20), "RUNNING",
			"18:45", "5:00:00", "4:41:15", "1", "4", "1000M", "N/A", "None", "node" + strconv.Itoa(i%5000),
			"2026-09-28T12:52:41", "2026-09-28T17:52:41", "2026-09-28T12:52:41", "10111", "(null)",
			"user" + strconv.Itoa(i%200), "job-" + id,
		}
		b.WriteString(strings.Join(f, Sep))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func parseOnce(n int) ([]model.Job, []model.ParseWarning) { return MyJobs(bigQueue(n)) }

func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A parsed job keeps no part of its output line, and values that repeat
// (user, partition, account, node) are stored once. Budget in 5.5 of the
// improvement plan; lower it when the job record shrinks.
func TestLargeQueueMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 50,000-job queue")
	}
	const n = 50_000
	before := liveHeap()
	// The output goes out of scope inside parseOnce; only the jobs stay.
	jobs, warns := parseOnce(n)
	after := liveHeap()
	if len(jobs) != n || len(warns) != 0 {
		t.Fatalf("parsed %d jobs, %d warnings", len(jobs), len(warns))
	}
	perJob := int64(after-before) / n
	const budget = 640 // bytes per job, the whole record included (528 measured)
	if perJob > budget {
		t.Errorf("%d bytes per job, budget %d", perJob, budget)
	}
	t.Logf("%d bytes per job", perJob)
	// Interned: every job of user0 points at the same bytes.
	if unsafe.StringData(jobs[0].User) != unsafe.StringData(jobs[200].User) {
		t.Error("user names are not interned")
	}
	if unsafe.StringData(jobs[0].Partition) != unsafe.StringData(jobs[8].Partition) {
		t.Error("partitions are not interned")
	}
	runtime.KeepAlive(jobs)
}

func BenchmarkMyJobs50k(b *testing.B) {
	raw := bigQueue(50_000)
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		_, _ = MyJobs(raw)
	}
}
