package parse

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

func TestKV(t *testing.T) {
	cases := []struct {
		line string
		want map[string]string
	}{
		{
			"NodeName=gpu01 State=MIXED+DRAIN Reason=bad DIMM : Not responding [root@2026-09-28T12:52:55]",
			map[string]string{"NodeName": "gpu01", "State": "MIXED+DRAIN", "Reason": "bad DIMM : Not responding [root@2026-09-28T12:52:55]"},
		},
		{
			"AllocTRES=cpu=8,mem=64G,gres/gpu=2 CfgTRES=cpu=16",
			map[string]string{"AllocTRES": "cpu=8,mem=64G,gres/gpu=2", "CfgTRES": "cpu=16"},
		},
		{
			"AllocNode:Sid=localhost:8117 CPUs/Task=4 ReqB:S:C:T=0:0:*:* Socks/Node=*",
			map[string]string{"AllocNode:Sid": "localhost:8117", "CPUs/Task": "4", "ReqB:S:C:T": "0:0:*:*", "Socks/Node": "*"},
		},
		{
			"Command=/run.sh --lr=0.1 epochs=5 WorkDir=/home/a b/c",
			map[string]string{"Command": "/run.sh --lr=0.1 epochs=5", "WorkDir": "/home/a b/c"},
		},
		{"AllocTRES= Reason=", map[string]string{"AllocTRES": "", "Reason": ""}},
		{"", map[string]string{}},
		{"no keys here", map[string]string{}},
	}
	for _, tc := range cases {
		got, order := KV(tc.line)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("KV(%q) = %v, want %v", tc.line, got, tc.want)
		}
		if len(order) != len(tc.want) {
			t.Errorf("KV(%q) order = %v", tc.line, order)
		}
	}
	_, order := KV("B=1 A=2 B=3")
	if !reflect.DeepEqual(order, []string{"B", "A"}) {
		t.Fatalf("duplicate keys keep first position: %v", order)
	}
}

func TestTestOnlyAndParsable(t *testing.T) {
	time.Local = time.UTC
	est, err := TestOnly([]byte("sbatch: Job 23 to start at 2026-09-28T13:38:41 using 1 processors on nodes node01 in partition gpu\n"))
	if err != nil || est.JobID != "23" || est.Procs != 1 || est.Nodes != "node01" || est.Partition != "gpu" ||
		!est.Start.Equal(time.Date(2026, 9, 28, 13, 38, 41, 0, time.UTC)) {
		t.Fatalf("TestOnly = %+v, %v", est, err)
	}
	if _, err := TestOnly([]byte("sbatch: error: Batch job submission failed: Invalid account\n")); err == nil ||
		!strings.Contains(err.Error(), "Invalid account") {
		t.Fatalf("TestOnly error = %v", err)
	}
	if _, err := TestOnly(nil); err == nil || !strings.Contains(err.Error(), "no output") {
		t.Fatalf("TestOnly(nil) = %v", err)
	}
	if _, err := TestOnly([]byte("Job 1 to start at soon using 1 processors on nodes x in partition y")); err == nil {
		t.Fatal("bad timestamp accepted")
	}

	for in, want := range map[string][2]string{"123\n": {"123", ""}, "456;other\n": {"456", "other"}} {
		id, cl, err := Parsable([]byte(in))
		if err != nil || id != want[0] || cl != want[1] {
			t.Errorf("Parsable(%q) = %q %q %v", in, id, cl, err)
		}
	}
	if _, _, err := Parsable([]byte("sbatch: error: x")); err == nil {
		t.Fatal("Parsable accepted an error message")
	}
}

