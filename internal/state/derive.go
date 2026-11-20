package state

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

func sortTransitions(ts []Transition) {
	slices.SortFunc(ts, func(a, b Transition) int { return cmp.Compare(a.Job.ID.Raw, b.Job.ID.Raw) })
}

// JoinJobs adds what running jobs were allocated (GPUs, and memory, which
// squeue's %m shows per CPU for --mem-per-cpu jobs) and queue ranks to
// pending ones.
func JoinJobs(jobs []model.Job, running []model.RunningJob, pending []parse.PendingJob) []model.Job {
	out := slices.Clone(jobs)
	alloc := make(map[string]model.RunningJob, len(running))
	for _, r := range running {
		alloc[r.ID.Raw] = r
	}
	ranks := QueueRanks(pending)
	for i := range out {
		j := &out[i]
		if j.State == model.StateRunning {
			if r, ok := alloc[j.ID.Raw]; ok {
				*j = Allocated(*j, r)
			}
		}
		if j.State == model.StatePending {
			r, ok := ranks[j.ID.Raw]
			if !ok && j.ID.IsArray() {
				// squeue -O JobID prints a pending array as its base ID.
				r, ok = ranks[strconv.FormatUint(j.ID.ArrayJobID, 10)]
			}
			if ok {
				j.QueueRank, j.QueueTotal = r.Rank, r.Total
			}
		}
	}
	return out
}

// Rank is a pending job's position in its partition.
type Rank struct{ Rank, Total int }

// Allocated is running job j with what it holds: its GPUs (which count
// --gpus requests) and memory per node.
func Allocated(j model.Job, r model.RunningJob) model.Job {
	j.GPUs = r.GPUs
	if r.MemMB > 0 {
		j.MemPerNodeMB = r.MemMB / int64(max(j.Nodes, len(r.NodeList), 1))
	}
	return j
}

// QueueRanks ranks pending jobs within each partition by priority
// (descending), breaking ties by job ID. A job submitted to several
// partitions is ranked in each and keeps its best place.
func QueueRanks(pending []parse.PendingJob) map[string]Rank {
	byPart := map[string][]parse.PendingJob{}
	for _, p := range pending {
		for _, part := range strings.Split(p.Partition, ",") {
			if part == "" {
				continue
			}
			q := p
			q.Partition = part
			byPart[part] = append(byPart[part], q)
		}
	}
	out := make(map[string]Rank, len(pending))
	for _, list := range byPart {
		slices.SortStableFunc(list, func(a, b parse.PendingJob) int {
			if c := cmp.Compare(b.Priority, a.Priority); c != 0 {
				return c
			}
			return compareIDs(a.ID, b.ID)
		})
		for i, p := range list {
			r := Rank{Rank: i + 1, Total: len(list)}
			if old, ok := out[p.ID]; !ok || r.Rank < old.Rank || (r.Rank == old.Rank && r.Total < old.Total) {
				out[p.ID] = r
			}
		}
	}
	return out
}

func compareIDs(a, b string) int {
	na, nb := leadingNumber(a), leadingNumber(b)
	if na != nb {
		return cmp.Compare(na, nb)
	}
	return cmp.Compare(a, b)
}

func leadingNumber(s string) uint64 {
	var n uint64
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + uint64(s[i]-'0')
	}
	return n
}

// NodeUsage is how a node's GPUs are shared.
type NodeUsage struct {
	Node   model.Node
	Mine   int            // whole GPUs held by the user's jobs (estimated)
	Others int            // whole GPUs held by other users (estimated)
	Users  map[string]int // other users' GPUs and MIG slices
	Free   int            // whole GPUs free
	// MIG slices, counted apart from whole GPUs.
	MIGMine, MIGOthers, MIGFree int
	FreeBy                      time.Time          // earliest end of a GPU job here, when none is free
	Jobs                        []model.RunningJob // running jobs on this node, soonest end first
	Estimate                    bool               // counts spread multi-node jobs evenly
}

// GPUShare is the GPUs (whole GPUs and MIG slices) a running job holds on
// each of its nodes, assuming an even spread.
func GPUShare(r model.RunningJob) int { return spread(r.GPUs, len(r.NodeList)) }

