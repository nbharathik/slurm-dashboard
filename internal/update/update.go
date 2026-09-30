// Package update implements "sdash update": it finds the latest GitHub
// release, downloads the archive for this platform, verifies it against the
// release's checksums.txt (and its signature, when cosign or gh is
// installed) and replaces the running binary.
//
// Release checks run only when requested; Slurm clients and configured
// hooks may also use the network.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

// Default release locations.
const (
	DefaultAPI      = "https://api.github.com/repos/" + meta.Repo
	DefaultDownload = "https://github.com/" + meta.Repo + "/releases/download"
)

// Bound downloads and extracted data from release servers.
const (
	maxJSON     = 1 << 20
	maxChecksum = 1 << 20
	maxArchive  = 200 << 20
	maxBinary   = 200 << 20
)

// Errors callers can match.
var (
	// ErrDevBuild means the running binary has no release version.
	ErrDevBuild = errors.New("development build")
	// ErrChecksum means the download did not match checksums.txt.
	ErrChecksum = errors.New("checksum mismatch")
	// ErrNoRelease means the repository has no published release yet.
	ErrNoRelease = errors.New("no release published yet")
	// ErrGitCheckout means the binary was built from a git checkout.
	ErrGitCheckout = errors.New("built from a git checkout")
)

// gitDescribe matches versions from "git describe --tags --always --dirty"
// that are not a release: commits after a tag, or a modified tree.
var gitDescribe = regexp.MustCompile(`-\d+-g[0-9a-f]{7,}(-dirty)?$|-dirty$|^[0-9a-f]{7,}$`)

