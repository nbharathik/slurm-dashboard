package insights

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Pending is the explanation of a pending reason.
type Pending struct {
	Code        string // the reason code as matched ("Resources")
	Explanation string
	Action      string // what the user can do, or ""
	Never       bool   // the job can never start as submitted
	Known       bool   // false: unknown code, see man squeue
}

type rule struct {
	match       func(string) bool
	code        string
	explanation string
	action      string
	never       bool
}

func exact(code string) func(string) bool { return func(s string) bool { return s == code } }
func prefix(p string) func(string) bool {
	return func(s string) bool { return strings.HasPrefix(s, p) }
}

func contains(p string) func(string) bool {
	return func(s string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(p)) }
}

var qosPerUser = regexp.MustCompile(`^QOSMax.*PerUser`)

// rules are tried in order: exact codes, then prefixes, then free text.
var rules = []rule{
	{exact("None"), "None", "Just submitted; the scheduler has not evaluated it yet.", "Wait one scheduling cycle.", false},
	{exact("Priority"), "Priority", "Higher-priority jobs are ahead of you.", "See your fairshare and priority factors.", false},
	{exact("Resources"), "Resources", "You are next; waiting for enough free resources.", "A shorter --time lets the backfill scheduler start it sooner.", false},
	{exact("Dependency"), "Dependency", "Waiting for the job(s) it depends on to finish.", "Jump to the dependency.", false},
	{exact("DependencyNeverSatisfied"), "DependencyNeverSatisfied", "A job it depends on failed or was cancelled; this job will never start.", "Cancel it and resubmit without the dependency.", true},
	{exact("JobHeldUser"), "JobHeldUser", "You held this job.", "Release it.", false},
	{exact("JobHeldAdmin"), "JobHeldAdmin", "An administrator held this job.", "Contact support to find out why.", false},
	{exact("BeginTime"), "BeginTime", "It starts no earlier than its --begin time.", "", false},
	{prefix("ReqNodeNotAvail"), "ReqNodeNotAvail", "Requested nodes are down, drained or reserved (for example for maintenance).", "Check the reservations; a --time that ends before the maintenance lets it run now.", false},
	{contains("Nodes required for job are DOWN, DRAINED or reserved"), "ReqNodeNotAvail", "Requested nodes are down, drained or reserved for jobs in higher-priority partitions.", "Check node states and reservations, or submit to another partition.", false},
	{exact("PartitionTimeLimit"), "PartitionTimeLimit", "The time requested exceeds the partition's maximum; it will never start.", "Rerun it with a shorter time limit (n in Usage) or use a partition with a longer limit.", true},
	{exact("PartitionNodeLimit"), "PartitionNodeLimit", "The node count is outside the partition's limits.", "", false},
	{exact("PartitionDown"), "PartitionDown", "The partition is not running jobs.", "", false},
	{exact("PartitionInactive"), "PartitionInactive", "The partition is not running jobs.", "", false},
	{func(s string) bool { return qosPerUser.MatchString(s) }, "QOSMax…PerUser", "You hit a per-user limit of your QOS (for example the maximum GPUs).", "It starts when your other jobs finish.", false},
	{prefix("QOSGrp"), "QOSGrp…", "The QOS-wide limit shared by all its users is reached.", "Wait.", false},
	{prefix("QOSMax"), "QOSMax…", "The job exceeds a maximum of its QOS.", "Request less or use another QOS.", false},
	{prefix("AssocGrp"), "AssocGrp…", "Your account's limit for this resource is reached.", "Wait, or ask the account owner.", false},
	{prefix("AssocMax"), "AssocMax…", "Your account's limit for this resource is reached.", "Wait, or ask the account owner.", false},
	{exact("JobArrayTaskLimit"), "JobArrayTaskLimit", "The array's throttle (%N) limits how many tasks run at once.", "The next task starts when a running one finishes.", false},
	{exact("Reservation"), "Reservation", "Waiting for its reservation to start.", "", false},
	{exact("Licenses"), "Licenses", "Waiting for licences.", "", false},
	{exact("BadConstraints"), "BadConstraints", "No node satisfies the requested constraints; it will never start.", "Fix --constraint and resubmit.", true},
	{exact("InvalidAccount"), "InvalidAccount", "The account is not valid for you; it will never start.", "Resubmit with a valid --account.", true},
	{exact("InvalidQOS"), "InvalidQOS", "The QOS is not valid for you; it will never start.", "Resubmit with a valid --qos.", true},
	{contains("launch failed requeued held"), "launch failed requeued held", "Launch failed; the job was requeued and held.", "Release it after checking the node.", false},
}

