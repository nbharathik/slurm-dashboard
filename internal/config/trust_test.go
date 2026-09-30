package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveDoesNotPromoteUntrustedCommands(t *testing.T) {
	for _, kind := range []string{"file", "parent", "link parent", "target parent", "dangling link"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "config.toml")
			body := []byte("notify_command = 'echo untrusted'\n[[storage]]\nlabel = 'quota'\npath = '/tmp'\nbackend = 'command'\ncommand = ['quota']\nformat = 'json'\n")
			if err := os.WriteFile(target, body, 0o600); err != nil {
				t.Fatal(err)
			}
			path := target
			unsafe := dir
			if strings.Contains(kind, "link") || kind == "target parent" {
				linkDir := t.TempDir()
				path = filepath.Join(linkDir, "config.toml")
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
				if kind == "link parent" {
					unsafe = linkDir
				}
			}
			if kind == "dangling link" {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			} else {
				mode := os.FileMode(0o770)
				if kind == "file" {
					unsafe, mode = target, 0o664
				}
				if err := os.Chmod(unsafe, mode); err != nil {
					t.Fatal(err)
				}
			}
			cfg, _, err := LoadLayers([]string{path}, testEnv)
			if err != nil || cfg.NotifyCommand != "" || len(cfg.Storage) != 0 {
				t.Fatalf("initial load trusted commands: %+v, %v", cfg, err)
			}
			cfg.Theme = "light"
			if err := Save(path, cfg, "theme"); err == nil || !strings.Contains(err.Error(), "untrusted config") {
				t.Fatalf("save = %v", err)
			}
			if kind != "dangling link" {
				got, err := os.ReadFile(target)
				if err != nil || !bytes.Equal(got, body) {
					t.Fatalf("config changed: %q, %v", got, err)
				}
			}
			cfg, _, err = LoadLayers([]string{path}, testEnv)
			if err != nil || cfg.NotifyCommand != "" || len(cfg.Storage) != 0 {
				t.Fatalf("save promoted commands: %+v, %v", cfg, err)
			}
			if _, err := os.Lstat(target + ".bak"); !os.IsNotExist(err) {
				t.Fatalf("unexpected backup: %v", err)
			}
		})
	}
}

func TestSavePreservesTrustedCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("notify_command = 'echo trusted'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.Theme = "light"
	if err := Save(path, cfg, "theme"); err != nil {
		t.Fatal(err)
	}
	got, issues, err := LoadLayers([]string{path}, testEnv)
	if err != nil || len(issues) != 0 || got.NotifyCommand != "echo trusted" || got.Theme != "light" {
		t.Fatalf("saved config: %+v, %v, %v", got, issues, err)
	}
}

func TestUntrustedConfigLosesCommands(t *testing.T) {
	uid := os.Getuid()
	t.Cleanup(func() { getuid = os.Getuid })
	getuid = func() int { return uid } // the owner of the temp files, even when tests run as root
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `notify_command = "curl -d @- https://example.org"

[[storage]]
label = "Home"
path = "/home/alice"

[[storage]]
label = "Project"
path = "/project/lab"
backend = "command"
command = ["myquota", "--json"]
format = "json"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, issues, err := LoadLayers([]string{path}, testEnv)
	if err != nil || cfg.NotifyCommand == "" || len(cfg.Storage) != 2 || len(issues) != 0 {
		t.Fatalf("a private file keeps its commands: %q %v %v", cfg.NotifyCommand, issues, err)
	}
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	cfg, issues, _ = LoadLayers([]string{path}, testEnv)
	if cfg.NotifyCommand != "" || len(cfg.Storage) != 1 || cfg.Storage[0].Label != "Home" {
		t.Fatalf("a group-writable file must lose its hook and command storage: %q %+v", cfg.NotifyCommand, cfg.Storage)
	}
	if len(issues) != 1 || issues[0].Level != Error || !strings.Contains(issues[0].Msg, "writable by its group") {
		t.Fatalf("issues = %v", issues)
	}
	getuid = func() int { return uid + 1 }
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if uid != 0 { // root-owned files are trusted, so this only shows for other owners
		if is := TrustIssues(path); len(is) != 1 || !strings.Contains(is[0].Msg, "another user") {
			t.Fatalf("other owner: %v", is)
		}
	}
	getuid = func() int { return uid }
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if is := TrustIssues(path); len(is) != 1 || !strings.Contains(is[0].Msg, "directory") {
		t.Fatalf("world-writable directory: %v", is)
	}
	if err := os.Chmod(dir, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if is := TrustIssues(path); len(is) != 0 {
		t.Fatalf("a sticky directory such as /tmp is fine: %v", is)
	}
}

func TestConfigCommandsRejectWritableParents(t *testing.T) {
	for _, mode := range []os.FileMode{0o770, 0o777} {
		t.Run(mode.String(), func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "private")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(path, []byte("notify_command = 'echo done'\ntheme = 'light'\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(base, mode); err != nil {
				t.Fatal(err)
			}
			cfg, issues, err := LoadLayers([]string{path}, testEnv)
			if err != nil || cfg.NotifyCommand != "" || cfg.Theme != "light" || len(issues) != 1 {
				t.Fatalf("unsafe ancestor: config=%+v issues=%v err=%v", cfg, issues, err)
			}
		})
	}
}

func TestConfigSymlinkChecksTargetParents(t *testing.T) {
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "config.toml")
	if err := os.WriteFile(target, []byte("notify_command = 'echo done'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if issues := TrustIssues(link); len(issues) != 0 {
		t.Fatalf("private symlink target: %v", issues)
	}
	if err := os.Chmod(targetDir, 0o770); err != nil {
		t.Fatal(err)
	}
	cfg, issues, err := LoadLayers([]string{link}, testEnv)
	if err != nil || cfg.NotifyCommand != "" || len(issues) != 1 {
		t.Fatalf("unsafe symlink target: hook=%q issues=%v err=%v", cfg.NotifyCommand, issues, err)
	}
}