func TestVersionAndClusterName(t *testing.T) {
	for in, want := range map[string]string{"slurm 23.11.4": "23.11.4", "slurm-wlm 23.11.4\n": "23.11.4", "slurm 24.05.0-0rc1": "24.05.0"} {
		c, err := Version([]byte(in))
		if err != nil || c.Version != want || !c.HasMe || !c.HasOverlap {
			t.Errorf("Version(%q) = %+v %v", in, c, err)
		}
	}
	old, _ := Version([]byte("slurm 20.02.7"))
	if old.HasMe || old.HasOverlap || old.Major != 20 || old.Minor != 2 {
		t.Fatalf("old version = %+v", old)
	}
	if !old.AtLeast(19, 5) || old.AtLeast(20, 11) {
		t.Fatal("AtLeast")
	}
	if _, err := Version([]byte("garbage")); err == nil {
		t.Fatal("garbage version")
	}
	if _, err := ClusterName([]byte("Foo = bar\nClusterName = \n")); err == nil {
		t.Fatal("empty cluster name")
	}
}

func TestMalformedRecords(t *testing.T) {
	parts, warns := Partitions([]byte("PartitionName=x MaxTime=nonsense Nodes=a[1-2]\nState=UP\nNo partitions in the system\n"))
	if len(parts) != 0 || len(warns) != 2 {
		t.Fatalf("Partitions = %v, %v", parts, warns)
	}
	res, warns := Reservations([]byte("ReservationName=r StartTime=bad\nFlags=MAINT\n"))
	if len(res) != 0 || len(warns) != 2 {
		t.Fatalf("Reservations = %v, %v", res, warns)
	}
	jobs, warns := JobDetails([]byte("JobId=abc JobState=RUNNING\nJobState=RUNNING\nNo jobs in the system\n"))
	if len(jobs) != 0 || len(warns) != 2 {
		t.Fatalf("JobDetails = %v, %v", jobs, warns)
	}
	q, warns := QueueRank([]byte("1 gpu x\n1 gpu\n2 gpu 10\n"))
	if len(q) != 1 || len(warns) != 2 {
		t.Fatalf("QueueRank = %v, %v", q, warns)
	}
	nodes, warns := Nodes([]byte("NodeName=n CPULoad=x\nNodeName=m FreeMem=lots\n"))
	if len(nodes) != 0 || len(warns) != 2 {
		t.Fatalf("Nodes = %v, %v", nodes, warns)
	}
	shares, warns := Shares([]byte("a|u|1|x|1|1|1\n"))
	if len(shares) != 0 || len(warns) != 1 {
		t.Fatalf("Shares = %v, %v", shares, warns)
	}
	run, warns := Running([]byte("1 u p 1 n[1-2 2026-01-01T00:00:00 cpu=1\nx u p 1 n 2026-01-01T00:00:00 cpu=1\n"))
	if len(run) != 0 || len(warns) != 2 {
		t.Fatalf("Running = %v, %v", run, warns)
	}
	stat, warns := Sstat([]byte("1.batch|1|bad|0|00:00:00|\n"))
	if stat != nil || len(warns) != 1 {
		t.Fatalf("Sstat = %v, %v", stat, warns)
	}
	fs, warns := FinalStates([]byte("1|FAILED|x|1\n1|FAILED|1:0|-3\n"))
	if len(fs) != 0 || len(warns) != 2 {
		t.Fatalf("FinalStates = %v, %v", fs, warns)
	}
	pr, warns := Priorities([]byte(strings.Join([]string{"1", "x", "0", "0", "0", "0", "0"}, Sep) + "\n"))
	if len(pr) != 0 || len(warns) != 1 {
		t.Fatalf("Priorities = %v, %v", pr, warns)
	}
	h, warns := History([]byte(strings.Repeat(Sep, HistoryFields-1) + "\n"))
	if len(h) != 0 || len(warns) != 1 {
		t.Fatalf("History empty ID = %v, %v", h, warns)
	}
}

