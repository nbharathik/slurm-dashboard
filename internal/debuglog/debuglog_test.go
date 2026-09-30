package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotationKeepsThreeFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sdash")
	log, closer, err := open(dir, true, 200)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		log.Debug("line", "i", i, "pad", strings.Repeat("x", 40))
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		fi, _ := e.Info()
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", e.Name(), fi.Mode().Perm())
		}
		if fi.Size() > 200 {
			t.Errorf("%s is %d bytes, over the 200-byte limit", e.Name(), fi.Size())
		}
	}
	want := []string{"debug.log", "debug.log.1", "debug.log.2"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v, want %v", names, want)
	}
	last, _ := os.ReadFile(filepath.Join(dir, "debug.log"))
	if !strings.Contains(string(last), "i=49") {
		t.Fatalf("newest record not in debug.log: %q", last)
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", di.Mode().Perm())
	}
}

func TestLevels(t *testing.T) {
	dir := t.TempDir()
	log, closer, err := Open(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("hidden")
	log.Info("shown")
	_ = closer.Close()
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	if strings.Contains(string(b), "hidden") || !strings.Contains(string(b), "shown") {
		t.Fatalf("log = %q", b)
	}
}

func TestOpenFailureDiscards(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	log, closer, err := Open(filepath.Join(file, "sub"), true)
	if err == nil || log == nil || closer == nil {
		t.Fatalf("want a discard logger and an error, got %v", err)
	}
	log.Info("goes nowhere")
	if closer.Close() != nil {
		t.Fatal("nop closer")
	}
}

func TestWriteCrash(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteCrash(dir, "boom", []byte("goroutine 1 [running]:"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(path), "crash-") {
		t.Fatalf("path = %s", path)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "panic: boom") || !strings.Contains(string(b), "goroutine 1") {
		t.Fatalf("crash file = %q", b)
	}
}
