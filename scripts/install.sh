#!/bin/sh
# Install a built marmot (after `make`) into PREFIX and link the command into BINDIR.
#
#   PREFIX  install directory (default ~/.marmot)
#   BINDIR  directory on PATH for the marmot command (default ~/.local/bin)
#
# The install is a copy, so rebuilding the source tree does not change it.
set -eu
cd "$(dirname "$0")/.."
PREFIX=${PREFIX:-$HOME/.marmot}
BINDIR=${BINDIR:-$HOME/.local/bin}

for f in build/bin/marmot build/bin/marmot-backend build/lib lib/src; do
	[ -e "$f" ] || { echo "install: $f missing, run make first" >&2; exit 1; }
done

tmp="$PREFIX.new.$$"
rm -rf "$tmp"
mkdir -p "$tmp/build"
cp -R lib runtime "$tmp/"
cp -R build/bin build/lib "$tmp/build/"
# Keep the build cache of an earlier install.
if [ -d "$PREFIX/build/cache" ]; then mv "$PREFIX/build/cache" "$tmp/build/cache"; fi
rm -rf "$PREFIX"
mv "$tmp" "$PREFIX"

mkdir -p "$BINDIR"
ln -sf "$PREFIX/build/bin/marmot" "$BINDIR/marmot"
echo "installed $(MARMOT_OUTPUT=plain "$BINDIR/marmot" version 2>/dev/null || echo marmot) in $PREFIX, command at $BINDIR/marmot"
