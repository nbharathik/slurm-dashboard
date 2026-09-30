package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestExpandPath(t *testing.T) {
	cases := []struct {
		in, want string
		missing  []string
	}{
		{"~", "/home/alice", nil},
		{"~/data", "/home/alice/data", nil},
		{"$HOME/x", "/home/alice/x", nil},
		{"${SCRATCH}/y", "/scratch/alice/y", nil},
		{"/scratch/$USER", "/scratch/alice", nil},
		{"/work/$NOPE/z", "/work/z", []string{"NOPE"}},
		{"~bob/x", "~bob/x", nil},
		{"/plain//path/", "/plain/path", nil},
	}
	for _, tc := range cases {
		got, missing := ExpandPath(tc.in, testEnv)
		if got != tc.want || !slices.Equal(missing, tc.missing) {
			t.Errorf("ExpandPath(%q) = %q, %v; want %q, %v", tc.in, got, missing, tc.want, tc.missing)
		}
	}
}

func TestResolvePaths(t *testing.T) {
	p, err := ResolvePaths(env("HOME", "/home/alice"))
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		ConfigDir:  "/home/alice/.config/sdash",
		ConfigFile: "/home/alice/.config/sdash/config.toml",
		StateDir:   "/home/alice/.local/state/sdash",
		CacheDir:   "/home/alice/.cache/sdash",
	}
	if p != want {
		t.Fatalf("ResolvePaths = %+v", p)
	}

	p, err = ResolvePaths(env("HOME", "/home/alice", "XDG_CONFIG_HOME", "/xdg/cfg", "XDG_STATE_HOME", "relative/state", "XDG_CACHE_HOME", "/xdg/cache"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile != "/xdg/cfg/sdash/config.toml" || p.StateDir != "/home/alice/.local/state/sdash" || p.CacheDir != "/xdg/cache/sdash" {
		t.Fatalf("XDG paths = %+v", p)
	}

	if _, err := ResolvePaths(env()); err == nil {
		t.Fatal("no HOME and no XDG must fail")
	}
}

func TestWriteDefault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg", "sdash")
	path := filepath.Join(dir, "config.toml")

	backup, err := WriteDefault(path, false)
	if err != nil || backup != "" {
		t.Fatalf("WriteDefault = %q, %v", backup, err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v", fi.Mode().Perm(), err)
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", di.Mode().Perm())
	}
	cfg, issues, err := Load(path, testEnv)
	if err != nil || len(issues) != 0 || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("written file does not validate: %v %v", issues, err)
	}

	if err := os.WriteFile(path, []byte("default_tab = \"jobs\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDefault(path, false); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite without force: %v", err)
	}
	backup, err = WriteDefault(path, true)
	if err != nil || backup != path+".bak" {
		t.Fatalf("force: %q, %v", backup, err)
	}
	old, _ := os.ReadFile(backup)
	if string(old) != "default_tab = \"jobs\"\n" {
		t.Fatalf("backup = %q", old)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".config-") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}
