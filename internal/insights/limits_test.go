package insights

import (
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestLimits(t *testing.T) {
	scopes := []model.LimitScope{
		{Kind: "user", Name: "research", Limits: []model.Limit{
			{Name: "GrpTRES cpu", Max: 64, Used: 60},
			{Name: "GrpTRES gres/gpu", Max: 4, Used: 1},
			{Name: "GrpTRESMins cpu", Unit: "min", Max: 100000, Used: 6000},
			{Name: "MaxJobs", Max: 20, Used: 3},
			{Name: "MaxSubmitJobs", Max: 50, Used: -1},
			{Name: "MaxWall", Unit: "min", Max: 720, Used: -1},
			{Name: "MaxTRESPJ cpu", Max: 16, Used: -1},
			{Name: "GrpTRES mem", Unit: "MB", Max: 262144, Used: 65536},
		}},
		{Kind: "account", Name: "research", Limits: []model.Limit{{Name: "GrpJobs", Max: 100, Used: 0}}},
		{Kind: "qos", Name: "normal", Limits: []model.Limit{{Name: "MaxWall", Unit: "min", Max: 1440, Used: -1}}},
	}
	want := []struct {
		scope, what, text string
		near              bool
	}{
		{"you in research", "CPUs in use", "60 of 64 (94%)", true},
		{"you in research", "GPUs in use", "1 of 4 (25%)", false},
		{"you in research", "CPUs time spent", "100 h of 1667 h (6%)", false},
		{"you in research", "running jobs", "3 of 20 (15%)", false},
		{"you in research", "submitted jobs", "limit 50", false},
		{"you in research", "longest job", "up to 12h", false},
		{"you in research", "CPUs per job", "up to 16", false},
		{"you in research", "memory in use", "64 GB of 256 GB (25%)", false},
		{"account research", "running jobs (whole group)", "0 of 100 (0%)", false},
		{"QOS normal", "longest job", "up to 1d", false},
	}
	got := Limits(scopes)
	if got[0].Owner != "your" || got[8].Owner != "account research" || got[9].Owner != "QOS normal" {
		t.Errorf("owners = %q %q %q", got[0].Owner, got[8].Owner, got[9].Owner)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Scope != w.scope || g.What != w.what || g.Text != w.text || g.Near() != w.near {
			t.Errorf("line %d = {%q %q %q near=%v}, want %+v", i, g.Scope, g.What, g.Text, g.Near(), w)
		}
	}
	if len(Limits(nil)) != 0 {
		t.Error("no scopes must give no lines")
	}
}

func TestStandingAndPending(t *testing.T) {
	shares := []model.Share{
		{Account: "research", RawShares: 1, NormShares: 0.25, EffectiveUsage: 0.18, FairShare: 0.62},
		{Account: "research", User: "alice", NormShares: 0.25, EffectiveUsage: 0.4, FairShare: 0.3},
		{Account: "other", User: "bob", FairShare: 0.9},
	}
	st := FairShareStanding(shares, "alice")
	if len(st) != 1 || st[0].Account != "research" || st[0].UsedPct != 40 || st[0].Verdict != "over your share: others' jobs go first" {
		t.Errorf("standing = %+v", st)
	}
	for f, want := range map[float64]string{0.9: "well under your share: your jobs are favoured", 0.5: "under your share", 0.01: "far over your share: expect to wait"} {
		if got := standingVerdict(f); got != want {
			t.Errorf("verdict(%v) = %q, want %q", f, got, want)
		}
	}

	pp := PendingPriorities([]model.PriorityFactors{
		{JobID: "812", Priority: 5120, Age: 512, FairShare: 3072, Partition: 1000},
		{JobID: "813", Priority: 1},
	})
	if pp[0].Biggest != "fairshare 3072" || pp[0].Line != "5120 = fairshare 3072 + partition 1000 + age 512" {
		t.Errorf("pending = %+v", pp[0])
	}
	if pp[1].Biggest != "" || pp[1].Line != "1" {
		t.Errorf("pending without factors = %+v", pp[1])
	}
}

func TestBuildAccountNotes(t *testing.T) {
	cases := []struct {
		name         string
		in           AccountInput
		limits, prio string
	}{
		{"no accounting", AccountInput{NoAccounting: true}, "this cluster has no accounting database, so Slurm sets no account or QOS limits", "this cluster has no accounting database, so there is no fairshare"},
		{"basic priority", AccountInput{LimitsRead: true, BasicPriority: true}, "no account or QOS limit is set for you", "PriorityType=priority/basic has no fairshare or priority factors"},
		{"hidden", AccountInput{LimitsErr: "permission denied", PriorityErr: "denied"}, "limits are hidden by the site or could not be read: permission denied", "fairshare could not be read: denied"},
		{"not yet", AccountInput{}, "limits have not been read yet", "fairshare has not been read yet"},
		{"no row", AccountInput{PriorityRead: true, User: "alice", Shares: []model.Share{{Account: "x", User: "bob"}}}, "limits have not been read yet", "you have no fairshare row"},
	}
	for _, c := range cases {
		a := BuildAccount(c.in)
		if a.LimitsNote != c.limits || a.PriorityNote != c.prio {
			t.Errorf("%s: notes = %q / %q, want %q / %q", c.name, a.LimitsNote, a.PriorityNote, c.limits, c.prio)
		}
	}
	ok := BuildAccount(AccountInput{
		User: "alice", LimitsRead: true, PriorityRead: true,
		Scopes: []model.LimitScope{{Kind: "user", Name: "x", Limits: []model.Limit{{Name: "MaxJobs", Max: 5, Used: 5}}}},
		Shares: []model.Share{{Account: "x", User: "alice", FairShare: 0.6}},
	})
	if ok.LimitsNote != "" || ok.PriorityNote != "" || len(ok.Limits) != 1 || !ok.Limits[0].Near() || len(ok.Standing) != 1 {
		t.Errorf("account = %+v", ok)
	}
}
