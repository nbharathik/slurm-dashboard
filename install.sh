#!/bin/sh
# Install a verified release without root or a Go toolchain.
set -eu

REPO=nbharathik/slurm-dashboard
API_URL=${SDASH_API_URL:-https://api.github.com/repos/$REPO}
DOWNLOAD_URL=${SDASH_DOWNLOAD_URL:-https://github.com/$REPO/releases/download}
INSTALL_DIR=${SDASH_INSTALL_DIR:-${HOME:?HOME is not set}/.local/bin}
VERSION=
COMPLETIONS=

say() { printf '%s\n' "$*"; }
die() {
	printf 'sdash install: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'EOF'
Usage: install.sh [--version vX.Y.Z] [--dir DIR] [--completions bash]

Downloads an sdash release, verifies its SHA-256 checksum and installs the
binary into DIR (default: $SDASH_INSTALL_DIR or ~/.local/bin). No sudo.
EOF
}

while [ $# -gt 0 ]; do
	case $1 in
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		VERSION=$2
		shift 2
		;;
	--version=*)
		VERSION=${1#*=}
		shift
		;;
	--dir)
		[ $# -ge 2 ] || die "--dir needs a value"
		INSTALL_DIR=$2
		shift 2
		;;
	--dir=*)
		INSTALL_DIR=${1#*=}
		shift
		;;
	--completions)
		[ $# -ge 2 ] || die "--completions needs a value"
		COMPLETIONS=$2
		[ "$COMPLETIONS" = bash ] || die "supported completion: bash"
		shift 2
		;;
	--completions=*)
		COMPLETIONS=${1#*=}
		[ "$COMPLETIONS" = bash ] || die "supported completion: bash"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		die "unknown option: $1"
		;;
	esac
done
[ -n "$INSTALL_DIR" ] || die "install directory must not be empty"

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported operating system: $(uname -s) (Linux and macOS only)" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m) (x86_64 and arm64 only)" ;;
esac
if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
	die "Intel Macs are not supported by the release builds; build from source with 'make build'"
fi

if command -v curl >/dev/null 2>&1; then
	fetcher=curl
elif command -v wget >/dev/null 2>&1; then
	fetcher=wget
else
	die "needs curl or wget"
fi

fetch() {
	if [ "$fetcher" = curl ]; then
		curl -fsSL --retry 2 -o "$2" "$1"
	else
		wget -q -O "$2" "$1"
	fi
}

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
	die "needs sha256sum or shasum to verify the download"
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/sdash-install.XXXXXX")
new=
comp_new=
cleanup() {
	[ -z "$new" ] || rm -f -- "$new"
	[ -z "$comp_new" ] || rm -f -- "$comp_new"
	rm -rf -- "$tmp"
}
trap cleanup EXIT
trap 'exit 1' HUP INT PIPE TERM

if [ -z "$VERSION" ]; then
	if ! fetch "$API_URL/releases/latest" "$tmp/latest.json"; then
		if [ "$fetcher" = curl ] && [ "$(curl -s -o /dev/null -w '%{http_code}' "$API_URL/releases/latest")" = 404 ]; then
			die "no release has been published yet; install from source instead: git clone https://github.com/$REPO && cd slurm-dashboard && make build install"
		fi
		die "could not ask $API_URL for the latest release (offline? see the offline install in the README)"
	fi
	VERSION=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -n 1)
	[ -n "$VERSION" ] || die "could not find the latest release"
fi
case $VERSION in
v*) ;;
*) VERSION=v$VERSION ;;
esac
# The tag goes into URLs and file names: digits, letters, dots and dashes only.
case $VERSION in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) die "invalid version: $VERSION" ;;
esac
case ${VERSION#v} in
*[!0-9A-Za-z.-]*) die "invalid version: $VERSION" ;;
esac