// tagPattern accepts release tags such as v0.1.0 and v0.2.0-rc.1. Tags go
// into URLs and file names, so nothing else is accepted.
var tagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?$`)

// Updater checks for and installs releases.
type Updater struct {
	Client   *http.Client
	API      string // e.g. DefaultAPI
	Download string // e.g. DefaultDownload
	Current  string // the running version, e.g. meta.Version
	GOOS     string
	GOARCH   string
	// Executable is the binary to replace; empty means os.Executable.
	Executable string
	// Build is how the running binary was built (meta.Build).
	Build string
	// Runner and LookPath check the release signature with cosign or gh
	// when either is installed; a nil Runner skips the check.
	Runner   execx.Runner
	LookPath func(string) (string, error)
}

// Result is what Apply installed.
type Result struct {
	Exe string
	// Signed says how the release was verified beyond its checksum:
	// SignedCosign, SignedGH or "" (then Note says why not).
	Signed string
	Note   string
}

// New returns an Updater for the running binary.
func New(goos, goarch string) *Updater {
	return &Updater{
		Client:   &http.Client{Timeout: 2 * time.Minute},
		API:      DefaultAPI,
		Download: DefaultDownload,
		Current:  meta.Version,
		Build:    meta.Build,
		GOOS:     goos,
		GOARCH:   goarch,
		Runner:   execx.NewReal(execx.Options{DefaultTimeout: 2 * time.Minute}),
		LookPath: execx.LookPath,
	}
}

// Status is the result of a check.
type Status struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Newer   bool   `json:"update_available"`
}

// Check compares the running version with the latest release.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	latest, err := u.Latest(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{Current: u.Current, Latest: latest}
	if tagPattern.MatchString(u.Current) {
		st.Newer = Compare(latest, u.Current) > 0
	}
	return st, nil
}

// Latest returns the tag of the latest release.
func (u *Updater) Latest(ctx context.Context) (string, error) {
	body, err := u.get(ctx, strings.TrimRight(u.API, "/")+"/releases/latest", maxJSON)
	var he httpError
	if errors.As(err, &he) && he.status == http.StatusNotFound {
		return "", ErrNoRelease
	}
	if err != nil {
		return "", fmt.Errorf("latest release: %w", err)
	}
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", fmt.Errorf("latest release: %w", err)
	}
	if !tagPattern.MatchString(rel.Tag) {
		return "", fmt.Errorf("latest release: unexpected tag %q", rel.Tag)
	}
	return rel.Tag, nil
}

// ArchiveName is the release archive for a version and platform, matching
// .goreleaser.yaml.
func ArchiveName(tag, goos, goarch string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", meta.AppName, strings.TrimPrefix(tag, "v"), goos, goarch)
}

// Apply installs release tag over the running binary: it downloads the
// archive and checksums.txt, verifies the SHA-256 and, when cosign or gh
// is installed, the release signature; then it stages the binary
// next to the destination and renames it into place. The running binary
// is untouched if anything fails.
func (u *Updater) Apply(ctx context.Context, tag string) (Result, error) {
	if !tagPattern.MatchString(tag) {
		return Result{}, fmt.Errorf("invalid version %q (expected e.g. v0.1.0)", tag)
	}
	exe, err := u.Target()
	if err != nil {
		return Result{}, err
	}
	name := ArchiveName(tag, u.GOOS, u.GOARCH)
	base := strings.TrimRight(u.Download, "/") + "/" + tag + "/"
	sums, err := u.get(ctx, base+"checksums.txt", maxChecksum)
	if err != nil {
		return Result{}, fmt.Errorf("checksums: %w", err)
	}
	want, ok := findChecksum(sums, name)
	if !ok {
		return Result{}, fmt.Errorf("release %s has no %s (unsupported platform %s/%s?)", tag, name, u.GOOS, u.GOARCH)
	}
	archive, err := u.get(ctx, base+name, maxArchive)
	if err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return Result{}, fmt.Errorf("%s: %w (expected %s, got %s); nothing was changed", name, ErrChecksum, want, got)
	}
	signed, note, err := u.verify(ctx, base, sums, archive, name)
	if err != nil {
		return Result{}, err
	}
	bin, err := extract(archive, meta.AppName)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", name, err)
	}
	if !executableFor(bin, u.GOOS) {
		return Result{}, fmt.Errorf("%s: the binary is not a %s executable", name, u.GOOS)
	}
	return Result{Exe: exe, Signed: signed, Note: note}, replace(exe, bin)
}

// Install methods reported by Method.
const (
	MethodRelease = "release"
	MethodGit     = "git checkout"
	MethodDev     = "development build"
)

// Method says how the binary at exe was installed, given its version and
// build stamp, and the command that updates it.
func Method(_, version, build string) (method, hint string) {
	switch {
	case build == "make" || gitDescribe.MatchString(version):
		return MethodGit, "make update (in your clone)"
	case !tagPattern.MatchString(version):
		return MethodDev, "make update in your clone, or the install script"
	}
	return MethodRelease, meta.AppName + " update"
}

// Target resolves the binary Apply would replace. Only release installs
// can update themselves: development builds and git checkouts are refused
// with the command that updates them instead.
func (u *Updater) Target() (string, error) {
	exe := u.Executable
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return "", err
		}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	switch m, hint := Method(exe, u.Current, u.Build); m {
	case MethodRelease:
		return exe, nil
	case MethodGit:
		return "", fmt.Errorf("%w; update it with: %s", ErrGitCheckout, hint)
	default:
		return "", fmt.Errorf("%w (%s): %s", ErrDevBuild, u.Current, hint)
	}
}

// get fetches url, failing on a non-200 status or a body over limit bytes.
func (u *Updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", meta.AppName+"/"+u.Current)
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, httpError{url: url, status: resp.StatusCode, text: resp.Status}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("GET %s: response larger than %d bytes", url, limit)
	}
	return body, nil
}

// findChecksum returns the SHA-256 for name from a sha256sum-style file.
func findChecksum(sums []byte, name string) (string, bool) {
	for line := range strings.SplitSeq(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			if _, err := hex.DecodeString(f[0]); err == nil {
				return strings.ToLower(f[0]), true
			}
		}
	}
	return "", false
}

// extract returns the regular file called name at the top of a .tar.gz.
func extract(archive []byte, name string) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", name)
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimPrefix(h.Name, "./") != name || h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Size > maxBinary {
			return nil, fmt.Errorf("%s is larger than %d bytes", name, maxBinary)
		}
		return io.ReadAll(io.LimitReader(tr, maxBinary))
	}
}

// executableFor checks the file magic, so a broken archive never replaces
// a working binary.
func executableFor(bin []byte, goos string) bool {
	switch goos {
	case "linux":
		return bytes.HasPrefix(bin, []byte("\x7fELF"))
	case "darwin":
		return bytes.HasPrefix(bin, []byte{0xcf, 0xfa, 0xed, 0xfe}) || bytes.HasPrefix(bin, []byte{0xca, 0xfe, 0xba, 0xbe})
	}
	return false
}

// replace stages beside exe for an atomic rename, preserving permissions.
func replace(exe string, bin []byte) error {
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".sdash-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", exe, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	_, werr := f.Write(bin)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		return err
	}
	return nil
}

// Compare orders two release tags like semver: -1, 0 or 1. A pre-release
// sorts before its release. Invalid tags sort first.
func Compare(a, b string) int {
	ma, mb := tagPattern.FindStringSubmatch(a), tagPattern.FindStringSubmatch(b)
	switch {
	case ma == nil && mb == nil:
		return 0
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return comparePre(strings.TrimPrefix(ma[4], "-"), strings.TrimPrefix(mb[4], "-"))
}

// comparePre compares pre-release strings per semver section 11.
func comparePre(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if c := compareIdent(pa[i], pb[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	return 0
}

func compareIdent(a, b string) int {
	x, errA := strconv.Atoi(a)
	y, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return cmpInt(x, y)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// httpError is a non-200 response.
type httpError struct {
	url    string
	status int
	text   string
}

func (e httpError) Error() string { return "GET " + e.url + ": " + e.text }
