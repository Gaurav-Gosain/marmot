#!/bin/sh
# Install a built marmot (after `make`) into PREFIX and link the command into BINDIR.
#
#   PREFIX  install directory (default ~/.marmot)
#   BINDIR  directory on PATH for the marmot command (default ~/.local/bin)
#   LLVM_BIN, LLVM_PREFIX  the LLVM 22 marmot was built with (default: llvm-config 22 on PATH)
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
# Record the LLVM 22 marmot was built with: marmot runs clang and the other LLVM tools from there
# (after LLVM_BIN, before Homebrew and PATH). No record when none is known.
llvm=
if [ -n "${LLVM_BIN:-}" ]; then
	llvm=$LLVM_BIN
elif [ -n "${LLVM_PREFIX:-}" ]; then
	llvm=$LLVM_PREFIX/bin
else
	for c in llvm-config-22 llvm-config; do
		if command -v "$c" >/dev/null 2>&1 && "$c" --version 2>/dev/null | grep -q '^22\.'; then
			llvm=$("$c" --bindir)
			break
		fi
	done
fi
if [ -n "$llvm" ] && [ -x "$llvm/clang" ]; then
	(cd "$llvm" && pwd -P) > "$tmp/build/llvm-bin"
fi
# Keep the build cache of an earlier install.
if [ -d "$PREFIX/build/cache" ]; then mv "$PREFIX/build/cache" "$tmp/build/cache"; fi
rm -rf "$PREFIX"
mv "$tmp" "$PREFIX"

mkdir -p "$BINDIR"
ln -sf "$PREFIX/build/bin/marmot" "$BINDIR/marmot"
echo "installed $(MARMOT_OUTPUT=plain "$BINDIR/marmot" version 2>/dev/null || echo marmot) in $PREFIX, command at $BINDIR/marmot"
if [ -f "$PREFIX/build/llvm-bin" ]; then echo "LLVM: $(cat "$PREFIX/build/llvm-bin")"; fi
