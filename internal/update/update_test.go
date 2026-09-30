package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

var fakeELF = append([]byte("\x7fELF"), bytes.Repeat([]byte{1}, 64)...)

// release is a fake GitHub API and download server.
type release struct {
	tag      string
	files    map[string][]byte // download name -> content
	requests []string
}

func (r *release) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.requests = append(r.requests, req.URL.Path)
	switch {
	case req.URL.Path == "/api/releases/latest":
		_, _ = fmt.Fprintf(w, `{"tag_name": %q, "name": "release"}`, r.tag)
	case strings.HasPrefix(req.URL.Path, "/dl/"+r.tag+"/"):
		b, ok := r.files[strings.TrimPrefix(req.URL.Path, "/dl/"+r.tag+"/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(b)
	default:
		http.NotFound(w, req)
	}
}

func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, b := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// setup serves release v0.2.0 for linux/amd64 and returns an Updater whose
// binary is a temporary file.
func setup(t *testing.T, mutate func(*release)) (*Updater, *release, string) {
	t.Helper()
	archive := tarGz(t, map[string][]byte{"sdash": fakeELF, "LICENSE": []byte("MIT"), "completions/sdash.bash": []byte("# bash")})
	name := ArchiveName("v0.2.0", "linux", "amd64")
	rel := &release{tag: "v0.2.0", files: map[string][]byte{
		name:            archive,
		"checksums.txt": []byte(sha(archive) + "  " + name + "\n" + strings.Repeat("0", 64) + "  sdash_0.2.0_darwin_arm64.tar.gz\n"),
	}}
	if mutate != nil {
		mutate(rel)
	}
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)
	exe := filepath.Join(t.TempDir(), "sdash")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	verifyDir := t.TempDir()
	t.Setenv("TMPDIR", verifyDir)
	t.Cleanup(func() {
		for _, dir := range []string{verifyDir, filepath.Dir(exe)} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Error(err)
			}
			for _, entry := range entries {
				if dir == verifyDir || entry.Name() != "sdash" {
					t.Errorf("update left a temporary file: %s", filepath.Join(dir, entry.Name()))
				}
			}
		}
	})
	u := &Updater{
		Client: srv.Client(), API: srv.URL + "/api", Download: srv.URL + "/dl",
		Current: "v0.1.0", GOOS: "linux", GOARCH: "amd64", Executable: exe,
	}
	return u, rel, exe
}

func TestCheck(t *testing.T) {
	u, _, _ := setup(t, nil)
	st, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Latest != "v0.2.0" || !st.Newer {
		t.Errorf("got %+v, want v0.2.0 newer", st)
	}
	u.Current = "v0.2.0"
	if st, _ := u.Check(context.Background()); st.Newer {
		t.Error("the same version is not newer")
	}
	u.Current = "dev"
	if st, _ := u.Check(context.Background()); st.Newer || st.Latest != "v0.2.0" {
		t.Errorf("dev build: got %+v", st)
	}
}

func TestLatestRejectsOddTags(t *testing.T) {
	for _, tag := range []string{"", "latest", "v1.2", "v1.2.3/../../x", "v1.2.3 ", "1.2.3"} {
		u, _, _ := setup(t, func(r *release) { r.tag = tag })
		if _, err := u.Latest(context.Background()); err == nil {
			t.Errorf("tag %q accepted", tag)
		}
	}
}

func TestApply(t *testing.T) {
	u, _, exe := setup(t, nil)
	got, err := u.Apply(context.Background(), "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Exe != exe || got.Signed != "" {
		t.Errorf("replaced %+v, want %s", got, exe)
	}
	b, _ := os.ReadFile(exe)
	if !bytes.Equal(b, fakeELF) {
		t.Error("binary not replaced")
	}
	fi, _ := os.Stat(exe)
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v, want 0755", fi.Mode().Perm())
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Error(".new file left behind")
	}
}

func TestReplacePreservesUnrelatedFile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sdash")
	if err := os.WriteFile(exe+".new", []byte("unrelated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replace(exe, fakeELF); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(exe + ".new"); err != nil || string(data) != "unrelated" {
		t.Fatalf("unrelated file changed: %q, %v", data, err)
	}
}

