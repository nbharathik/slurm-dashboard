package parse

import (
	"slices"
	"testing"
)

func TestPartitionsAccessLists(t *testing.T) {
	raw := "PartitionName=gpu AllowGroups=physics,chem AllowAccounts=ALL AllowQos=ALL Default=YES MaxTime=1-00:00:00 Nodes=g[01-02] State=UP TotalCPUs=64 TotalNodes=2 TRES=cpu=64\n" +
		"PartitionName=cpu AllowGroups=ALL DenyAccounts=guests,temp AllowQos=normal,high Default=NO MaxTime=UNLIMITED Nodes=c01 State=UP TotalCPUs=32 TotalNodes=1 TRES=cpu=32\n" +
		"PartitionName=open AllowGroups=ALL AllowAccounts=ALL AllowQos=ALL DenyQos=(null) Default=NO MaxTime=UNLIMITED Nodes=c01 State=UP TotalCPUs=32 TotalNodes=1 TRES=cpu=32\n" +
		"PartitionName=qos AllowGroups=all AllowAccounts=lab-a DenyQos=low Default=NO MaxTime=UNLIMITED Nodes=c01 State=UP TotalCPUs=32 TotalNodes=1 TRES=cpu=32\n"
	parts, warns := Partitions([]byte(raw))
	if len(warns) != 0 || len(parts) != 4 {
		t.Fatalf("parts = %d, warnings = %v", len(parts), warns)
	}
	same := func(what string, got, want []string) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Errorf("%s = %q, want %q", what, got, want)
		}
	}
	same("gpu AllowGroups", parts[0].AllowGroups, []string{"physics", "chem"})
	same("gpu AllowAccounts", parts[0].AllowAccounts, nil)
	same("cpu AllowGroups", parts[1].AllowGroups, nil)
	same("cpu DenyAccounts", parts[1].DenyAccounts, []string{"guests", "temp"})
	same("cpu AllowQOS", parts[1].AllowQOS, []string{"normal", "high"})
	if parts[2].Restricted() {
		t.Errorf("open partition is restricted: %+v", parts[2])
	}
	same("qos AllowGroups", parts[3].AllowGroups, nil)
	same("qos AllowAccounts", parts[3].AllowAccounts, []string{"lab-a"})
	same("qos DenyQOS", parts[3].DenyQOS, []string{"low"})
	for i, want := range []bool{true, true, false, true} {
		if got := parts[i].Restricted(); got != want {
			t.Errorf("%s Restricted = %v, want %v", parts[i].Name, got, want)
		}
	}
}

func TestGroups(t *testing.T) {
	got, warns := Groups([]byte("alice  physics\tchem\n"))
	if len(warns) != 0 || !slices.Equal(got, []string{"alice", "physics", "chem"}) {
		t.Fatalf("groups = %q, warnings = %v", got, warns)
	}
	if got, _ := Groups(nil); len(got) != 0 {
		t.Fatalf("empty output gave %q", got)
	}
}
