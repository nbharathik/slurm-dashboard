package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

var testEnv = env("HOME", "/home/alice", "USER", "alice", "SCRATCH", "/scratch/alice")

// findIssue returns the first issue for key, failing the test if absent.
func findIssue(t *testing.T, issues []Issue, key string) Issue {
	t.Helper()
	for _, i := range issues {
		if i.Key == key {
			return i
		}
	}
	t.Fatalf("no issue for %q in %v", key, issues)
	return Issue{}
}

func TestDefaultFileMatchesDefault(t *testing.T) {
	cfg, issues := Parse(defaultFile, testEnv)
	if len(issues) != 0 {
		t.Fatalf("default file has issues: %v", issues)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("default file and Default() drifted:\nfile:    %+v\ndefault: %+v", cfg, Default())
	}
	// Every setting is listed, commented out, with its default value.
	d := Default()
	for _, st := range Settings {
		val, err := st.tomlValue(&d)
		if err != nil {
			t.Fatal(err)
		}
		want := regexp.MustCompile(`(?m)^# ` + st.Key + ` *= *` + regexp.QuoteMeta(val) + `( |$)`)
		if !want.Match(defaultFile) {
			t.Errorf("default.toml lacks \"# %s = %s\"", st.Key, val)
		}
	}
}

func TestDefaultsSane(t *testing.T) {
	for _, speed := range Speeds {
		iv := SpeedIntervals(speed)
		for _, d := range []time.Duration{iv.MyJobs, iv.AllJobs, iv.Cluster, iv.Nodes, iv.QueueRank, iv.History, iv.Fairshare, iv.Storage, iv.Partitions, iv.Resv} {
			if d < MinRefresh {
				t.Errorf("%s goes below the floor: %s", speed, d)
			}
		}
		if iv.Manual != (speed == RefreshManual) {
			t.Errorf("%s: manual = %v", speed, iv.Manual)
		}
	}
	if n := Default().Intervals(); n.MyJobs != 10*time.Second || n.Cluster != 30*time.Second || n.QueueRank != time.Minute {
		t.Errorf("normal changed: %+v", n)
	}
	if SpeedIntervals("turbo") != SpeedIntervals(RefreshNormal) {
		t.Error("an unknown speed must be normal")
	}
	// Defaults must survive their own validation unchanged.
	v := validator{layout: layout{keys: map[string]pos{}}, getenv: testEnv}
	c := Default()
	v.validate(&c)
	if len(v.issues) != 0 || !reflect.DeepEqual(c, Default()) {
		t.Fatalf("Default() fails validation: %v", v.issues)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, issues, err := Load(filepath.Join(t.TempDir(), "nope.toml"), testEnv)
	if err != nil || len(issues) != 0 || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("Load(missing) = %v, %v", issues, err)
	}
}

func TestParseValues(t *testing.T) {
	src := `refresh = "manual"
start_tab = "queue"
theme = "light"
ascii = true
mouse = false
notify = false
notify_command = "notify-send done"
shell = "ssh"
hide_partitions = ["login", "debug"]
cluster_name = "birch"
storage_warn = 80
storage_crit = 95
layout = "detailed"
warn_time_left = "30m"
warn_waste = false

[gpu_names]
nvidia_h200_nvl = "H200"
`
	cfg, issues := Parse([]byte(src), testEnv)
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	want := Config{
		Refresh: "manual", StartTab: "queue", Theme: "light", ASCII: true, Mouse: false, Notify: false,
		NotifyCommand: "notify-send done", Shell: "ssh", HidePartitions: []string{"login", "debug"},
		ClusterName: "birch", StorageWarn: 80, StorageCrit: 95, WarnTimeLeft: "30m", WarnWaste: false, Layout: "detailed",
		GPUNames: map[string]string{"nvidia_h200_nvl": "H200"}, Storage: []StorageEntry{},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("cfg = %+v\nwant  %+v", cfg, want)
	}
	if !cfg.Intervals().Manual {
		t.Error("manual refresh not applied")
	}
}

func TestUnknownKeysWarn(t *testing.T) {
	cfg, issues := Parse([]byte("theem = \"dark\"\n"), testEnv)
	i := findIssue(t, issues, "theem")
	if i.Level != Warning || i.Line != 1 || !strings.Contains(i.Msg, `did you mean "theme"`) {
		t.Fatalf("issue = %+v", i)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatal("an unknown key changed the config")
	}
}