archive=sdash_${VERSION#v}_${os}_${arch}.tar.gz
say "Downloading sdash $VERSION for $os/$arch..."
fetch "$DOWNLOAD_URL/$VERSION/checksums.txt" "$tmp/checksums.txt" || die "could not download checksums.txt for $VERSION"
fetch "$DOWNLOAD_URL/$VERSION/$archive" "$tmp/$archive" || die "could not download $archive"

expected=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/checksums.txt")
[ -n "$expected" ] || die "checksums.txt has no entry for $archive"
actual=$(sha256 "$tmp/$archive")
if [ "$actual" != "$expected" ]; then
	die "checksum mismatch for $archive (expected $expected, got $actual); nothing was installed"
fi
say "Checksum verified."

# Bind optional signature verification to this repository's release workflow.
signer="^https://github.com/$REPO/\.github/workflows/release\.yml@refs/tags/v"
if command -v cosign >/dev/null 2>&1; then
	if fetch "$DOWNLOAD_URL/$VERSION/checksums.txt.sigstore.json" "$tmp/checksums.txt.sigstore.json"; then
		cosign verify-blob --bundle "$tmp/checksums.txt.sigstore.json" \
			--certificate-identity-regexp "$signer" \
			--certificate-oidc-issuer https://token.actions.githubusercontent.com \
			"$tmp/checksums.txt" >/dev/null 2>&1 ||
			die "the signature of checksums.txt does not verify with cosign; nothing was installed"
		say "Signature verified with cosign."
	else
		die "could not download the cosign signature; nothing was installed"
	fi
elif command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
	gh attestation verify "$tmp/$archive" --repo "$REPO" >/dev/null 2>&1 ||
		die "gh attestation verify failed for $archive; nothing was installed"
	say "Build provenance verified with gh."
else
	say "SHA-256 verified; signature verification needs an existing cosign or authenticated gh."
fi

mkdir -p "$INSTALL_DIR"
[ ! -d "$INSTALL_DIR/sdash" ] || die "destination is a directory"
new=$(mktemp "$INSTALL_DIR/.sdash-install.XXXXXX")
# Extract only the binary, next to its destination for an atomic rename.
tar -xOzf "$tmp/$archive" sdash > "$new" || die "could not extract sdash"
[ -s "$new" ] || die "archive contains no regular sdash binary"
chmod 755 "$new"
binary_size=$(wc -c < "$new" | tr -d ' ')
installed_size=$binary_size

if [ "$COMPLETIONS" = bash ]; then
	comp_dir=${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion/completions
	mkdir -p "$comp_dir"
	[ "$(cd "$comp_dir" && pwd -P)" != "$(cd "$INSTALL_DIR" && pwd -P)" ] || die "binary and completion directories must differ"
	[ ! -d "$comp_dir/sdash" ] || die "completion destination is a directory"
	comp_new=$(mktemp "$comp_dir/.sdash-install.XXXXXX")
	tar -xOzf "$tmp/$archive" completions/sdash.bash > "$comp_new" || die "could not extract bash completion"
	[ -s "$comp_new" ] || die "archive contains no bash completion"
	chmod 644 "$comp_new"
	installed_size=$((binary_size + $(wc -c < "$comp_new")))
fi
[ "$installed_size" -lt 20971520 ] || die "installed application must be smaller than 20 MiB"
if [ -n "$comp_new" ]; then
	mv -f "$comp_new" "$comp_dir/sdash"
	comp_new=
	say "Installed bash completion to $comp_dir/sdash"
fi
[ ! -d "$INSTALL_DIR/sdash" ] || die "destination is a directory"
mv -f "$new" "$INSTALL_DIR/sdash"
new=
say "Installed sdash $VERSION to $INSTALL_DIR/sdash"
awk -v n="$installed_size" 'BEGIN { printf "Installed size: %d bytes (%.2f MiB)\n", n, n/1048576 }'

case :${PATH:-}: in
*:"$INSTALL_DIR":*) ;;
*)
	# Show the directory as $HOME/... so the line works when pasted.
	case $INSTALL_DIR in
	"$HOME"/*) shown="\$HOME/${INSTALL_DIR#"$HOME"/}" ;;
	*) shown=$INSTALL_DIR ;;
	esac
	say ""
	say "$INSTALL_DIR is not on your PATH. Add this line to ~/.bashrc (or ~/.zshrc):"
	say "  export PATH=\"$shown:\$PATH\""
	;;
esac

say ""
say "Next: run 'sdash doctor' to check your cluster setup, then 'sdash'."