func TestReplaceFailureCleansStage(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sdash")
	if err := os.Mkdir(exe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replace(exe, fakeELF); err == nil {
		t.Fatal("replaced a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "sdash" {
		t.Fatalf("failed replacement left files: %v, %v", entries, err)
	}
}

func TestApplyCancellationPreservesBinary(t *testing.T) {
	u, _, exe := setup(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.Apply(ctx, "v0.2.0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "old binary" {
		t.Fatal("cancellation changed the existing binary")
	}
}

func TestPackagingUpdate(t *testing.T) {
	path := os.Getenv("SDASH_PACKAGING_BINARY")
	if path == "" {
		t.Skip("set SDASH_PACKAGING_BINARY to test a compiled release")
	}
	bin, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _, exe := setup(t, func(r *release) {
		name := ArchiveName(r.tag, "linux", runtime.GOARCH)
		archive := tarGz(t, map[string][]byte{"sdash": bin})
		r.files[name] = archive
		r.files["checksums.txt"] = []byte(sha(archive) + "  " + name + "\n")
	})
	u.GOARCH = runtime.GOARCH
	if err := os.WriteFile(exe, bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Apply(context.Background(), "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || !bytes.Equal(got, bin) {
		t.Fatalf("compiled binary changed: %v", err)
	}
	t.Logf("update without verifier: installed=%d B; peak file bytes=%d B (existing + staged binary; downloads are in memory)", len(bin), 2*len(bin))
}

// Every failure leaves the running binary exactly as it was.
func TestApplyFailuresKeepBinary(t *testing.T) {
	name := ArchiveName("v0.2.0", "linux", "amd64")
	cases := map[string]struct {
		mutate func(*release)
		setup  func(*Updater)
		want   string
		is     error
	}{
		"checksum mismatch": {mutate: func(r *release) {
			r.files[name] = append(r.files[name], 0)
		}, is: ErrChecksum},
		"no checksum line": {mutate: func(r *release) {
			r.files["checksums.txt"] = []byte("abc  other.tar.gz\n")
		}, want: "has no " + name},
		"no checksums file": {mutate: func(r *release) {
			delete(r.files, "checksums.txt")
		}, want: "404"},
		"missing archive": {mutate: func(r *release) {
			delete(r.files, name)
		}, want: "404"},
		"not a binary": {mutate: func(r *release) {
			a := tarGz(t, map[string][]byte{"sdash": []byte("#!/bin/sh\necho hi\n")})
			r.files[name] = a
			r.files["checksums.txt"] = []byte(sha(a) + "  " + name + "\n")
		}, want: "not a linux executable"},
		"binary in a subdirectory": {mutate: func(r *release) {
			a := tarGz(t, map[string][]byte{"x/sdash": fakeELF})
			r.files[name] = a
			r.files["checksums.txt"] = []byte(sha(a) + "  " + name + "\n")
		}, want: "no sdash in the archive"},
		"unsupported platform": {setup: func(u *Updater) { u.GOARCH = "riscv64" }, want: "unsupported platform"},
		"dev build":            {setup: func(u *Updater) { u.Current = "dev" }, is: ErrDevBuild},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			u, _, exe := setup(t, tc.mutate)
			if tc.setup != nil {
				tc.setup(u)
			}
			_, err := u.Apply(context.Background(), "v0.2.0")
			switch {
			case err == nil:
				t.Fatal("Apply succeeded")
			case tc.is != nil && !errors.Is(err, tc.is):
				t.Errorf("error %v, want %v", err, tc.is)
			case tc.want != "" && !strings.Contains(err.Error(), tc.want):
				t.Errorf("error %q, want it to mention %q", err, tc.want)
			}
			if b, _ := os.ReadFile(exe); string(b) != "old binary" {
				t.Error("the binary was changed")
			}
			if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
				t.Error(".new file left behind")
			}
		})
	}
}

func TestApplyRejectsBadTag(t *testing.T) {
	u, rel, _ := setup(t, nil)
	for _, tag := range []string{"latest", "../v0.2.0", "v0.2.0/../../etc"} {
		if _, err := u.Apply(context.Background(), tag); err == nil {
			t.Errorf("tag %q accepted", tag)
		}
	}
	if len(rel.requests) != 0 {
		t.Errorf("made requests %v for invalid tags", rel.requests)
	}
}

func TestApplyFollowsSymlink(t *testing.T) {
	u, _, exe := setup(t, nil)
	link := filepath.Join(t.TempDir(), "sdash")
	if err := os.Symlink(exe, link); err != nil {
		t.Skip(err)
	}
	u.Executable = link
	got, err := u.Apply(context.Background(), "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Exe != exe {
		t.Errorf("replaced %s, want the link target %s", got.Exe, exe)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced")
	}
}

func TestSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2*maxJSON))
	}))
	defer srv.Close()
	u := &Updater{Client: srv.Client(), API: srv.URL, Current: "v0.1.0"}
	if _, err := u.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("err = %v, want a size error", err)
	}
}