// A config using legacy keys still loads: every old key is named with
// its replacement, and nothing else changes.
func TestOldKeysExplained(t *testing.T) {
	src := `default_tab = "jobs"
[refresh]
preset = "gentle"
myjobs = "20s"
[ui]
theme = "dark"
show_other_users = false
[notify]
on_job_end = "echo hi"
[keys]
cancel = ["x"]
`
	cfg, issues := Parse([]byte(src), testEnv)
	if HasErrors(issues) {
		t.Fatalf("errors: %v", issues)
	}
	for key, want := range map[string]string{
		"default_tab":       "replaced by start_tab",
		"refresh.preset":    "replaced by refresh",
		"refresh.myjobs":    "replaced by refresh",
		"ui":                "replaced by theme, mouse and ascii",
		"notify.on_job_end": "replaced by notify_command",
		"keys":              "removed",
	} {
		if i := findIssue(t, issues, key); i.Level != Warning || !strings.Contains(i.Msg, want) {
			t.Errorf("%s: %+v, want %q", key, i, want)
		}
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("old keys changed the config: %+v", cfg)
	}
}

func TestInvalidValuesFallBackPerKey(t *testing.T) {
	src := `refresh = "turbo"
start_tab = "gpus2"
theme = "neon"
shell = "telnet"
hide_partitions = ["ok", "bad name", "x;y"]
storage_warn = 0
storage_crit = 150
cluster_name = "a\u001b[31mred"
ascii = true
[gpu_names]
good = "G"
bad = ""
`
	cfg, issues := Parse([]byte(src), testEnv)
	d := Default()
	for key, line := range map[string]int{
		"refresh": 1, "start_tab": 2, "theme": 3, "shell": 4, "hide_partitions": 5,
		"storage_warn": 6, "storage_crit": 7, "cluster_name": 8, "gpu_names.bad": 12,
	} {
		if i := findIssue(t, issues, key); i.Level != Error || i.Line != line {
			t.Errorf("%s: %+v, want an error on line %d", key, i, line)
		}
	}
	if cfg.Refresh != d.Refresh || cfg.StartTab != d.StartTab || cfg.Theme != d.Theme || cfg.Shell != d.Shell ||
		cfg.StorageWarn != d.StorageWarn || cfg.StorageCrit != d.StorageCrit || cfg.ClusterName != "" {
		t.Fatalf("invalid values not reset: %+v", cfg)
	}
	if !cfg.ASCII || !slices.Equal(cfg.HidePartitions, []string{"ok"}) || cfg.GPUNames["good"] != "G" || len(cfg.GPUNames) != 1 {
		t.Fatalf("valid values lost: %+v", cfg)
	}
	cfg, issues = Parse([]byte("storage_warn = 95\nstorage_crit = 90\n"), testEnv)
	if i := findIssue(t, issues, "storage_warn"); i.Level != Error || cfg.StorageWarn != 90 || cfg.StorageCrit != 97 {
		t.Errorf("warn above crit: %+v %+v", i, cfg)
	}
}

func TestTypeErrorsIsolated(t *testing.T) {
	src := `start_tab = "jobs"
mouse = "no"
ascii = true
storage_warn = "high"
hide_partitions = "login"
`
	cfg, issues := Parse([]byte(src), testEnv)
	for _, want := range []struct {
		key  string
		line int
	}{{"mouse", 2}, {"storage_warn", 4}, {"hide_partitions", 5}} {
		i := findIssue(t, issues, want.key)
		if i.Level != Error || i.Line != want.line {
			t.Errorf("%s: %+v, want error at line %d", want.key, i, want.line)
		}
	}
	d := Default()
	if cfg.StartTab != "jobs" || !cfg.ASCII {
		t.Fatalf("valid values lost: %+v", cfg)
	}
	if cfg.Mouse != d.Mouse || cfg.StorageWarn != d.StorageWarn || len(cfg.HidePartitions) != 0 {
		t.Fatalf("invalid values not reset: %+v", cfg)
	}
}

func TestMultilineTypeErrorKeepsLines(t *testing.T) {
	src := "storage_warn = [\n  1,\n  2,\n]\nascii = true\n\ntheme = 7\n"
	cfg, issues := Parse([]byte(src), testEnv)
	if i := findIssue(t, issues, "storage_warn"); i.Line != 1 {
		t.Fatalf("array type error: %+v", i)
	}
	if i := findIssue(t, issues, "theme"); i.Line != 7 {
		t.Fatalf("second type error line moved: %+v", i)
	}
	if !cfg.ASCII || cfg.Theme != "auto" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestSyntaxErrorUsesDefaults(t *testing.T) {
	src := "start_tab = \"jobs\"\n[gpu_names\nx = \"y\"\n"
	cfg, issues := Parse([]byte(src), testEnv)
	if len(issues) != 1 || issues[0].Level != Error || issues[0].Line != 2 || !strings.Contains(issues[0].Msg, "defaults") {
		t.Fatalf("issues = %v", issues)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatal("syntax error must yield defaults")
	}
}

func TestHeaderTypeErrorUsesDefaults(t *testing.T) {
	src := "start_tab = \"jobs\"\n[theme]\nx = \"dark\"\n"
	cfg, issues := Parse([]byte(src), testEnv)
	if !HasErrors(issues) || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("issues=%v cfg=%+v", issues, cfg)
	}
}

