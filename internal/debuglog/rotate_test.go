package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two sdash instances share one log: when one rotates, the other follows
// the new file instead of rotating again, so no fresh log is pushed out.
func TestTwoInstancesRotateOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.log")
	a, err := openRotating(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := openRotating(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	line := strings.Repeat("x", 39) + "\n"
	_, _ = a.Write([]byte(line))
	_, _ = a.Write([]byte(line))
	_, _ = a.Write([]byte(line)) // a rotates: path.1 holds 80 bytes
	_, _ = b.Write([]byte("b-first\n"))
	_, _ = b.Write([]byte(line))
	_, _ = b.Write([]byte(line))
	_, _ = b.Write([]byte(line)) // b's counter says full; the file at path is new
	if _, err := os.Stat(path + ".2"); err == nil {
		t.Fatal("b rotated a second time and pushed a's log to .2")
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cur), "b-first") && !fileContains(t, path+".1", "b-first") {
		t.Fatal("b's lines were lost")
	}
}

func fileContains(t *testing.T, p, s string) bool {
	t.Helper()
	b, err := os.ReadFile(p)
	return err == nil && strings.Contains(string(b), s)
}
