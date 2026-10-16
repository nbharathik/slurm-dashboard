package state

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestPartitionAccess(t *testing.T) {
	me := Identity{Groups: []string{"alice", "physics"}, GroupsKnown: true, Accounts: []string{"lab-a"}, AccountsKnown: true}
	two := Identity{Accounts: []string{"lab-a", "lab-b"}, AccountsKnown: true}
	cases := []struct {
		name string
		p    model.Partition
		id   Identity
		want Access
		why  string
	}{
		{"open", model.Partition{}, me, AccessAllowed, ""},
		{"group member", model.Partition{AllowGroups: []string{"chem", "physics"}}, me, AccessAllowed, ""},
		{"not in the group", model.Partition{AllowGroups: []string{"chem"}}, me, AccessDenied, "AllowGroups"},
		{"groups unknown", model.Partition{AllowGroups: []string{"chem"}}, Identity{}, AccessUnknown, "AllowGroups"},
		{"account listed", model.Partition{AllowAccounts: []string{"lab-b", "lab-a"}}, me, AccessAllowed, ""},
		{"account may match through a parent", model.Partition{AllowAccounts: []string{"dept"}}, me, AccessUnknown, "AllowAccounts"},
		{"accounts unknown", model.Partition{AllowAccounts: []string{"lab-a"}}, Identity{}, AccessUnknown, "AllowAccounts"},
		{"every account denied", model.Partition{DenyAccounts: []string{"lab-a", "x"}}, me, AccessDenied, "DenyAccounts"},
		{"one account left", model.Partition{DenyAccounts: []string{"lab-a"}}, two, AccessUnknown, "DenyAccounts"},
		{"deny list without a match", model.Partition{DenyAccounts: []string{"x"}}, me, AccessUnknown, "DenyAccounts"},
		{"deny accounts with unknown accounts", model.Partition{DenyAccounts: []string{"x"}}, Identity{}, AccessUnknown, "DenyAccounts"},
		{"allow qos", model.Partition{AllowQOS: []string{"high"}}, me, AccessUnknown, "AllowQos"},
		{"deny qos", model.Partition{DenyQOS: []string{"low"}}, me, AccessUnknown, "DenyQos"},
		{"denied beats unknown", model.Partition{AllowGroups: []string{"chem"}, AllowQOS: []string{"high"}}, me, AccessDenied, "AllowGroups"},
		{"unknown beats allowed", model.Partition{AllowGroups: []string{"physics"}, AllowQOS: []string{"high"}}, me, AccessUnknown, "AllowQos"},
		{"root", model.Partition{AllowGroups: []string{"chem"}}, Identity{Root: true}, AccessAllowed, ""},
	}
	for _, tc := range cases {
		got, why := PartitionAccess(tc.p, tc.id)
		if got != tc.want || why != tc.why {
			t.Errorf("%s: %v (%q), want %v (%q)", tc.name, got, why, tc.want, tc.why)
		}
	}
	var zero Access
	if zero != AccessUnknown || AccessAllowed.String() != "allowed" || AccessDenied.String() != "denied" || zero.String() != "unknown" {
		t.Fatal("the zero Access must be unknown")
	}
}

func TestStoreIdentityAndCapacityAccess(t *testing.T) {
	st := &Store{User: "alice", UID: "1000"}
	if id := st.Identity(); id.Root || id.GroupsKnown || id.AccountsKnown {
		t.Fatalf("empty store knows %+v", id)
	}
	parts := []model.Partition{{Name: "open", State: "UP"}, {Name: "closed", State: "UP", AllowGroups: []string{"chem"}}}
	st.Apply(Update{Source: "partitions", Data: parts, At: t0})
	if _, sums := st.Capacity(); sums[1].Access != AccessUnknown || sums[1].AccessWhy != "AllowGroups" {
		t.Fatalf("before the groups load: %+v", sums[1])
	}
	st.Apply(Update{Source: "groups", Data: []string{"alice", "physics"}, At: t0})
	shares := []model.Share{
		{Account: "lab-a", User: "alice"}, {Account: "lab-a", User: "alice"}, {Account: "lab-a"}, {Account: "lab-b", User: "bob"},
	}
	st.Apply(Update{Source: "fairshare", Data: shares, At: t0})
	id := st.Identity()
	if !id.GroupsKnown || !id.AccountsKnown || !slices.Equal(id.Accounts, []string{"lab-a"}) {
		t.Fatalf("identity = %+v", id)
	}
	_, sums := st.Capacity()
	if sums[0].Access != AccessAllowed || sums[1].Access != AccessDenied || sums[1].AccessWhy != "AllowGroups" {
		t.Fatalf("after the groups load: %+v / %+v", sums[0], sums[1])
	}
	if !(&Store{User: "root", UID: "0"}).Identity().Root {
		t.Fatal("uid 0 is root")
	}
}

