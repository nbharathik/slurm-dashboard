package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nbharathik/slurm-dashboard/internal/meta"
	"github.com/nbharathik/slurm-dashboard/internal/update"
)

func newUpdateCmd(a *app) *cobra.Command {
	var check, asJSON bool
	var version string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update sdash to the latest release from GitHub",
		Long: `Download the latest release from GitHub, verify its SHA-256 checksum
against the release's checksums.txt, and replace this binary. Nothing is
changed if the download or the check fails.

Source installations update with make update from their checkout.`,
		Example: `  sdash update --check
  sdash update
  sdash update --version v0.1.0`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if check && version != "" {
				return usageError{fmt.Errorf("--check and --version cannot be combined")}
			}
			if asJSON && !check {
				return usageError{fmt.Errorf("--json needs --check")}
			}
			u := a.updater
			if u == nil {
				u = update.New(runtime.GOOS, runtime.GOARCH)
			}
			ctx, out := cmd.Context(), cmd.OutOrStdout()
			noRelease := func() error {
				_, err := fmt.Fprintf(out, "No %s release has been published yet. From a git checkout, update with: make update\n", meta.AppName)
				return err
			}
			if check {
				st, err := u.Check(ctx)
				if errors.Is(err, update.ErrNoRelease) {
					return noRelease()
				}
				if err != nil {
					return err
				}
				if asJSON {
					enc := json.NewEncoder(out)
					enc.SetIndent("", "  ")
					return enc.Encode(struct {
						Schema int `json:"schema"`
						update.Status
					}{jsonSchema, st})
				}
				switch {
				case st.Newer:
					_, err = fmt.Fprintf(out, "%s %s is available (you have %s). Run: %s\n", meta.AppName, st.Latest, st.Current, upgradeHint(u))
				case update.Compare(st.Current, st.Latest) >= 0:
					_, err = fmt.Fprintf(out, "%s %s is up to date (latest release: %s).\n", meta.AppName, st.Current, st.Latest)
				default:
					_, err = fmt.Fprintf(out, "The latest release is %s; this is %s.\n", st.Latest, st.Current)
				}
				return err
			}
			// Refuse before touching the network.
			if _, err := u.Target(); err != nil {
				return err
			}
			tag := version
			if tag != "" && !strings.HasPrefix(tag, "v") {
				tag = "v" + tag
			}
			if tag == "" {
				st, err := u.Check(ctx)
				if errors.Is(err, update.ErrNoRelease) {
					return noRelease()
				}
				if err != nil {
					return err
				}
				// Never downgrade without --version.
				if update.Compare(st.Current, st.Latest) >= 0 {
					_, err = fmt.Fprintf(out, "%s %s is up to date (latest release: %s).\n", meta.AppName, st.Current, st.Latest)
					return err
				}
				tag = st.Latest
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Downloading %s %s for %s/%s...\n", meta.AppName, tag, u.GOOS, u.GOARCH)
			res, err := u.Apply(ctx, tag)
			if err != nil {
				return err
			}
			a.log.Info("updated", "from", u.Current, "to", tag, "path", res.Exe, "signed", res.Signed)
			how := "checksum verified"
			switch res.Signed {
			case update.SignedCosign:
				how = "checksum and cosign signature verified"
			case update.SignedGH:
				how = "checksum and build provenance verified with gh"
			}
			if res.Note != "" {
				how += "; " + res.Note
			}
			_, err = fmt.Fprintf(out, "Updated %s from %s to %s (%s).\n", res.Exe, u.Current, tag, how)
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&check, "check", false, "only report whether a newer release exists")
	f.StringVar(&version, "version", "", "install this release `tag` instead of the latest (e.g. v0.1.0)")
	f.BoolVar(&asJSON, "json", false, "with --check, print JSON")
	return cmd
}

// upgradeHint is the command that upgrades this copy of sdash.
func upgradeHint(u *update.Updater) string {
	exe := u.Executable
	if exe == "" {
		exe, _ = os.Executable()
	}
	_, hint := update.Method(exe, u.Current, u.Build)
	return hint
}