func TestCompare(t *testing.T) {
	ordered := []string{"v0.1.0-alpha", "v0.1.0-alpha.1", "v0.1.0-beta", "v0.1.0-beta.2", "v0.1.0-beta.11", "v0.1.0-rc.1", "v0.1.0", "v0.1.1", "v0.2.0", "v0.10.0", "v1.0.0"}
	for i := range ordered {
		for j := range ordered {
			want := cmpInt(i, j)
			if got := Compare(ordered[i], ordered[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
	if Compare("dev", "v0.1.0") != -1 || Compare("v0.1.0", "dev") != 1 || Compare("dev", "x") != 0 {
		t.Error("invalid tags must sort first")
	}
}

func TestFindChecksum(t *testing.T) {
	good := strings.Repeat("ab", 32)
	sums := []byte("garbage\n" + strings.Repeat("z", 64) + "  a.tar.gz\n" + good + " *b.tar.gz\n" + good + "  c.tar.gz extra\n")
	if _, ok := findChecksum(sums, "a.tar.gz"); ok {
		t.Error("non-hex checksum accepted")
	}
	if s, ok := findChecksum(sums, "b.tar.gz"); !ok || s != good {
		t.Errorf("binary-mode line: got %q %v", s, ok)
	}
	if _, ok := findChecksum(sums, "c.tar.gz"); ok {
		t.Error("malformed line accepted")
	}
}

func TestMethod(t *testing.T) {
	cases := []struct {
		exe, version, build, want string
	}{
		{"/home/u/.local/bin/sdash", "v0.1.0", "release", MethodRelease},
		{"/home/u/.local/bin/sdash", "v0.1.0", "", MethodRelease},
		{"/home/u/.local/bin/sdash", "v0.1.0", "make", MethodGit},
		{"/home/u/.local/bin/sdash", "v0.1.0-3-gabc1234", "", MethodGit},
		{"/home/u/.local/bin/sdash", "v0.1.0-dirty", "", MethodGit},
		{"/home/u/.local/bin/sdash", "3b05e96", "", MethodGit},
		{"/home/u/.local/bin/sdash", "dev", "", MethodDev},
	}
	for _, tc := range cases {
		if got, hint := Method(tc.exe, tc.version, tc.build); got != tc.want || hint == "" {
			t.Errorf("Method(%s, %s, %s) = %q %q, want %q", tc.exe, tc.version, tc.build, got, hint, tc.want)
		}
	}
}

func TestNoRelease(t *testing.T) {
	u, _, _ := setup(t, func(r *release) { r.tag = "" })
	u.API += "/missing"
	if _, err := u.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("err = %v, want ErrNoRelease", err)
	}
	if _, err := u.Check(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("check err = %v, want ErrNoRelease", err)
	}
}

func TestTargetRefusesCheckouts(t *testing.T) {
	u, _, _ := setup(t, nil)
	u.Build = "make"
	if _, err := u.Target(); !errors.Is(err, ErrGitCheckout) || !strings.Contains(err.Error(), "make update") {
		t.Errorf("git checkout: %v", err)
	}
}

func TestApplyChecksSignature(t *testing.T) {
	path := func(tools ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, t := range tools {
				if t == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	withBundle := func(r *release) { r.files["checksums.txt.sigstore.json"] = []byte(`{"bundle":1}`) }
	for _, c := range []struct {
		name    string
		mutate  func(*release)
		tools   []string
		respond execx.FakeResponse
		signed  string
		note    string
		err     error
	}{
		{"no tools", withBundle, nil, execx.FakeResponse{}, "", "install cosign or gh", nil},
		{"cosign ok", withBundle, []string{"cosign"}, execx.FakeResponse{}, SignedCosign, "", nil},
		{"cosign fails", withBundle, []string{"cosign", "gh"}, execx.FakeResponse{ExitCode: 1, Stderr: []byte("Error: none of the expected identities matched")}, "", "", ErrSignature},
		{"no bundle", nil, []string{"cosign"}, execx.FakeResponse{}, "", "no cosign signature", nil},
		{"gh ok", nil, []string{"gh"}, execx.FakeResponse{}, SignedGH, "", nil},
		{"gh logged out", nil, []string{"gh"}, execx.FakeResponse{ExitCode: 4, Stderr: []byte("To get started with GitHub CLI, please run:  gh auth login")}, "", "not logged in", nil},
		{"gh fails", nil, []string{"gh"}, execx.FakeResponse{ExitCode: 1, Stderr: []byte("verification failed: no attestations")}, "", "", ErrSignature},
	} {
		t.Run(c.name, func(t *testing.T) {
			u, _, exe := setup(t, c.mutate)
			fake := execx.NewFake()
			fake.Handler = func(_ context.Context, argv []string) (execx.Result, error) {
				if argv[0] != "cosign" && argv[0] != "gh" || execx.Classify(argv) != execx.ReadOnly {
					t.Errorf("unexpected command %q", argv)
				}
				res := execx.Result{Argv: argv, ExitCode: c.respond.ExitCode, Stderr: c.respond.Stderr}
				if c.respond.ExitCode != 0 {
					return res, &execx.ExitError{Name: argv[0], Code: c.respond.ExitCode, Stderr: string(c.respond.Stderr)}
				}
				return res, nil
			}
			u.Runner, u.LookPath = fake, path(c.tools...)
			got, err := u.Apply(context.Background(), "v0.2.0")
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("err = %v, want %v", err, c.err)
				}
				if b, _ := os.ReadFile(exe); string(b) != "old binary" {
					t.Fatal("a failed signature check changed the binary")
				}
				return
			}
			if err != nil || got.Signed != c.signed || !strings.Contains(got.Note, c.note) {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}
