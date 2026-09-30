package slurm

import (
	"os"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/slurm/parse"
)

func info(t *testing.T, file string) parse.ClusterInfo {
	t.Helper()
	b, err := os.ReadFile("../../testdata/fixtures/" + file)
	if err != nil {
		t.Fatal(err)
	}
	i, err := parse.ClusterConfig(b)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func statuses(fs []Feature) map[string]FeatureStatus {
	out := map[string]FeatureStatus{}
	for _, f := range fs {
		out[f.Name] = f.Status
	}
	return out
}

func TestFeaturesFullCluster(t *testing.T) {
	caps := model.Capabilities{Major: 23, Minor: 11, HasSacct: true, HasSshare: true, HasSprio: true, HasSstat: true}
	for _, f := range Features(caps, info(t, "23.11/clusterconfig.txt")) {
		if f.Status != Available || f.Note != "" {
			t.Errorf("%s: %v %q, want available", f.Name, f.Status, f.Note)
		}
	}
}

// A small cluster without accounting: every feature that needs it says why.
func TestFeaturesBasicCluster(t *testing.T) {
	caps := model.Capabilities{Major: 22, Minor: 5, HasSstat: true}
	fs := Features(caps, info(t, "handwritten/clusterconfig-basic.txt"))
	got := statuses(fs)
	want := map[string]FeatureStatus{
		"history and efficiency":  Unavailable,
		"rerun a finished job":    Unavailable,
		"fairshare and priority":  Unavailable,
		"live job usage":          Unavailable,
		"everyone's jobs (Queue)": Available,
	}
	for name, st := range want {
		if got[name] != st {
			t.Errorf("%s = %v, want %v", name, got[name], st)
		}
	}
	for _, f := range fs {
		if f.Status != Available && (f.Note == "" || strings.Contains(f.Note, "\n")) {
			t.Errorf("%s needs a one-line reason, has %q", f.Name, f.Note)
		}
	}
	for _, f := range fs {
		switch f.Name {
		case "history and efficiency":
			if !strings.Contains(f.Note, "accounting_storage/none") {
				t.Errorf("history note = %q", f.Note)
			}
		case "fairshare and priority":
			if !strings.Contains(f.Note, "priority/basic") {
				t.Errorf("priority note = %q", f.Note)
			}
		case "live job usage":
			if !strings.Contains(f.Note, "jobacct_gather/none") {
				t.Errorf("usage note = %q", f.Note)
			}
		}
	}
}

func TestFeaturesLimited(t *testing.T) {
	full := info(t, "23.11/clusterconfig.txt")
	caps := model.Capabilities{Major: 23, Minor: 11, HasSacct: true, HasSshare: true, HasSprio: true, HasSstat: true}

	noScripts := full
	noScripts.Settings = map[string]string{"AccountingStorageType": "accounting_storage/slurmdbd", "AccountingStoreFlags": "(null)"}
	if got := statuses(Features(caps, noScripts))["rerun a finished job"]; got != Limited {
		t.Errorf("no job_script: %v, want limited", got)
	}
	old := caps
	old.Major, old.Minor = 22, 5
	if got := statuses(Features(old, full))["rerun a finished job"]; got != Limited {
		t.Errorf("before 23.02: %v, want limited", got)
	}
	private := full
	private.PrivateData = []string{"jobs"}
	if got := statuses(Features(caps, private))["everyone's jobs (Queue)"]; got != Limited {
		t.Errorf("PrivateData=jobs: %v, want limited", got)
	}
	half := caps
	half.HasSprio = false
	if got := statuses(Features(half, full))["fairshare and priority"]; got != Limited {
		t.Errorf("sshare only: %v, want limited", got)
	}
}

// A config that could not be read hides nothing: features fall back to what
// the probes found.
func TestFeaturesUnknownSiteHidesNothing(t *testing.T) {
	caps := model.Capabilities{Major: 24, Minor: 11, HasSacct: true, HasSshare: true, HasSprio: true, HasSstat: true}
	for _, f := range Features(caps, parse.ClusterInfo{}) {
		if f.Status != Available {
			t.Errorf("%s: %v %q with an unreadable config", f.Name, f.Status, f.Note)
		}
	}
	var none parse.ClusterInfo
	if none.AccountingOff() || none.NoJobScripts() || none.BasicPriority() || none.NoUsageGather() {
		t.Error("an empty ClusterInfo must not claim anything is missing")
	}
}
