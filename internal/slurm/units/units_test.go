package units

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

func TestParseDuration(t *testing.T) {
	h, m, s := time.Hour, time.Minute, time.Second
	cases := []struct {
		in   string
		want time.Duration
		kind DurKind
	}{
		{"0:00", 0, DurValid},
		{"05:46", 5*m + 46*s, DurValid},
		{"1:00:00", h, DurValid},
		{"24:00:00", 24 * h, DurValid},
		{"2-12:00:00", 60 * h, DurValid},
		{"1-00", 24 * h, DurValid},
		{"1-02", 26 * h, DurValid},
		{"1-02:30", 26*h + 30*m, DurValid},
		{"7-00:00:00", 7 * 24 * h, DurValid},
		{"00:19.749", 19*s + 749*time.Millisecond, DurValid},
		{"00:00.008", 8 * time.Millisecond, DurValid},
		{"1:02:03.5", h + 2*m + 3*s + 500*time.Millisecond, DurValid},
		{"10", 10 * m, DurValid},
		{"UNLIMITED", 0, DurUnlimited},
		{"Partition_Limit", 0, DurUnlimited},
		{"INFINITE", 0, DurUnlimited},
		{"INVALID", 0, DurUnknown},
		{"NOT_SET", 0, DurUnknown},
		{"N/A", 0, DurUnknown},
		{"NONE", 0, DurUnknown},
		{"", 0, DurUnknown},
	}
	for _, tc := range cases {
		got, kind, err := ParseDuration(tc.in)
		if err != nil || got != tc.want || kind != tc.kind {
			t.Errorf("ParseDuration(%q) = %v, %v, %v; want %v, %v", tc.in, got, kind, err, tc.want, tc.kind)
		}
	}
	for _, bad := range []string{"abc", "1:2:3:4", "-5", "1:-2", "1.", "x-01:00", "1:00.abc", "1:00.1234567890"} {
		if _, _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should fail", bad)
		}
	}
}

