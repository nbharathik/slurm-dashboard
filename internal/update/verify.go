package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
	"github.com/nbharathik/slurm-dashboard/internal/meta"
)

// How a release was verified beyond its checksum.
const (
	SignedCosign = "cosign"
	SignedGH     = "gh"
)

// ErrSignature means the release's signature or provenance did not verify.
var ErrSignature = errors.New("signature check failed")

// maxBundle bounds the size of checksums.txt.sigstore.json.
const maxBundle = 1 << 20

// Signer is the certificate identity of this repository's release
// workflow, which signs checksums.txt.
var Signer = "^https://github.com/" + regexp.QuoteMeta(meta.Repo) + `/\.github/workflows/release\.yml@refs/tags/v`

// CosignArgv checks the cosign bundle of checksums.txt.
func CosignArgv(bundle, checksums string) []string {
	return []string{
		"cosign", "verify-blob", "--bundle", bundle, "--certificate-identity-regexp", Signer,
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com", checksums,
	}
}

// GHArgv checks an archive's build provenance attestation.
func GHArgv(archive string) []string {
	return []string{"gh", "attestation", "verify", archive, "--repo", meta.Repo}
}

// verify checks the release's signature with cosign, or its build
// provenance with gh, whichever is installed. It returns how it verified,
// and a note when it could not.
func (u *Updater) verify(ctx context.Context, base string, sums, archive []byte, name string) (how, note string, err error) {
	if u.Runner == nil || u.LookPath == nil {
		return "", "", nil
	}
	_, cosignErr := u.LookPath("cosign")
	_, ghErr := u.LookPath("gh")
	if cosignErr != nil && ghErr != nil {
		return "", "install cosign or gh to also check the release signature", nil
	}
	dir, err := os.MkdirTemp("", "sdash-verify-")
	if err != nil {
		return "", "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	write := func(file string, b []byte) (string, error) {
		p := filepath.Join(dir, file)
		return p, os.WriteFile(p, b, 0o600)
	}
	sumsPath, err := write("checksums.txt", sums)
	if err != nil {
		return "", "", err
	}
	if cosignErr == nil {
		bundle, err := u.get(ctx, base+"checksums.txt.sigstore.json", maxBundle)
		var he httpError
		switch {
		case errors.As(err, &he) && he.status == http.StatusNotFound:
			if ghErr != nil {
				return "", "this release has no cosign signature", nil
			}
		case err != nil:
			return "", "", fmt.Errorf("signature: %w", err)
		default:
			bundlePath, err := write("checksums.txt.sigstore.json", bundle)
			if err != nil {
				return "", "", err
			}
			if _, err := u.Runner.Run(execx.WithLabel(ctx, "verify-cosign"), CosignArgv(bundlePath, sumsPath)...); err != nil {
				return "", "", fmt.Errorf("%w: cosign: %v; nothing was changed", ErrSignature, execx.FirstLine([]byte(err.Error())))
			}
			return SignedCosign, "", nil
		}
	}
	archivePath, err := write(name, archive)
	if err != nil {
		return "", "", err
	}
	res, err := u.Runner.Run(execx.WithLabel(ctx, "verify-gh"), GHArgv(archivePath)...)
	if err != nil {
		msg := strings.ToLower(string(res.Stderr) + " " + err.Error())
		if strings.Contains(msg, "gh auth login") || strings.Contains(msg, "authenticat") {
			return "", "gh is not logged in, so the build provenance was not checked", nil
		}
		return "", "", fmt.Errorf("%w: gh attestation verify: %v; nothing was changed", ErrSignature, execx.FirstLine([]byte(err.Error())))
	}
	return SignedGH, "", nil
}