func TestFitNodesSkipsClosedPartitions(t *testing.T) {
	day := 24 * time.Hour
	nodes := []model.Node{
		{Name: "a01", State: "IDLE", CPUTotal: 32},
		{Name: "b01", State: "IDLE", CPUTotal: 32},
		{Name: "c01", State: "IDLE", CPUTotal: 32},
	}
	usage := NodeGPUUsage(nodes, nil, "me")
	parts := []model.Partition{
		{Name: "open", State: "UP", MaxTime: &day, Nodes: []string{"a01"}},
		{Name: "closed", State: "UP", MaxTime: &day, Nodes: []string{"b01"}, AllowGroups: []string{"chem"}},
		{Name: "maybe", State: "UP", MaxTime: &day, Nodes: []string{"c01"}, AllowQOS: []string{"high"}},
	}
	who := Identity{Groups: []string{"physics"}, GroupsKnown: true}

	r := FitNodes(usage, parts, Request{CPUs: 8, Who: &who}, nil)
	if len(r.Now) != 2 || r.Now[0].Node != "a01" || r.Now[1].Node != "c01" {
		t.Fatalf("fits = %+v", r.Now)
	}
	if !slices.Equal(r.Denied, []string{"closed"}) || !slices.Equal(r.Unverified, []string{"maybe"}) {
		t.Fatalf("denied %q, unverified %q", r.Denied, r.Unverified)
	}

	r = FitNodes(usage, parts, Request{CPUs: 8, Partition: "closed", Who: &who}, nil)
	if len(r.Now) != 0 || len(r.Reasons) != 1 || r.Reasons[0] != "you may not submit to partition closed (AllowGroups)" {
		t.Fatalf("closed partition asked for: %+v", r)
	}
	r = FitNodes(usage, parts[1:2], Request{CPUs: 8, Who: &who}, nil)
	if len(r.Now) != 0 || len(r.Reasons) != 1 || r.Reasons[0] != "no partition you may submit to is up and allows that time limit" {
		t.Fatalf("only closed partitions: %+v", r)
	}
	r = FitNodes(usage, parts, Request{CPUs: 8, Partition: "maybe", Who: &who}, nil)
	if len(r.Now) != 1 || len(r.Denied) != 0 || !slices.Equal(r.Unverified, []string{"maybe"}) {
		t.Fatalf("unverified partition asked for: %+v", r)
	}

	r = FitNodes(usage, parts, Request{CPUs: 8}, nil)
	if len(r.Now) != 3 || len(r.Denied) != 0 || len(r.Unverified) != 0 {
		t.Fatalf("no identity must check nothing: %+v", r)
	}
	r = FitNodes(usage, parts, Request{CPUs: 8, Who: &Identity{}}, nil)
	if len(r.Now) != 3 || len(r.Denied) != 0 || !slices.Equal(r.Unverified, []string{"closed", "maybe"}) {
		t.Fatalf("unknown identity must hide nothing: %+v", r)
	}
}

func TestRateLimitIsRecognised(t *testing.T) {
	for _, stderr := range []string{
		"squeue: error: slurm_load_jobs error: Rate limit exceeded, please retry later",
		"sbatch: error: Rate limit exceeded, please retry momentarily",
		"first line\nsqueue: error: RATE LIMIT EXCEEDED, please retry later\n",
	} {
		err := Classify(&execx.ExitError{Code: 1}, []byte(stderr))
		if KindOf(err) != ErrRateLimited {
			t.Errorf("%q: kind %v", stderr, KindOf(err))
		}
	}
	for _, stderr := range []string{"squeue: error: Unable to contact slurm controller", "squeue: error: no limit set", ""} {
		if KindOf(Classify(&execx.ExitError{Code: 1}, []byte(stderr))) == ErrRateLimited {
			t.Errorf("%q must not count as a rate limit", stderr)
		}
	}

	st := &Store{}
	limited := Classify(&execx.ExitError{Code: 1}, []byte("Rate limit exceeded, please retry later"))
	st.Apply(Update{Source: "nodes", Err: limited, At: t0})
	if !st.RateLimited() {
		t.Fatal("a rate-limited source must show")
	}
	st.Apply(Update{Source: "nodes", Data: []model.Node{}, At: t0.Add(time.Minute)})
	if st.RateLimited() {
		t.Fatal("a recovered source must clear it")
	}
	st.Apply(Update{Source: "nodes", Err: errors.New("other"), At: t0})
	if st.RateLimited() {
		t.Fatal("other errors are not rate limits")
	}
}
