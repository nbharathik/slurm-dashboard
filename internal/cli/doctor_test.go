package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestClusterCheck(t *testing.T) {
	cases := []struct {
		name, override, status, detail string
		err                            error
	}{
		{"tundra", "", checkOK, "tundra", nil},
		{"cluster", "", checkWarn, `ClusterName is generic ("cluster")`, nil},
		{"Cluster", "", checkWarn, "generic", nil},
		{"cluster", "hpc-a", checkOK, "hpc-a (from the config; Slurm calls it cluster)", nil},
		{"", "hpc-a", checkOK, "hpc-a (from the config)", errors.New("no config")},
		{"", "", checkWarn, "boom", errors.New("boom")},
	}
	for _, tc := range cases {
		_, status, detail, hint := clusterCheck(tc.name, tc.err, tc.override)
		if status != tc.status || !strings.Contains(detail, tc.detail) {
			t.Errorf("clusterCheck(%q, %v, %q) = %s %q", tc.name, tc.err, tc.override, status, detail)
		}
		if status == checkWarn && !strings.Contains(hint, "cluster_name") {
			t.Errorf("warning without the cluster_name hint: %q", hint)
		}
	}
}

func TestPathCheck(t *testing.T) {
	if _, st, _, _ := pathCheck("/home/u/.local/bin/sdash", nil, "/home/u/.local/bin/sdash"); st != checkOK {
		t.Error("same binary should be ok")
	}
	if _, st, d, _ := pathCheck("/usr/bin/sdash", nil, "/home/u/.local/bin/sdash"); st != checkWarn || !strings.Contains(d, "not this binary") {
		t.Errorf("other binary: %s %s", st, d)
	}
	if _, st, _, h := pathCheck("", errors.New("not found"), "/tmp/sdash"); st != checkWarn || !strings.Contains(h, "make install") {
		t.Errorf("missing: %s %s", st, h)
	}
}

// doctor lists what the cluster offers, with the reason for each gap, and
// does not call a configured-off feature a problem.
func TestDoctorFeatures(t *testing.T) {
	h := newHarness(t)
	h.run("doctor", "--demo=basic")
	out := h.stdout.String()
	for _, want := range []string{
		"history and efficiency", "not available: Slurm accounting is off (AccountingStorageType=accounting_storage/none)",
		"rerun a finished job", "fairshare and priority", "PriorityType=priority/basic",
		"live job usage", "jobacct_gather/none", "limited: PrivateData=jobs", "off, as configured",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("basic doctor lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "!  accounting") {
		t.Errorf("accounting configured off must not warn:\n%s", out)
	}

	h = newHarness(t)
	h.run("doctor", "--demo")
	out = h.stdout.String()
	if strings.Contains(out, "not available") || strings.Contains(out, "limited:") || !strings.Contains(out, "rerun a finished job") {
		t.Errorf("default doctor should find everything available:\n%s", out)
	}
}
