package privatefile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrivatePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := Write(path, []byte("ok")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, 1); err == nil {
		t.Fatal("unbounded read")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission and symlink checks")
	}
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, 100); err == nil {
		t.Fatal("read symlink")
	}
	if _, err := Append(path); err == nil {
		t.Fatal("append symlink")
	}
	if err := Write(path, []byte("changed")); err == nil {
		t.Fatal("write symlink")
	}
	b, _ := os.ReadFile(other)
	if string(b) != "secret" {
		t.Fatal("target changed")
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(dir, "new"), []byte("data")); err == nil {
		t.Fatal("unsafe directory accepted")
	}
}
