package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetLine(t *testing.T) {
	for _, c := range []struct{ name, in, key, val, want string }{
		{
			"replace keeps the comment column",
			"theme = \"auto\"   # colours\nmouse = true\n",
			"theme", `"dark"`,
			"theme = \"dark\"   # colours\nmouse = true\n",
		},
		{
			"uncomment the template line",
			"# refresh         = \"normal\"    # fast | normal\n# mouse = true\n",
			"refresh", `"manual"`,
			"refresh         = \"manual\"    # fast | normal\n# mouse = true\n",
		},
		{
			"insert before the first table",
			"# settings\nascii = true\n\n[gpu_names]\na = \"b\"\n",
			"theme", `"light"`,
			"# settings\nascii = true\n\ntheme = \"light\"\n\n[gpu_names]\na = \"b\"\n",
		},
		{
			"a key inside a table is not the setting",
			"[profiles.x]\ntheme = \"old\"\n",
			"theme", `"dark"`,
			"theme = \"dark\"\n\n[profiles.x]\ntheme = \"old\"\n",
		},
		{
			"a multi-line array becomes one line",
			"hide_partitions = [\n  \"login\", # front ends\n  \"debug\",\n]\nmouse = false\n",
			"hide_partitions", `["gpu"]`,
			"hide_partitions = [\"gpu\"]\nmouse = false\n",
		},
		{
			"a hash inside a string is not a comment",
			"cluster_name = \"a#b\" # name\n",
			"cluster_name", `"c"`,
			"cluster_name = \"c\"   # name\n",
		},
		{
			"no trailing newline",
			"ascii = true",
			"mouse", "false",
			"ascii = true\nmouse = false\n",
		},
	} {
		if got := setLine(c.in, c.key, c.val); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sdash", "config.toml")
	c := Default()
	c.Refresh = RefreshManual
	if err := Save(path, c, "refresh"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "# sdash settings") || !strings.Contains(string(b), "refresh = \"manual\"\n") || strings.Contains(string(b), "theme") {
		t.Fatalf("a new file holds only the changed setting:\n%s", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode())
	}

	// Existing files keep their comments, and a .bak of the last version.
	if err := os.WriteFile(path, defaultFile, 0o600); err != nil {
		t.Fatal(err)
	}
	c.HidePartitions = []string{"login", "debug"}
	for _, key := range []string{"refresh", "hide_partitions"} {
		if err := Save(path, c, key); err != nil {
			t.Fatal(err)
		}
	}
	got, issues, err := Load(path, testEnv)
	if err != nil || len(issues) != 0 || got.Refresh != RefreshManual || strings.Join(got.HidePartitions, ",") != "login,debug" {
		t.Fatalf("reload = %+v %v %v", got, issues, err)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "# Short names for GPU types") || !strings.Contains(string(b), "# fast | normal | slow | manual") {
		t.Errorf("comments lost:\n%s", b)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}

	// Commands and unknown keys are never written.
	for _, key := range []string{"notify_command", "cluster_name", "nope"} {
		if err := Save(path, c, key); !errors.Is(err, ErrNotSettable) {
			t.Errorf("Save(%s) = %v", key, err)
		}
	}
	// A file an older version wrote, with [refresh] as a table, is left
	// alone with a message.
	if err := os.WriteFile(path, []byte("[refresh]\nmyjobs = \"20s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, c, "refresh"); err == nil || !strings.Contains(err.Error(), "sdash config edit") {
		t.Errorf("old table: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "[refresh]\nmyjobs = \"20s\"\n" {
		t.Errorf("a failed save changed the file: %q", b)
	}
}

func TestSaveFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles.toml")
	link := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(target, []byte("ascii = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c := Default()
	c.Theme = "dark"
	if err := Save(link, c, "theme"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "ascii = true\ntheme = \"dark\"\n" {
		t.Fatalf("target = %q", b)
	}
}

func TestSettingsRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, st := range Settings {
		n := 0
		for _, set := range []bool{st.Str != nil, st.Bool != nil, st.Int != nil, st.List != nil} {
			if set {
				n++
			}
		}
		if n != 1 || st.Help == "" || seen[st.Key] || (st.Screen && st.Label == "") {
			t.Errorf("bad setting %+v", st)
		}
		seen[st.Key] = true
		if st.Screen && st.Key == "notify_command" {
			t.Error("commands must not be on the Settings screen")
		}
	}
	c := Default()
	refresh, _ := Lookup("refresh")
	refresh.Step(&c, 1)
	if c.Refresh != RefreshSlow {
		t.Errorf("step = %s", c.Refresh)
	}
	refresh.Step(&c, -1)
	refresh.Step(&c, -1)
	refresh.Step(&c, -1)
	if c.Refresh != RefreshManual { // wraps around
		t.Errorf("wrap = %s", c.Refresh)
	}
	warn, _ := Lookup("storage_warn")
	c.StorageWarn = 99
	warn.Step(&c, 1)
	if c.StorageWarn != 99 || warn.Format(&c) != "99" {
		t.Errorf("warn above its max: %d", c.StorageWarn)
	}
	mouse, _ := Lookup("mouse")
	mouse.Step(&c, 1)
	if c.Mouse || mouse.Format(&c) != "off" {
		t.Error("toggle")
	}
	hide, _ := Lookup("hide_partitions")
	if hide.Format(&c) != "-" {
		t.Errorf("empty list = %q", hide.Format(&c))
	}
}

// docs/config.md describes every setting.
func TestDocsListEverySetting(t *testing.T) {
	b, err := os.ReadFile("../../docs/config.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range Settings {
		if !strings.Contains(string(b), "`"+st.Key+"`") {
			t.Errorf("docs/config.md does not describe %s", st.Key)
		}
	}
}
