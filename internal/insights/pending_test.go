package insights

import (
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestEveryReasonCode(t *testing.T) {
	cases := map[string]struct {
		code  string
		never bool
	}{
		"None":                     {"None", false},
		"Priority":                 {"Priority", false},
		"Resources":                {"Resources", false},
		"Dependency":               {"Dependency", false},
		"DependencyNeverSatisfied": {"DependencyNeverSatisfied", true},
		"JobHeldUser":              {"JobHeldUser", false},
		"JobHeldAdmin":             {"JobHeldAdmin", false},
		"BeginTime":                {"BeginTime", false},
		"ReqNodeNotAvail, Reserved for maintenance":                                                   {"ReqNodeNotAvail", false},
		"ReqNodeNotAvail, UnavailableNodes:gpu[01-02]":                                                {"ReqNodeNotAvail", false},
		"Nodes required for job are DOWN, DRAINED or reserved for jobs in higher priority partitions": {"ReqNodeNotAvail", false},
		"PartitionTimeLimit":            {"PartitionTimeLimit", true},
		"PartitionNodeLimit":            {"PartitionNodeLimit", false},
		"PartitionDown":                 {"PartitionDown", false},
		"PartitionInactive":             {"PartitionInactive", false},
		"QOSMaxGRESPerUser":             {"QOSMax…PerUser", false},
		"QOSMaxJobsPerUserLimit":        {"QOSMax…PerUser", false},
		"QOSGrpGRES":                    {"QOSGrp…", false},
		"QOSMaxWallDurationPerJobLimit": {"QOSMax…", false},
		"AssocGrpGRES":                  {"AssocGrp…", false},
		"AssocMaxJobsLimit":             {"AssocMax…", false},
		"JobArrayTaskLimit":             {"JobArrayTaskLimit", false},
		"Reservation":                   {"Reservation", false},
		"Licenses":                      {"Licenses", false},
		"BadConstraints":                {"BadConstraints", true},
		"InvalidAccount":                {"InvalidAccount", true},
		"InvalidQOS":                    {"InvalidQOS", true},
		"launch failed requeued held":   {"launch failed requeued held", false},
	}
	for reason, want := range cases {
		p := Explain(reason)
		if !p.Known || p.Code != want.code || p.Never != want.never || p.Explanation == "" {
			t.Errorf("Explain(%q) = %+v, want code %q never %v", reason, p, want.code, want.never)
		}
	}
	unk := Explain("SomeFutureReason")
	if unk.Known || !strings.Contains(unk.Action, "man squeue") || !strings.Contains(unk.Explanation, "SomeFutureReason") {
		t.Fatalf("unknown = %+v", unk)
	}
	if Explain("").Code != "(none)" {
		t.Fatal("empty reason")
	}
}

func TestWhyCard(t *testing.T) {
	j := model.Job{State: model.StatePending, Reason: "Resources", QueueRank: 2, QueueTotal: 5, Partition: "gpu"}
	c := WhyCard(j, "today 14:05")
	if c.Headline != "PENDING · Resources · #2 of 5 in partition gpu" {
		t.Fatalf("headline = %q", c.Headline)
	}
	joined := strings.Join(c.Lines, "\n")
	if !strings.Contains(joined, "today 14:05") || !strings.Contains(joined, "backfill") {
		t.Fatalf("lines = %v", c.Lines)
	}
	p := WhyCard(model.Job{Reason: "Priority", QueueRank: 3, QueueTotal: 9}, "")
	if !strings.Contains(p.Lines[0], "#3 of 9") {
		t.Fatalf("priority = %v", p.Lines)
	}
	d := WhyCard(model.Job{Reason: "Dependency", Dependency: "afterok:790(unfulfilled)"}, "")
	if !strings.Contains(d.Lines[0], "afterok:790") {
		t.Fatalf("dependency = %v", d.Lines)
	}
}

func TestPriorityLine(t *testing.T) {
	got := PriorityLine(model.PriorityFactors{Priority: 5120, Age: 512, FairShare: 3072, JobSize: 256, Partition: 1000, QOS: 280})
	if got != "5120 = fairshare 3072 + partition 1000 + age 512 + QOS 280 + job size 256" {
		t.Fatal(got)
	}
	if got := PriorityLine(model.PriorityFactors{Priority: 7}); got != "7" {
		t.Fatal(got)
	}
}
