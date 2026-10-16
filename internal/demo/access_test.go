package demo

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// The hetero scenario restricts "login" to the group admins and "mig" to two
// accounts, one of them the user's.
func TestHeteroHasRestrictedPartitions(t *testing.T) {
	_, clock, src := setupScenario(t, "hetero")
	ctx := context.Background()
	parts, err := src.Partitions(ctx)
	must(t, err)
	by := map[string]model.Partition{}
	for _, p := range parts {
		by[p.Name] = p
	}
	if !slices.Equal(by["login"].AllowGroups, []string{"admins"}) || !slices.Equal(by["mig"].AllowAccounts, []string{"lab-you", "lab-nlp"}) || by["cpu"].Restricted() {
		t.Fatalf("access lists: login %+v, mig %+v, cpu %+v", by["login"], by["mig"], by["cpu"])
	}
	groups, err := src.Groups(ctx)
	must(t, err)
	if !slices.Equal(groups, []string{"you"}) {
		t.Fatalf("groups = %q", groups)
	}
	shares, err := src.Fairshare(ctx)
	must(t, err)

	st := &state.Store{User: "you"}
	st.Apply(state.Update{Source: "partitions", Data: parts, At: clock.Now()})
	st.Apply(state.Update{Source: "groups", Data: groups, At: clock.Now()})
	st.Apply(state.Update{Source: "fairshare", Data: shares, At: clock.Now()})
	id := st.Identity()
	for name, want := range map[string]state.Access{"login": state.AccessDenied, "mig": state.AccessAllowed, "cpu": state.AccessAllowed} {
		if got, _ := state.PartitionAccess(by[name], id); got != want {
			t.Errorf("%s access = %v, want %v", name, got, want)
		}
	}
}

func TestScenarioGroupsAndAccessFields(t *testing.T) {
	sc, err := Load("default")
	must(t, err)
	sc.Groups = []string{sc.User, "ml"}
	clock := state.NewFakeClock(t0)
	res, err := New(sc, clock).Runner().Run(context.Background(), "id", "-Gn")
	if err != nil || string(res.Stdout) != sc.User+" ml\n" {
		t.Fatalf("id -Gn = %q, %v", res.Stdout, err)
	}

	open := accessFields(Partition{})
	if open != "AllowGroups=ALL AllowAccounts=ALL AllowQos=ALL" {
		t.Fatalf("open partition: %q", open)
	}
	closed := Partition{AllowGroups: []string{"a", "b"}, DenyAccounts: []string{"x"}, DenyQOS: []string{"low"}}
	if got := accessFields(closed); got != "AllowGroups=a,b DenyAccounts=x DenyQos=low" {
		t.Fatalf("closed partition: %q", got)
	}
	parts, warns := parse.Partitions([]byte("PartitionName=p " + accessFields(closed) + " Nodes=n1 State=UP\n"))
	if len(warns) != 0 || len(parts) != 1 || !slices.Equal(parts[0].AllowGroups, []string{"a", "b"}) ||
		!slices.Equal(parts[0].DenyAccounts, []string{"x"}) || !slices.Equal(parts[0].DenyQOS, []string{"low"}) || strings.Contains(accessFields(closed), "AllowAccounts") {
		t.Fatalf("round trip: %+v %v", parts, warns)
	}
}