// Explain explains a pending reason. Exact codes win, then prefixes, then
// known sentences; anything else points to "man squeue".
func Explain(reason string) Pending {
	r := strings.TrimSpace(reason)
	for _, ru := range rules {
		if ru.match(r) {
			return Pending{Code: ru.code, Explanation: ru.explanation, Action: ru.action, Never: ru.never, Known: true}
		}
	}
	if r == "" {
		r = "(none)"
	}
	return Pending{
		Code:        r,
		Explanation: fmt.Sprintf("Slurm reports %q.", r),
		Action:      `See "JOB REASON CODES" in man squeue.`,
	}
}

// Card is the why-pending card for a job.
type Card struct {
	Headline string   // "PENDING · Resources · #2 of 5 in partition gpu"
	Lines    []string // explanation, estimate, request, tip
	Pending  Pending
}

// WhyCard builds the card shown for a pending job.
func WhyCard(j model.Job, estimate string) Card {
	p := Explain(j.Reason)
	c := Card{Pending: p}
	head := []string{"PENDING", p.Code}
	if j.QueueRank > 0 {
		head = append(head, fmt.Sprintf("#%d of %d in partition %s", j.QueueRank, j.QueueTotal, j.Partition))
	}
	c.Headline = strings.Join(head, " · ")
	expl := p.Explanation
	if p.Code == "Priority" && j.QueueRank > 0 {
		expl = fmt.Sprintf("Higher-priority jobs are ahead of you (#%d of %d).", j.QueueRank, j.QueueTotal)
	}
	if p.Code == "Dependency" && j.Dependency != "" {
		expl = "Waiting for " + j.Dependency + "."
	}
	c.Lines = append(c.Lines, expl)
	if estimate != "" {
		c.Lines = append(c.Lines, "Estimated start: "+estimate+" (scheduler estimate, can change)")
	}
	if p.Action != "" {
		c.Lines = append(c.Lines, "Tip: "+p.Action)
	}
	return c
}

// PriorityLine explains a pending job's priority from its sprio factors:
// "5120 = fairshare 3072 + partition 1000 + age 512 + ...".
func PriorityLine(pf model.PriorityFactors) string {
	type part struct {
		name string
		v    int64
	}
	parts := []part{{"age", pf.Age}, {"fairshare", pf.FairShare}, {"job size", pf.JobSize}, {"partition", pf.Partition}, {"QOS", pf.QOS}}
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].v > parts[j].v })
	var terms []string
	for _, p := range parts {
		if p.v != 0 {
			terms = append(terms, fmt.Sprintf("%s %d", p.name, p.v))
		}
	}
	if len(terms) == 0 {
		return strconv.FormatInt(pf.Priority, 10)
	}
	return fmt.Sprintf("%d = %s", pf.Priority, strings.Join(terms, " + "))
}

// PriorityFor finds a job's sprio factors; an array's pending tasks share
// the array job ID.
func PriorityFor(pfs []model.PriorityFactors, j model.Job) (model.PriorityFactors, bool) {
	arr := strconv.FormatUint(j.ID.ArrayJobID, 10)
	for _, pf := range pfs {
		if pf.JobID == j.ID.Raw || pf.JobID == arr {
			return pf, true
		}
	}
	return model.PriorityFactors{}, false
}