func TestHistoryStepErrors(t *testing.T) {
	f := func(fields ...string) string { return strings.Join(fields, Sep) }
	raw := f("5", "5", "j", "p", "COMPLETED", "0:0", "", "", "", "10", "1", "1", "00:01", "1G", "", "", "mem=1G", "1", "n1", "/w") + "\n" +
		f("5.batch", "5.batch", "batch", "", "COMPLETED", "0:0", "", "", "", "10", "", "1", "zz", "", "bad", "", "", "1", "n1", "") + "\n"
	jobs, warns := History([]byte(raw))
	if len(jobs) != 1 || len(warns) != 1 {
		t.Fatalf("History = %v, %v", jobs, warns)
	}
}

func TestLongLine(t *testing.T) {
	raw := []byte("NodeName=a State=IDLE\n" + strings.Repeat("x", 2<<20) + "\nNodeName=b State=IDLE\n")
	nodes, warns := Nodes(raw)
	if len(nodes) != 1 || len(warns) != 1 || !strings.Contains(warns[0].Msg, "stopped reading") {
		t.Fatalf("long line: %v %v", nodes, warns)
	}
}

func TestRecoverTurnsPanicIntoWarning(t *testing.T) {
	var got []model.ParseWarning
	func() {
		w := &warnings{source: "test"}
		defer func() { got = w.list }()
		defer w.recover()
		var list []int
		_ = list[len(list)+1] // index out of range panics
	}()
	if len(got) != 1 || !strings.Contains(got[0].Msg, "parser bug") {
		t.Fatalf("warnings = %v", got)
	}
	w := &warnings{source: "x"}
	w.add(1, strings.Repeat("y", 400), "m")
	if len(w.list[0].Raw) > 310 {
		t.Fatal("raw text must be truncated")
	}
}

func TestFieldHelpers(t *testing.T) {
	var fe fieldErr
	if fe.atof("1.5") != 1.5 || fe.atof("") != 0 || fe.atof("N/A") != 0 || fe.err != nil {
		t.Fatal("atof")
	}
	fe.atof("x")
	if fe.err == nil {
		t.Fatal("atof error")
	}
	if splitList("(null)") != nil || len(splitList("a, b,,c")) != 3 {
		t.Fatal("splitList")
	}
}

func FuzzAllParsers(f *testing.F) {
	for _, p := range []string{"23.11/submitline-script.txt", "23.11/assocmgr.txt", "handwritten/submitline-quoted.txt", "23.11/myjobs.txt", "23.11/nodes.txt", "23.11/history.txt", "handwritten/myjobs.txt", "handwritten/history.txt", "handwritten/jobdetail-array.txt", "handwritten/myjobs-escapes.txt", "handwritten/nodes-escapes.txt"} {
		if b, err := os.ReadFile("../../../testdata/fixtures/" + p); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		for name, p := range parsers {
			items, warns := p(raw)
			if len(warns) > 0 && strings.Contains(warns[len(warns)-1].Msg, "parser bug") {
				t.Fatalf("%s panicked: %s", name, warns[len(warns)-1].Msg)
			}
			if s, ok := dirtyString(reflect.ValueOf(items)); ok {
				t.Fatalf("%s returned an uncleaned string %q", name, s)
			}
		}
		_, _ = KV(string(raw))
		_, _ = TestOnly(raw)
		_, _, _ = Parsable(raw)
		for _, o := range ScriptDirectives(string(raw)) {
			_ = o.String()
		}
		_, _ = SubmitLine(string(raw), "x")
		_, _ = AssocMgr(raw, "root")
	})
}

// dirtyString finds a string in v that textsafe would change.
func dirtyString(v reflect.Value) (string, bool) {
	switch v.Kind() {
	case reflect.String:
		if s := v.String(); textsafe.Field(s) != s {
			return s, true
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return dirtyString(v.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if s, ok := dirtyString(v.Index(i)); ok {
				return s, true
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if s, ok := dirtyString(k); ok {
				return s, true
			}
			if s, ok := dirtyString(v.MapIndex(k)); ok {
				return s, true
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				if s, ok := dirtyString(v.Field(i)); ok {
					return s, true
				}
			}
		}
	}
	return "", false
}