func TestStorageValidation(t *testing.T) {
	src := `[[storage]]
label = "NoPath"

[[storage]]
label = "Unset"
path = "$WORK/x"

[[storage]]
label = "Relative"
path = "data"

[[storage]]
label = "BadBackend"
path = "/data"
backend = "zfs"

[[storage]]
label = "NoCommand"
backend = "command"

[[storage]]
label = "BadCommand"
backend = "command"
command = [1, 2]

[[storage]]
label = "IgnoredCommand"
path = "/ok"
command = ["myquota"]
`
	cfg, issues := Parse([]byte(src), testEnv)
	if len(cfg.Storage) != 1 || cfg.Storage[0].Label != "IgnoredCommand" || !cfg.Storage[0].Command.IsZero() {
		t.Fatalf("storage = %+v", cfg.Storage)
	}
	for key, line := range map[string]int{
		"storage[1].path":    1, // falls back to the entry header
		"storage[2].path":    6,
		"storage[3].path":    10,
		"storage[4].backend": 15,
		"storage[5].command": 17,
		"storage[6].command": 24,
		"storage[7].command": 29,
	} {
		if i := findIssue(t, issues, key); i.Line != line {
			t.Errorf("%s: %+v, want line %d", key, i, line)
		}
	}
	if i := findIssue(t, issues, "storage[2].path"); i.Level != Warning || !strings.Contains(i.Msg, "$WORK") {
		t.Errorf("unset var issue = %+v", i)
	}
}

func TestIssuesSortedByLine(t *testing.T) {
	src := "theem = \"dark\"\n[refresh]\nmyjobs = \"1s\"\nnodes = \"fast\"\n"
	_, issues := Parse([]byte(src), testEnv)
	var lines []int
	for _, i := range issues {
		lines = append(lines, i.Line)
	}
	if !slices.Equal(lines, []int{1, 3, 4}) {
		t.Fatalf("issue lines = %v, want [1 3 4]", lines)
	}
	mixed := []Issue{{Line: 0, Msg: "a"}, {Line: 3, Col: 2}, {Line: 3, Col: 1}, {Line: 1}}
	sortIssues(mixed)
	if mixed[0].Line != 1 || mixed[1].Col != 1 || mixed[2].Col != 2 || mixed[3].Line != 0 {
		t.Fatalf("sortIssues = %+v", mixed)
	}
}

func TestIssueString(t *testing.T) {
	i := Issue{Level: Error, Line: 3, Col: 7, Key: "theme", Msg: "bad"}
	if got := i.String(); got != "3:7: error: theme: bad" {
		t.Fatalf("String = %q", got)
	}
	if got := (Issue{Level: Warning, Msg: "x"}).String(); got != "warning: x" {
		t.Fatalf("String = %q", got)
	}
}

func TestEditDistanceAndSuggest(t *testing.T) {
	if editDistance("kitten", "sitting") != 3 || editDistance("", "abc") != 3 || editDistance("same", "same") != 0 {
		t.Fatal("editDistance")
	}
	if got := suggestKey([]string{"theem"}); got != "theme" {
		t.Fatalf("suggest = %q", got)
	}
	if got := suggestKey([]string{"storage", "lable"}); got != "label" {
		t.Fatalf("suggest in array table = %q", got)
	}
	if got := suggestKey([]string{"zzzzzzzz"}); got != "" {
		t.Fatalf("far-off key suggested %q", got)
	}
}

