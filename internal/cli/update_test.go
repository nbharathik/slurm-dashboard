package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/update"
)

// releaseServer serves tag with a linux/amd64 archive holding bin. With
// corrupt set, checksums.txt does not match the archive.
func releaseServer(t *testing.T, tag string, bin []byte, corrupt bool) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Name: "sdash", Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(bin)
	_ = tw.Close()
	_ = zw.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	if corrupt {
		sum[0] ^= 0xff
	}
	name := update.ArchiveName(tag, "linux", "amd64")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	})
	mux.HandleFunc("/dl/"+tag+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + name + "\n"))
	})
	mux.HandleFunc("/dl/"+tag+"/"+name, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runUpdate(t *testing.T, srv *httptest.Server, current, exe string, args ...string) (*harness, int) {
	t.Helper()
	h := newHarness(t)
	a := &app{env: h.env(), updater: &update.Updater{
		Client: srv.Client(), API: srv.URL + "/api", Download: srv.URL + "/dl",
		Current: current, GOOS: "linux", GOARCH: "amd64", Executable: exe,
	}}
	return h, run(context.Background(), newRoot(a), a, append([]string{"update"}, args...))
}

func TestUpdateCommand(t *testing.T) {
	newBin := []byte("\x7fELF new")
	srv := releaseServer(t, "v0.2.0", newBin, false)
	exe := filepath.Join(t.TempDir(), "sdash")
	if err := os.WriteFile(exe, []byte("\x7fELF old"), 0o755); err != nil {
		t.Fatal(err)
	}

	h, code := runUpdate(t, srv, "v0.1.0", exe, "--check")
	if code != ExitOK || !strings.Contains(h.stdout.String(), "v0.2.0 is available (you have v0.1.0). Run: sdash update") {
		t.Fatalf("--check: %d %q %q", code, h.stdout.String(), h.stderr.String())
	}
	h, code = runUpdate(t, srv, "v0.1.0", exe, "--check", "--json")
	if code != ExitOK || !strings.Contains(h.stdout.String(), `"update_available": true`) {
		t.Fatalf("--check --json: %d %q", code, h.stdout.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "\x7fELF old" {
		t.Fatal("--check changed the binary")
	}

	h, code = runUpdate(t, srv, "v0.2.0", exe)
	if code != ExitOK || !strings.Contains(h.stdout.String(), "up to date") {
		t.Fatalf("up to date: %d %q", code, h.stdout.String())
	}
	h, code = runUpdate(t, srv, "v0.3.0", exe)
	if code != ExitOK || !strings.Contains(h.stdout.String(), "up to date") {
		t.Fatalf("newer than latest must not downgrade: %d %q", code, h.stdout.String())
	}

	h, code = runUpdate(t, srv, "v0.1.0", exe)
	if code != ExitOK || !strings.Contains(h.stdout.String(), "from v0.1.0 to v0.2.0 (checksum verified)") {
		t.Fatalf("update: %d %q %q", code, h.stdout.String(), h.stderr.String())
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, newBin) {
		t.Fatal("binary not replaced")
	}
}

func TestUpdateRefusals(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "sdash")
	if err := os.WriteFile(exe, []byte("\x7fELF old"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := releaseServer(t, "v0.2.0", []byte("\x7fELF new"), true)
	h, code := runUpdate(t, bad, "v0.1.0", exe)
	if code != ExitError || !strings.Contains(h.stderr.String(), "checksum mismatch") || !strings.Contains(h.stderr.String(), "nothing was changed") {
		t.Fatalf("bad checksum: %d %q", code, h.stderr.String())
	}
	if b, _ := os.ReadFile(exe); string(b) != "\x7fELF old" {
		t.Fatal("a bad checksum changed the binary")
	}

	good := releaseServer(t, "v0.2.0", []byte("\x7fELF new"), false)
	h, code = runUpdate(t, good, "dev", exe)
	if code != ExitError || !strings.Contains(h.stderr.String(), "development build") {
		t.Fatalf("dev build: %d %q", code, h.stderr.String())
	}
	if _, code = runUpdate(t, good, "v0.1.0", exe, "--check", "--version", "v0.2.0"); code != ExitUsage {
		t.Fatalf("--check --version: exit %d", code)
	}
	if _, code = runUpdate(t, good, "v0.1.0", exe, "--json"); code != ExitUsage {
		t.Fatalf("--json without --check: exit %d", code)
	}
}

func TestUpdateWithoutReleaseOrFromCheckout(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "sdash")
	if err := os.WriteFile(exe, []byte("\x7fELF old"), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(empty.Close)
	for _, args := range [][]string{{"--check"}, nil} {
		h, code := runUpdate(t, empty, "v0.1.0", exe, args...)
		if code != ExitOK || !strings.Contains(h.stdout.String(), "No sdash release has been published yet") || !strings.Contains(h.stdout.String(), "make update") {
			t.Fatalf("%v: %d %q %q", args, code, h.stdout.String(), h.stderr.String())
		}
	}
	h, code := runUpdate(t, empty, "v0.1.0-4-gabc1234", exe)
	if code != ExitError || !strings.Contains(h.stderr.String(), "git checkout") || !strings.Contains(h.stderr.String(), "make update") {
		t.Fatalf("checkout build: %d %q", code, h.stderr.String())
	}
}
