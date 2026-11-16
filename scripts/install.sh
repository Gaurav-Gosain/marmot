#!/bin/sh
# Install a built marmot (after `make`) into PREFIX and link the command into BINDIR.
#
#   PREFIX  install directory (default ~/.marmot)
#   BINDIR  directory on PATH for the marmot command (default ~/.local/bin)
#   LLVM_BIN, LLVM_PREFIX  the LLVM 22 marmot was built with (default: llvm-config 22 on PATH)
#   LLVM_RELEASE=1  marmot was built against the official LLVM release
#                   (scripts/llvm-release.sh): the tools marmot runs (clang,
#                   lld, llvm-objcopy, ...) are copied into PREFIX/llvm, so
#                   the install does not depend on the download cache
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
if [ "${LLVM_RELEASE:-}" = 1 ] && [ -z "${LLVM_BIN:-}${LLVM_PREFIX:-}" ]; then
	rel=$(scripts/llvm-release.sh)
	mkdir -p "$tmp/llvm/bin" "$tmp/llvm/lib"
	for t in clang-22 lld llvm-ar llvm-link llvm-nm llvm-objcopy llvm-objdump mlir-translate; do
		cp "$rel/bin/$t" "$tmp/llvm/bin/"
	done
	ln -s clang-22 "$tmp/llvm/bin/clang"
	ln -s clang "$tmp/llvm/bin/clang++"
	for l in ld.lld wasm-ld; do ln -s lld "$tmp/llvm/bin/$l"; done
	ln -s llvm-ar "$tmp/llvm/bin/llvm-ranlib"
	ln -s llvm-objcopy "$tmp/llvm/bin/llvm-strip"
	# clang's resource directory (headers, compiler-rt), found beside its binary.
	cp -R "$rel/lib/clang" "$tmp/llvm/lib/"
	mkdir -p "$PREFIX"
	echo "$(cd "$PREFIX" && pwd -P)/llvm/bin" > "$tmp/build/llvm-bin"
elif [ -n "${LLVM_BIN:-}" ]; then
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
if [ -n "$llvm" ] && [ -x "$llvm/clang" ] && [ ! -f "$tmp/build/llvm-bin" ]; then
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
