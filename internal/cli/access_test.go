package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/report"
	"github.com/nbharathik/slurm-dashboard/internal/state"
)

// In the hetero demo, "login" denies the user's account and "mig" lists it.
func TestPartitionsShowAccess(t *testing.T) {
	out, errOut, code := demoRun(t, "partitions")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	wantAll(t, "partitions", out, "login (no access)")
	if strings.Contains(out, "mig (no access)") || strings.Contains(out, "cpu* (no access)") {
		t.Errorf("open partitions marked closed:\n%s", out)
	}

	out, _, _ = demoRun(t, "nodes", "--partitions", "--json")
	var doc report.PartitionsDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	access := map[string]string{}
	for _, p := range doc.Partitions {
		access[p.Name] = p.Access
		if p.Name == "login" && p.AccessWhy != "DenyAccounts" {
			t.Errorf("login access_why = %q", p.AccessWhy)
		}
	}
	for name, want := range map[string]string{"login": "denied", "mig": "allowed", "cpu": "allowed"} {
		if access[name] != want {
			t.Errorf("%s access = %q, want %q", name, access[name], want)
		}
	}
}

func TestFreeLeavesOutClosedPartitions(t *testing.T) {
	out, errOut, code := demoRun(t, "free", "--cpus", "1", "--json")
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var doc report.FreeDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(doc.Denied, []string{"login"}) || len(doc.Unverified) != 0 {
		t.Fatalf("denied %q, unverified %q", doc.Denied, doc.Unverified)
	}
	for _, f := range doc.Now {
		if f.Node == "login01" || slices.Contains(f.Partitions, "login") {
			t.Errorf("a closed partition is offered: %+v", f)
		}
	}
	if len(doc.Now) == 0 {
		t.Fatal("the open partitions must still be offered")
	}

	out, _, _ = demoRun(t, "free", "--cpus", "1")
	wantAll(t, "free", out, "Left out, you may not submit to: login")

	out, _, _ = demoRun(t, "free", "--cpus", "1", "-p", "login", "--json")
	doc = report.FreeDoc{}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Now) != 0 || !slices.Contains(doc.Reasons, "you may not submit to partition login (DenyAccounts)") {
		t.Fatalf("login asked for: %+v", doc)
	}
}

func TestHardFailNamesTheRateLimit(t *testing.T) {
	rt := &slurmRuntime{}
	rt.fail("nodes", state.Classify(&execx.ExitError{Code: 1}, []byte("scontrol: error: slurm_load_node error: Rate limit exceeded, please retry later")))
	err := rt.hardFail()
	if err == nil || !strings.Contains(err.Error(), "controller is rate-limiting") || !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Fatalf("hardFail = %v", err)
	}
}
