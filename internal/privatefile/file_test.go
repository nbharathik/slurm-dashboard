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
	// The user's own directory with loose permissions is tightened, not refused.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(dir, "new"), []byte("data")); err != nil {
		t.Fatalf("own directory: %v", err)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode %o, want 0700", st.Mode().Perm())
	}
	// A planted file the user does not own, or a symlinked directory, is refused.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := Directory(link); err == nil {
		t.Fatal("symlinked state directory accepted")
	}
}

// Shared parents (group-writable homes, umask 002) do not disable state.
func TestSharedParentAccepted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	parent := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(parent, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o775); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, ".local", "state", "sdash", "state.json")
	if err := Write(path, []byte("{}")); err != nil {
		t.Fatalf("group-writable parent refused: %v", err)
	}
	if b, err := Read(path, 10); err != nil || string(b) != "{}" {
		t.Fatalf("read back: %q %v", b, err)
	}
}