func TestProfiles(t *testing.T) {
	env := func(k string) string { return map[string]string{"HOME": "/home/alice"}[k] }
	cfg, issues := Parse([]byte(`
[profiles.big]
slurm_conf = "~/other/slurm.conf"
cluster_name = "big"

[profiles."bad name"]
slurm_conf = "/x"

[profiles.noconf]
cluster_name = "x"

[profiles.rel]
slurm_conf = "relative.conf"

[profiles.typo]
slurm_conf = "/etc/slurm/slurm.conf"
clustr_name = "x"
`), env)
	if p, ok := cfg.Profiles["big"]; !ok || p.SlurmConf != "/home/alice/other/slurm.conf" || p.ClusterName != "big" {
		t.Fatalf("big = %+v", cfg.Profiles)
	}
	for _, gone := range []string{"bad name", "noconf", "rel"} {
		if _, ok := cfg.Profiles[gone]; ok {
			t.Errorf("profile %q kept", gone)
		}
	}
	keys := map[string]bool{}
	for _, is := range issues {
		keys[is.Key] = true
	}
	for _, want := range []string{"profiles.bad name", "profiles.noconf.slurm_conf", "profiles.rel.slurm_conf", "profiles.typo.clustr_name"} {
		if !keys[want] {
			t.Errorf("no issue for %s: %v", want, issues)
		}
	}
}

func TestLoadLayers(t *testing.T) {
	dir := t.TempDir()
	site := filepath.Join(dir, "site.toml")
	user := filepath.Join(dir, "user.toml")
	mustWrite := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(site, `cluster_name = "birch"
refresh = "slow"
hide_partitions = ["login"]
[gpu_names]
nvidia_h200_nvl = "H200"
[[storage]]
label = "Project"
path = "/project/lab"
`)
	mustWrite(user, `start_tab = "nodes"
hide_partitions = []
theme = "nope"
[gpu_names]
"1g.16gb" = "small slice"
`)
	cfg, issues, err := LoadLayers([]string{site, user, filepath.Join(dir, "missing.toml")}, testEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClusterName != "birch" || cfg.Refresh != "slow" || cfg.StartTab != "nodes" {
		t.Errorf("scalars: %q %q %q", cfg.ClusterName, cfg.Refresh, cfg.StartTab)
	}
	if cfg.GPUNames["nvidia_h200_nvl"] != "H200" || cfg.GPUNames["1g.16gb"] != "small slice" {
		t.Errorf("tables must merge: %v", cfg.GPUNames)
	}
	if len(cfg.HidePartitions) != 0 {
		t.Errorf("arrays must be replaced: %v", cfg.HidePartitions)
	}
	if len(cfg.Storage) != 1 || cfg.Storage[0].Label != "Project" {
		t.Errorf("storage: %+v", cfg.Storage)
	}
	is := findIssue(t, issues, "theme")
	if is.File != user || !strings.HasPrefix(is.String(), user+":") {
		t.Errorf("issue should name its file: %q", is.String())
	}
	if cfg.Theme != "auto" {
		t.Errorf("invalid user value falls back to the lower layer's (here the default): %q", cfg.Theme)
	}
	cfg, issues, err = LoadLayers([]string{filepath.Join(dir, "none.toml")}, testEnv)
	if err != nil || len(issues) != 0 || !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("no files: %v %v", issues, err)
	}
}

func TestStartTabAcceptsAliases(t *testing.T) {
	for in, want := range map[string]string{"history": "usage", "usage": "usage", "gpus": "nodes", "Queue": "queue"} {
		cfg, issues := Parse([]byte("start_tab = \""+in+"\"\n"), testEnv)
		if cfg.StartTab != want {
			t.Errorf("start_tab %q -> %q, want %q (issues %v)", in, cfg.StartTab, want, issues)
		}
	}
	cfg, issues := Parse([]byte("start_tab = \"nowhere\"\n"), testEnv)
	if i := findIssue(t, issues, "start_tab"); i.Level != Error || cfg.StartTab != Default().StartTab {
		t.Errorf("unknown tab: %+v %q", i, cfg.StartTab)
	}
}

func TestWarnSettings(t *testing.T) {
	for in, want := range map[string]time.Duration{"off": -1, "5m": 5 * time.Minute, "10m": 10 * time.Minute, "1h": time.Hour, "0": -1, "none": -1} {
		cfg, issues := Parse([]byte("warn_time_left = \""+in+"\"\n"), testEnv)
		if got := cfg.TimeLeftWarn(); got != want || len(issues) != 0 {
			t.Errorf("warn_time_left %q -> %v (issues %v), want %v", in, got, issues, want)
		}
	}
	cfg, issues := Parse([]byte("warn_time_left = \"soon\"\n"), testEnv)
	if i := findIssue(t, issues, "warn_time_left"); i.Level != Error || cfg.WarnTimeLeft != "10m" {
		t.Errorf("bad value: %+v %q", i, cfg.WarnTimeLeft)
	}
	if d := Default(); d.TimeLeftWarn() != 10*time.Minute || !d.WarnWaste {
		t.Errorf("defaults: %+v", d)
	}
}
