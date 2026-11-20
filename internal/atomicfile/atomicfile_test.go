package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	for _, durable := range []bool{false, true} {
		for _, data := range []string{"one", "two, longer"} {
			if err := Write(path, []byte(data), 0o640, durable); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != data {
				t.Fatalf("read %q, %v", got, err)
			}
		}
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("left behind: %v", entries)
	}
	if err := Write(filepath.Join(dir, "missing", "f"), nil, 0o600, false); err == nil {
		t.Error("a missing directory must fail")
	}
}
