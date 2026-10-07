# Install

Install a published release:

```sh
curl -fsSL https://raw.githubusercontent.com/nbharathik/slurm-dashboard/main/install.sh | sh
```

Installs only `~/.local/bin/sdash`. No root, Go compiler, module cache or source
checkout is needed. Install once on a login node or shared home directory;
compute nodes need no installation. Follow the printed PATH instruction if needed.
The installer does not edit shell startup files or install verification tools.

Options can be combined:

```sh
curl -fsSL https://raw.githubusercontent.com/nbharathik/slurm-dashboard/main/install.sh |
  sh -s -- --version v0.1.0 --dir "$HOME/bin" --completions bash
```

`SDASH_INSTALL_DIR` changes the default directory. Mirrors can set
`SDASH_API_URL` (repository API base) and `SDASH_DOWNLOAD_URL` (release download
base). Downloads require `curl` or `wget`, `tar`, and `sha256sum` or `shasum`.
SHA-256 is always checked; existing `cosign` or authenticated `gh` adds signature
or provenance verification. See [security](../SECURITY.md).

## Compatibility

Release platforms: Linux amd64/arm64 (kernel 3.2+) and macOS arm64 (macOS 12+),
following the [Go 1.26 platform requirements](https://go.dev/wiki/MinimumRequirements).
Linux releases have no dynamic library dependencies.

Use the site's configured Slurm clients on `PATH`; try `sdash doctor`.
The parsers are tested with **Slurm 23.11** output. Other versions and site
customisations can use different formats. Missing optional accounting or priority
support disables the affected views. macOS also needs configured Slurm clients;
`sdash --demo` works without them.

## Footprint

The installed executable, including optional bash completion, takes less than
**20 MiB**. Download archives require additional temporary space.

Installation temporarily holds the archive and staged executable; upgrades also
retain the current executable until replacement. Temporary files are removed on
success, failure and catchable interruption. Filesystem allocation and signature
tools can add overhead, so temporary space is not a fixed guarantee.

Runtime cache (`~/.cache/sdash`) and state (`~/.local/state/sdash`) are separate
from the application budget; XDG overrides apply. Their size depends on use.
Requested recordings and user job logs are outside this budget. Installation
does not remove existing user data or toolchains.

## Offline, update and remove

Copy the matching archive and `checksums.txt` to the offline host.
Verify and stage the binary before replacing it (change the archive name):

```sh
(
  set -eu
  archive=sdash_0.1.0_linux_amd64.tar.gz
  expected=$(awk -v f="$archive" '$2 == f { print $1 }' checksums.txt)
  test -n "$expected"
  printf '%s  %s\n' "$expected" "$archive" | sha256sum -c -
  mkdir -p "$HOME/.local/bin"
  staged=$(mktemp "$HOME/.local/bin/.sdash-install.XXXXXX")
  trap 'rm -f "$staged"' EXIT
  trap 'exit 1' HUP INT TERM
  tar -xOzf "$archive" sdash > "$staged"
  test -s "$staged"
  chmod 755 "$staged"
  test ! -d "$HOME/.local/bin/sdash"
  mv -f "$staged" "$HOME/.local/bin/sdash"
)
```

On macOS use `shasum -a 256 -c -`. Remove the copied download files afterward.
Connected release installations update with `sdash update`; `sdash update --check`
only checks availability. Updates keep no downloaded archives or backup binaries.
Offline installations can repeat the verified steps above for a newer release.

Remove the default installation with `rm "$HOME/.local/bin/sdash"`. If installed,
remove bash completion at `${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions/sdash`.
Config, cache, state, recordings and job logs are retained.

## Source builds

To build from source, use the Go toolchain specified in `go.mod` and Make.
Run `make install` in the checkout. `PREFIX=/opt/sdash` changes the prefix. `make update` pulls and
reinstalls; `make uninstall` removes the binary and bash completion.