// spread divides n over nodes, rounding up.
func spread(n, nodes int) int {
	if nodes == 0 || n <= 0 {
		return 0
	}
	return (n + nodes - 1) / nodes
}

// NodeGPUUsage combines nodes with running jobs; me is the current user.
// With PrivateData=jobs, GPUs not explained by visible jobs count as Others.
func NodeGPUUsage(nodes []model.Node, running []model.RunningJob, me string) []NodeUsage {
	onNode := map[string][]model.RunningJob{}
	for _, r := range running {
		for _, n := range r.NodeList {
			onNode[n] = append(onNode[n], r)
		}
	}
	out := make([]NodeUsage, 0, len(nodes))
	for _, n := range nodes {
		u := NodeUsage{Node: n, Users: map[string]int{}}
		jobs := slices.Clone(onNode[n.Name])
		slices.SortFunc(jobs, func(a, b model.RunningJob) int { return a.EndTime.Compare(b.EndTime) })
		u.Jobs = jobs
		for _, r := range jobs {
			full := spread(r.GPUs-r.MIGSlices, len(r.NodeList))
			mig := spread(r.MIGSlices, len(r.NodeList))
			if n.GPUTotal == 0 && n.MIGTotal > 0 { // untyped GPUs on a MIG-only node are slices
				full, mig = 0, mig+full
			}
			if full+mig == 0 {
				continue
			}
			if len(r.NodeList) > 1 {
				u.Estimate = true
			}
			if r.User == me {
				u.Mine += full
				u.MIGMine += mig
			} else {
				u.Others += full
				u.MIGOthers += mig
				u.Users[r.User] += full + mig
			}
			if u.FreeBy.IsZero() || (!r.EndTime.IsZero() && r.EndTime.Before(u.FreeBy)) {
				u.FreeBy = r.EndTime
			}
		}
		// Untyped GPU counts on a node with whole GPUs and MIG slices:
		// what exceeds the allocated whole GPUs must sit on slices.
		if excess := u.Mine + u.Others - n.GPUAlloc; excess > 0 && n.MIGTotal > 0 {
			move := min(excess, max(n.MIGAlloc-u.MIGMine-u.MIGOthers, 0))
			fromOthers := min(move, u.Others)
			u.Others, u.MIGOthers = u.Others-fromOthers, u.MIGOthers+fromOthers
			u.Mine, u.MIGMine = u.Mine-(move-fromOthers), u.MIGMine+(move-fromOthers)
			u.Estimate = u.Estimate || move > 0
		}
		if hidden := n.GPUAlloc - u.Mine - u.Others; hidden > 0 {
			u.Others += hidden
			u.Users["others"] += hidden
		}
		if hidden := n.MIGAlloc - u.MIGMine - u.MIGOthers; hidden > 0 {
			u.MIGOthers += hidden
			u.Users["others"] += hidden
		}
		u.Free = max(n.GPUTotal-max(n.GPUAlloc, u.Mine+u.Others), 0)
		u.MIGFree = max(n.MIGTotal-max(n.MIGAlloc, u.MIGMine+u.MIGOthers), 0)
		if unavailable(n) {
			u.Free, u.MIGFree = 0, 0
		}
		if u.Free > 0 || (n.GPUTotal == 0 && u.MIGFree > 0) {
			u.FreeBy = time.Time{}
		}
		out = append(out, u)
	}
	return out
}

// unavailable reports nodes that cannot take new jobs now: down, drained,
// reserved, planned for a waiting job, rebooting or changing power state.
func unavailable(n model.Node) bool {
	switch n.State {
	case "DOWN", "DRAIN", "DRAINED", "DRAINING", "FAIL", "FAILING", "FUTURE", "UNKNOWN", "POWERED_DOWN", "MAINT", "RESERVED", "PLANNED":
		return true
	}
	for _, f := range []string{"DRAIN", "NOT_RESPONDING", "MAINTENANCE", "POWERED_DOWN", "FAIL", "RESERVED", "PLANNED", "POWERING_DOWN", "POWERING_UP", "POWER_DOWN", "REBOOT_REQUESTED", "REBOOT_ISSUED"} {
		if n.HasFlag(f) {
			return true
		}
	}
	return false
}

// Available reports whether a node can accept jobs.
func Available(n model.Node) bool { return !unavailable(n) }