func TestParseLimitAndRaw(t *testing.T) {
	if l, err := ParseLimit("UNLIMITED"); l != nil || err != nil {
		t.Fatal("UNLIMITED must be nil")
	}
	if l, err := ParseLimit("04:00:00"); err != nil || *l != 4*time.Hour {
		t.Fatalf("ParseLimit = %v %v", l, err)
	}
	if d, err := ParseSeconds("44"); err != nil || d != 44*time.Second {
		t.Fatal("ParseSeconds")
	}
	if _, err := ParseSeconds("-1"); err == nil {
		t.Fatal("negative seconds")
	}
	if l, err := ParseMinutesLimit("240"); err != nil || *l != 4*time.Hour {
		t.Fatal("ParseMinutesLimit")
	}
	for _, s := range []string{"", "UNLIMITED", "Partition_Limit"} {
		if l, err := ParseMinutesLimit(s); l != nil || err != nil {
			t.Fatalf("ParseMinutesLimit(%q) = %v %v", s, l, err)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                                   "00:00",
		5*time.Minute + 46*time.Second:      "05:46",
		4 * time.Hour:                       "04:00:00",
		60*time.Hour + 5*time.Second:        "2-12:00:05",
		-time.Second:                        "00:00",
		26*time.Hour + 30*time.Minute + 999: "1-02:30:00",
	}
	for d, want := range cases {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
	short := map[time.Duration]string{
		45 * time.Second:                "45s",
		12 * time.Minute:                "12m",
		3*time.Hour + 5*time.Minute:     "3h05m",
		51*time.Hour + 20*time.Minute:   "2d03h",
		-5 * time.Second:                "0s",
		time.Hour + 59*time.Second:      "1h",
		48*time.Hour + 30*time.Minute:   "2d",
		134*time.Minute + 3*time.Second: "2h14m",
	}
	for d, want := range short {
		if got := FormatShort(d); got != want {
			t.Errorf("FormatShort(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestParseTimestamp(t *testing.T) {
	got, err := ParseTimestamp("2026-09-28T12:52:41", time.UTC)
	if err != nil || !got.Equal(time.Date(2026, 9, 28, 12, 52, 41, 0, time.UTC)) {
		t.Fatalf("ParseTimestamp = %v %v", got, err)
	}
	for _, zero := range []string{"N/A", "Unknown", "None", "(null)", "", "  "} {
		if got, err := ParseTimestamp(zero, time.UTC); err != nil || !got.IsZero() {
			t.Errorf("ParseTimestamp(%q) = %v %v", zero, got, err)
		}
	}
	if _, err := ParseTimestamp("28/09/2026", time.UTC); err == nil {
		t.Fatal("bad timestamp accepted")
	}
	if got, err := ParseTimestamp("2026-01-01T00:00:00", nil); err != nil || got.Location() != time.Local {
		t.Fatal("nil location should mean Local")
	}
}

func TestParseMem(t *testing.T) {
	cases := []struct {
		in     string
		mb     float64
		perCPU bool
	}{
		{"4000M", 4000, false},
		{"4G", 4096, false},
		{"62.50G", 64000, false},
		{"1.50T", 1.5 * 1024 * 1024, false},
		{"512K", 0.5, false},
		{"3340K", 3340.0 / 1024, false},
		{"2048", 2048, false},
		{"0", 0, false},
		{"4000Mn", 4000, false},
		{"2000Mc", 2000, true},
		{"4Gc", 4096, true},
		{"", 0, false},
		{"N/A", 0, false},
	}
	for _, tc := range cases {
		mb, perCPU, err := ParseMemMB(tc.in)
		if err != nil || mb != tc.mb || perCPU != tc.perCPU {
			t.Errorf("ParseMemMB(%q) = %v, %v, %v; want %v, %v", tc.in, mb, perCPU, err, tc.mb, tc.perCPU)
		}
	}
	for _, bad := range []string{"G", "abcM", "-4G", "c", "NaNM"} {
		if _, _, err := ParseMemMB(bad); err == nil {
			t.Errorf("ParseMemMB(%q) should fail", bad)
		}
	}
	if n, err := MemMB("2000Mc", 4); err != nil || n != 8000 {
		t.Fatalf("MemMB per CPU = %d %v", n, err)
	}
	if n, err := MemMB("8G", 4); err != nil || n != 8192 {
		t.Fatalf("MemMB = %d %v", n, err)
	}
	if _, err := MemMB("x", 1); err == nil {
		t.Fatal("MemMB bad")
	}
	for in, want := range map[float64]string{900: "900M", 23244.8: "22.7G", 1024: "1G", 1.2 * 1024 * 1024: "1.2T", 150 * 1024: "150G"} {
		if got := FormatMB(in); got != want {
			t.Errorf("FormatMB(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int64]string{0: "0B", 1023: "1023B", 1024: "1K", 41 << 30: "41G", 1288490188800: "1.2T", 1 << 62: "4E"} {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParseJobID(t *testing.T) {
	cases := []struct {
		in   string
		want model.JobID
	}{
		{"123", model.JobID{Raw: "123", ArrayJobID: 123, HetOffset: -1}},
		{"123_4", model.JobID{Raw: "123_4", ArrayJobID: 123, TaskSpec: "4", HetOffset: -1}},
		{"123_[5-99]", model.JobID{Raw: "123_[5-99]", ArrayJobID: 123, TaskSpec: "[5-99]", HetOffset: -1}},
		{"123_[5-99%10]", model.JobID{Raw: "123_[5-99%10]", ArrayJobID: 123, TaskSpec: "[5-99%10]", HetOffset: -1}},
		{"123_[1,3,5-7]", model.JobID{Raw: "123_[1,3,5-7]", ArrayJobID: 123, TaskSpec: "[1,3,5-7]", HetOffset: -1}},
		{"13_[1-8%1]", model.JobID{Raw: "13_[1-8%1]", ArrayJobID: 13, TaskSpec: "[1-8%1]", HetOffset: -1}},
		{"123+0", model.JobID{Raw: "123+0", ArrayJobID: 123, HetOffset: 0}},
		{"123+1", model.JobID{Raw: "123+1", ArrayJobID: 123, HetOffset: 1}},
		{"123.batch", model.JobID{Raw: "123.batch", ArrayJobID: 123, HetOffset: -1, Step: "batch"}},
		{"123.extern", model.JobID{Raw: "123.extern", ArrayJobID: 123, HetOffset: -1, Step: "extern"}},
		{"123.0", model.JobID{Raw: "123.0", ArrayJobID: 123, HetOffset: -1, Step: "0"}},
		{"123_4.batch", model.JobID{Raw: "123_4.batch", ArrayJobID: 123, TaskSpec: "4", HetOffset: -1, Step: "batch"}},
		{"123+0.batch", model.JobID{Raw: "123+0.batch", ArrayJobID: 123, HetOffset: 0, Step: "batch"}},
	}
	for _, tc := range cases {
		got, err := ParseJobID(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseJobID(%q) = %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "abc", "123_", "123_x", "12a", "123+x", "123.", "123.a b", "_4", "123_[1-2"} {
		if _, err := ParseJobID(bad); err == nil {
			t.Errorf("ParseJobID(%q) should fail", bad)
		}
	}
	if got, _ := ParseJobID("123_4"); !got.IsArray() || got.String() != "123_4" {
		t.Fatal("IsArray/String")
	}
}

func TestValidJobRef(t *testing.T) {
	for _, ok := range []string{"812", "812_4", "812_[1-5]", "812_[1,3%2]", "812+0", "812_4+1"} {
		if !ValidJobRef.MatchString(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"812; rm -rf ~", "--user=bob", "812 813", "", "abc", "812_", "-812", "812\n813"} {
		if ValidJobRef.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestExpandHostlist(t *testing.T) {
	cases := map[string][]string{
		"gpu[01-04,07]":        {"gpu01", "gpu02", "gpu03", "gpu04", "gpu07"},
		"a01,b[1-3]":           {"a01", "b1", "b2", "b3"},
		"node01":               {"node01"},
		"gpu[02-03],node01":    {"gpu02", "gpu03", "node01"},
		"rack[1-2]-n[01-02]":   {"rack1-n01", "rack1-n02", "rack2-n01", "rack2-n02"},
		"c[8-10]":              {"c8", "c9", "c10"},
		"n[008-010]":           {"n008", "n009", "n010"},
		"None assigned":        nil,
		"(null)":               nil,
		"":                     nil,
		"x[1-2]y,z":            {"x1y", "x2y", "z"},
		"login1,login2,login3": {"login1", "login2", "login3"},
	}
	for in, want := range cases {
		got, err := ExpandHostlist(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ExpandHostlist(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"gpu[01-04", "gpu01]", "gpu[a-b]", "gpu[5-1]", "n[1-200000]", "n[1-400]m[1-400]"} {
		if _, err := ExpandHostlist(bad); err == nil {
			t.Errorf("ExpandHostlist(%q) should fail", bad)
		}
	}
}

func TestGRESAndTRES(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		typ  string
		desc string
	}{
		{"gpu:h200:4(S:0-1)", 4, "h200", "node gres with sockets"},
		{"gpu:4", 4, "", "count"},
		{"gpu", 1, "", "bare"},
		{"gpu:a100", 1, "a100", "type only"},
		{"gres:gpu:2", 2, "", "gres: prefix"},
		{"gres/gpu=2", 2, "", "tres form"},
		{"gres/gpu:h200=2", 2, "h200", "typed tres form"},
		{"gres/gpu:2", 2, "", "23.11 squeue %b"},
		{"gres/gpu:tesla:2", 2, "tesla", "23.11 typed"},
		{"gpu:a100:4,gpu:v100:2", 6, "a100,v100", "two types"},
		{"gpu:tesla:4(IDX:0-3)", 4, "tesla", "GresUsed"},
		{"N/A", 0, "", "none"},
		{"(null)", 0, "", "null"},
		{"", 0, "", "empty"},
		{"craynetwork:4", 0, "", "not a gpu"},
		{"gpu:x:y", 0, "", "bad count"},
		{"gres/gpu=abc", 0, "", "bad tres count"},
		{"shard:2,gpu:1", 1, "", "mixed"},
	}
	for _, tc := range cases {
		n, typ := ParseGRES(tc.in)
		if n != tc.n || typ != tc.typ {
			t.Errorf("%s: ParseGRES(%q) = %d, %q; want %d, %q", tc.desc, tc.in, n, typ, tc.n, tc.typ)
		}
	}

	m := ParseTRES("cpu=4,mem=8G,node=1,billing=4,gres/gpu=1")
	if m["cpu"] != "4" || m["mem"] != "8G" || m["gres/gpu"] != "1" {
		t.Fatalf("ParseTRES = %v", m)
	}
	if len(ParseTRES("")) != 0 || len(ParseTRES("(null)")) != 0 {
		t.Fatal("empty TRES")
	}
	if n, typ := GPUsFromTRES(ParseTRES("cpu=8,gres/gpu=2,gres/gpu:a100=2,gres/gpumem=80G")); n != 2 || typ != "a100" {
		t.Fatalf("untyped total must win: %d %q", n, typ)
	}
	if n, typ := GPUsFromTRES(ParseTRES("gres/gpu:a100=2,gres/gpu:v100=1")); n != 3 || typ != "a100,v100" {
		t.Fatalf("typed sum: %d %q", n, typ)
	}
	if n, _ := GPUsFromTRES(ParseTRES("cpu=1,gres/gpuutil=50")); n != 0 {
		t.Fatalf("gpuutil is not a count: %d", n)
	}
}

func TestStates(t *testing.T) {
	if s, uid := ParseJobState("CANCELLED by 1234"); s != model.StateCancelled || uid != "1234" {
		t.Fatalf("got %q %q", s, uid)
	}
	if s, uid := ParseJobState("CANCELLED by 0"); s != model.StateCancelled || uid != "0" {
		t.Fatalf("got %q %q", s, uid)
	}
	if s, _ := ParseJobState("running"); s != model.StateRunning {
		t.Fatalf("got %q", s)
	}
	if s, _ := ParseJobState("OUT_OF_ME+"); s != "OUT_OF_ME" {
		t.Fatalf("truncated state %q", s)
	}
	if s, _ := ParseJobState("WEIRD_NEW_STATE"); s != "WEIRD_NEW_STATE" {
		t.Fatal("unknown state must be kept")
	}

	node := map[string]struct {
		base  string
		flags []string
	}{
		"MIXED+DRAIN":                 {"MIXED", []string{"DRAIN"}},
		"DOWN+NOT_RESPONDING":         {"DOWN", []string{"NOT_RESPONDING"}},
		"DOWN+DRAIN+NOT_RESPONDING":   {"DOWN", []string{"DRAIN", "NOT_RESPONDING"}},
		"IDLE*":                       {"IDLE", []string{"NOT_RESPONDING"}},
		"idle~":                       {"IDLE", []string{"POWERED_DOWN"}},
		"ALLOCATED":                   {"ALLOCATED", nil},
		"IDLE+DRAIN+INVALID_REG":      {"IDLE", []string{"DRAIN", "INVALID_REG"}},
		"MIXED+PLANNED+SOMETHING_NEW": {"MIXED", []string{"PLANNED", "SOMETHING_NEW"}},
		"drain*":                      {"DRAIN", []string{"NOT_RESPONDING"}},
		"UNKNOWN+NOT_RESPONDING":      {"UNKNOWN", []string{"NOT_RESPONDING"}},
	}
	for in, want := range node {
		base, flags := ParseNodeState(in)
		if base != want.base || !reflect.DeepEqual(flags, want.flags) {
			t.Errorf("ParseNodeState(%q) = %q %v; want %q %v", in, base, flags, want.base, want.flags)
		}
	}

	for in, want := range map[string][2]int{"0:0": {0, 0}, "3:0": {3, 0}, "0:9": {0, 9}, "137:0": {137, 0}, "": {0, 0}, "1": {1, 0}} {
		c, s, err := ParseExitCode(in)
		if err != nil || c != want[0] || s != want[1] {
			t.Errorf("ParseExitCode(%q) = %d %d %v", in, c, s, err)
		}
	}
	for _, bad := range []string{"a:b", "x", "1:y"} {
		if _, _, err := ParseExitCode(bad); err == nil {
			t.Errorf("ParseExitCode(%q) should fail", bad)
		}
	}
}

func TestExpandLogPattern(t *testing.T) {
	v := LogVars{JobID: "812", Name: "train", User: "alice", ArrayJobID: "800", TaskID: "7", FirstNode: "gpu01", WorkDir: "/home/alice/run"}
	cases := map[string]string{
		"/logs/%x-%j.out":        "/logs/train-812.out",
		"slurm-%A_%a.out":        "/home/alice/run/slurm-800_7.out",
		"%u/%N/%j.err":           "/home/alice/run/alice/gpu01/812.err",
		"/x/100%%.log":           "/x/100%.log",
		"/x/%4a.log":             "/x/0007.log",
		"/x/%s-%j.log":           "/x/%s-812.log",
		"/plain/file.out":        "/plain/file.out",
		"trailing%":              "/home/alice/run/trailing%",
		"/x/%12":                 "/x/%12",
		"":                       "",
		"/home/alice/run/%x.out": "/home/alice/run/train.out",
	}
	for in, want := range cases {
		if got := ExpandLogPattern(in, v); got != want {
			t.Errorf("ExpandLogPattern(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ExpandLogPattern("rel.out", LogVars{}); got != "rel.out" {
		t.Fatalf("no workdir: %q", got)
	}
	if !strings.HasPrefix(ExpandLogPattern("%x", LogVars{Name: "a", WorkDir: "/w"}), "/w/") {
		t.Fatal("relative join")
	}
}

func FuzzParsers(f *testing.F) {
	for _, s := range []string{"2-12:00:00", "gpu[01-04,07]", "123_[5-99%10].batch", "gres/gpu:h200=2", "4000Mc", "MIXED+DRAIN*", "0:9", "%4a/%x"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) {
		_, _, _ = ParseDuration(s)
		_, _ = ParseLimit(s)
		_, _ = ParseMinutesLimit(s)
		_, _ = ParseTimestamp(s, time.UTC)
		_, _, _ = ParseMemMB(s)
		_, _ = ParseJobID(s)
		_, _ = ExpandHostlist(s)
		_, _ = ParseGRES(s)
		_, _ = GPUsFromTRES(ParseTRES(s))
		_, _ = ParseJobState(s)
		_, _ = ParseNodeState(s)
		_, _, _ = ParseExitCode(s)
		_ = ExpandLogPattern(s, LogVars{JobID: "1", WorkDir: "/w"})
	})
}

func TestFormatCount(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 950: "950", 9999: "9999", 12_000: "12k", 1_500_000: "1.5M", 2_000_000_000: "2G"} {
		if got := FormatCount(n); got != want {
			t.Errorf("FormatCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatLimit(t *testing.T) {
	for d, want := range map[time.Duration]string{
		24 * time.Hour: "1d", 48 * time.Hour: "2d", 12 * time.Hour: "12h", 30 * time.Minute: "30m",
		90 * time.Minute: "1h30m", 45 * time.Second: "45s", 26 * time.Hour: "26h", 0: "0s",
	} {
		if got := FormatLimit(d); got != want {
			t.Errorf("FormatLimit(%v) = %q, want %q", d, got, want)
		}
	}
}
