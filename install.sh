#!/bin/sh
#
# Install luatdo on Linux or macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/tamnd/luatdo/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/tamnd/luatdo/main/install.sh | sh -s -- --with-data
#
# The second form downloads the published graph and brings up a local Neo4j over
# it, which needs podman or docker and about 20GB of disk.
#
# Written for POSIX sh rather than bash, because the shell a person pipes this
# into is dash on Debian and Ubuntu and there is nothing here worth a bashism.

set -eu

REPO=tamnd/luatdo
VERSION=${LUATDO_VERSION:-}
BIN_DIR=${LUATDO_BIN_DIR:-}
WITH_DATA=${LUATDO_WITH_DATA:-}

for arg in "$@"; do
	case $arg in
	--with-data) WITH_DATA=1 ;;
	--version=*) VERSION=${arg#--version=} ;;
	--bin-dir=*) BIN_DIR=${arg#--bin-dir=} ;;
	-h | --help)
		echo "usage: install.sh [--with-data] [--version=vX.Y.Z] [--bin-dir=DIR]"
		exit 0
		;;
	*)
		echo "install.sh: unknown option $arg" >&2
		exit 2
		;;
	esac
done

say() { printf '%s\n' "$*"; }
die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}
have() { command -v "$1" >/dev/null 2>&1; }

# One of curl or wget is enough, and every machine this has to run on has at
# least one. Which one it is changes the flags for everything below, so it is
# settled once here.
if have curl; then
	get() { curl -fsSL "$1" -o "$2"; }
	read_url() { curl -fsSL "$1"; }
elif have wget; then
	get() { wget -qO "$2" "$1"; }
	read_url() { wget -qO - "$1"; }
else
	die "neither curl nor wget is installed"
fi

os=$(uname -s)
case $os in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "$os is not a platform this releases for, build from source with go install github.com/tamnd/luatdo/cmd/luatdo@latest" ;;
esac

arch=$(uname -m)
case $arch in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "$arch is not a platform this releases for, build from source with go install github.com/tamnd/luatdo/cmd/luatdo@latest" ;;
esac

# The release page redirects to the newest tag, which answers the version
# question without spending one of the sixty unauthenticated API calls an
# address gets in an hour. A machine behind a busy NAT would otherwise fail here
# for reasons that have nothing to do with the install.
if [ -z "$VERSION" ]; then
	if have curl; then
		VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')
	else
		VERSION=$(read_url "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
	fi
fi
[ -n "$VERSION" ] || die "could not work out the latest version, pass --version=vX.Y.Z"

# The tag carries a leading v and the archive name does not.
number=${VERSION#v}
archive="luatdo_${number}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"

if [ -z "$BIN_DIR" ]; then
	BIN_DIR=$HOME/.local/bin
fi

tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM

say "installing luatdo $VERSION for $os/$arch"
get "$base/$archive" "$tmp/$archive" || die "no $archive in release $VERSION"
get "$base/checksums.txt" "$tmp/checksums.txt" || die "release $VERSION publishes no checksums"

# A binary downloaded over the network and then run is worth thirty two bytes of
# checking. Both tools are named because macOS ships shasum and Linux ships
# sha256sum, and neither ships the other.
if have sha256sum; then
	sum=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
elif have shasum; then
	sum=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
else
	die "neither sha256sum nor shasum is installed, so the download cannot be checked"
fi
want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$want" ] || die "$archive is not listed in checksums.txt"
[ "$sum" = "$want" ] || die "$archive has checksum $sum and the release says $want"

tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/luatdo" ] || die "$archive does not hold a luatdo binary"

mkdir -p "$BIN_DIR" || die "cannot create $BIN_DIR"
# Written next to the destination and then moved, so that upgrading while a long
# run is in flight replaces the name rather than the running image.
cp "$tmp/luatdo" "$BIN_DIR/.luatdo.new"
chmod 0755 "$BIN_DIR/.luatdo.new"
mv "$BIN_DIR/.luatdo.new" "$BIN_DIR/luatdo" || die "cannot write to $BIN_DIR"

say "installed $BIN_DIR/luatdo"
"$BIN_DIR/luatdo" version || true

case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*)
	say ""
	say "$BIN_DIR is not on your PATH. Add this to your shell profile:"
	say "  export PATH=\"$BIN_DIR:\$PATH\""
	;;
esac

if [ -z "$WITH_DATA" ]; then
	say ""
	say "to get the graph and a local Neo4j over it, run:"
	say "  luatdo neo4j install"
	exit 0
fi

if ! have podman && ! have docker; then
	die "--with-data needs podman or docker and neither is installed, the binary is in place so install one and run luatdo neo4j install"
fi

say ""
say "fetching the published graph and loading it, which takes a while"
exec "$BIN_DIR/luatdo" neo4j install