// Down reports a node that is broken or taken out of service: down,
// drained or draining, failing or not responding. A node that is only
// planned, reserved, rebooting or powered down is not down.
func Down(n model.Node) bool {
	switch n.State {
	case "DOWN", "DRAIN", "DRAINED", "DRAINING", "FAIL", "FAILING":
		return true
	}
	return n.HasFlag("DRAIN") || n.HasFlag("FAIL") || n.HasFlag("NOT_RESPONDING")
}

// Reserved reports a healthy node held for a waiting job, a reservation
// or maintenance.
func Reserved(n model.Node) bool {
	if Down(n) {
		return false
	}
	switch n.State {
	case "PLANNED", "RESERVED", "MAINT":
		return true
	}
	return n.HasFlag("PLANNED") || n.HasFlag("RESERVED") || n.HasFlag("MAINTENANCE")
}

// GPUNodes returns only nodes with GPUs or MIG slices.
func GPUNodes(usage []NodeUsage) []NodeUsage {
	var out []NodeUsage
	for _, u := range usage {
		if u.Node.HasGPUs() {
			out = append(out, u)
		}
	}
	return out
}

// GPUTotals are whole GPUs and MIG slices over a set of nodes, kept apart
// so that "20 of 21 GPUs free" never mixes slices with whole GPUs.
type GPUTotals struct {
	Total, Free, MIGTotal, MIGFree, NodesWithFree int
}

// SumGPUs adds up node usage.
func SumGPUs(usage []NodeUsage) GPUTotals {
	var t GPUTotals
	for _, u := range usage {
		t.Total += u.Node.GPUTotal
		t.Free += u.Free
		t.MIGTotal += u.Node.MIGTotal
		t.MIGFree += u.MIGFree
		if u.Free > 0 || u.MIGFree > 0 {
			t.NodesWithFree++
		}
	}
	return t
}

// SortByFree orders node usage by free GPUs (most first), then name.
func SortByFree(usage []NodeUsage) {
	slices.SortStableFunc(usage, func(a, b NodeUsage) int {
		if c := cmp.Compare(b.Free, a.Free); c != 0 {
			return c
		}
		return strings.Compare(a.Node.Name, b.Node.Name)
	})
}

// PartitionsOfPending returns the partitions of the user's pending jobs,
// which is what the queuerank query needs.
func PartitionsOfPending(jobs []model.Job) []string {
	var out []string
	for _, j := range jobs {
		if j.State != model.StatePending {
			continue
		}
		for _, p := range strings.Split(j.Partition, ",") {
			if p != "" && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Counts summarises jobs by state.
type Counts struct {
	Running, Pending, Other int
}

// CountJobs counts jobs by state.
func CountJobs(jobs []model.Job) Counts { return count(jobs, func(model.Job) int { return 1 }) }

// CountTasks is CountJobs with a pending range counted as its tasks.
func CountTasks(jobs []model.Job) Counts {
	return count(jobs, func(j model.Job) int { return j.ID.Tasks() })
}

func count(jobs []model.Job, n func(model.Job) int) Counts {
	var c Counts
	for _, j := range jobs {
		switch j.State {
		case model.StateRunning, model.StateCompleting, model.StateConfiguring:
			c.Running += n(j)
		case model.StatePending:
			c.Pending += n(j)
		default:
			c.Other += n(j)
		}
	}
	return c
}

// PendingJobs turns the pending jobs of a full queue listing into what the
// queue-rank query returns, so ranks can come from it without a query.
func PendingJobs(jobs []model.Job) []parse.PendingJob {
	var out []parse.PendingJob
	for _, j := range jobs {
		if j.State == model.StatePending {
			out = append(out, parse.PendingJob{ID: j.ID.Raw, Partition: j.Partition, Priority: j.Priority})
		}
	}
	return out
}

// PendingIn keeps the pending jobs in any of partitions, once per matching
// partition, as the queue-rank query lists them.
func PendingIn(jobs []parse.PendingJob, partitions []string) []parse.PendingJob {
	var out []parse.PendingJob
	for _, j := range jobs {
		for _, p := range strings.Split(j.Partition, ",") {
			if slices.Contains(partitions, p) {
				out = append(out, parse.PendingJob{ID: j.ID, Partition: p, Priority: j.Priority})
			}
		}
	}
	return out
}
