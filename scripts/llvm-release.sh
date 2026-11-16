#!/bin/sh
# llvm-release.sh: the official LLVM release marmot builds against with
# LLVM_RELEASE=1 (backend/build.sh, scripts/install.sh). Prints the directory
# of the unpacked release, after it downloads, checks and unpacks it the
# first time.
#
#   scripts/llvm-release.sh            print the release directory
#
# The release has static LLVM and MLIR libraries only, so the backend links
# them with MARMOT_STATIC=ON (one binary, no libLLVM to load per run).
# Downloads go to MARMOT_LLVM_CACHE (default ~/.cache/marmot/llvm). A
# tarball is used only when its sha256 is in scripts/llvm-release.sha256;
# add a row only for a download verified against the release's own
# attestation:
#
#   gh attestation verify ASSET.tar.xz --repo llvm/llvm-project --bundle ASSET.tar.xz.jsonl
set -eu
VERSION=22.1.8
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CACHE=${MARMOT_LLVM_CACHE:-$HOME/.cache/marmot/llvm}

die() { echo "llvm-release.sh: $*" >&2; exit 1; }

case "$(uname -s)/$(uname -m)" in
Linux/x86_64) asset=LLVM-$VERSION-Linux-X64 ;;
Linux/aarch64) asset=LLVM-$VERSION-Linux-ARM64 ;;
Darwin/arm64) asset=LLVM-$VERSION-macOS-ARM64 ;;
*) die "no LLVM $VERSION release for $(uname -s)/$(uname -m)" ;;
esac
dir=$CACHE/$VERSION/$asset

# check DIR: the unpack has what marmot needs, else a message on stderr.
check() {
	d=$1
	[ -x "$d/bin/llvm-config" ] || { echo "no bin/llvm-config" >&2; return 1; }
	v=$("$d/bin/llvm-config" --version)
	[ "$v" = "$VERSION" ] || { echo "llvm-config says $v, not $VERSION" >&2; return 1; }
	targets=$("$d/bin/llvm-config" --targets-built)
	for t in X86 AArch64 NVPTX WebAssembly; do
		case " $targets " in *" $t "*) ;; *) echo "no $t target" >&2; return 1 ;; esac
	done
	for f in lib/cmake/llvm/LLVMConfig.cmake lib/cmake/mlir/MLIRConfig.cmake lib/libMLIRIR.a \
		bin/clang bin/ld.lld bin/wasm-ld bin/llvm-objcopy bin/llvm-objdump bin/llvm-nm bin/llvm-link bin/mlir-translate; do
		[ -e "$d/$f" ] || { echo "no $f" >&2; return 1; }
	done
}

# cfg DIR: on macOS the release clang has no default SDK; clang.cfg beside it
# (read by clang at start; clang++ reads clang++.cfg) names the one xcrun
# finds. Written again when the recorded SDK is gone (an Xcode update).
cfg() {
	[ "$(uname -s)" = Darwin ] || return 0
	if [ -f "$1/bin/clang.cfg" ] && [ -f "$1/bin/clang++.cfg" ]; then
		old=$(sed -n 's/^-isysroot //p' "$1/bin/clang.cfg")
		[ -n "$old" ] && [ -d "$old" ] && return 0
	fi
	sdk=$(xcrun --show-sdk-path 2>/dev/null) || die "no macOS SDK (xcode-select --install)"
	echo "-isysroot $sdk" > "$1/bin/clang.cfg"
	echo "@clang.cfg" > "$1/bin/clang++.cfg"
}

if [ -f "$dir/.checked" ]; then
	cfg "$dir"
	echo "$dir"
	exit 0
fi

# One download and unpack at a time (two builds on a fresh host share the
# cache); flock where it exists, else a lock directory.
mkdir -p "$CACHE/$VERSION"
lock=$CACHE/$VERSION/.lock
if command -v flock >/dev/null 2>&1; then
	exec 9>"$lock"
	flock 9
else
	until mkdir "$lock.d" 2>/dev/null; do sleep 2; done
	trap 'rmdir "$lock.d"' EXIT
fi
if [ -f "$dir/.checked" ]; then
	echo "$dir"
	exit 0
fi

sum=$(awk -v a="$asset.tar.xz" '$2 == a { print $1 }' "$ROOT/scripts/llvm-release.sha256")
[ -n "$sum" ] || die "no verified checksum for $asset.tar.xz in scripts/llvm-release.sha256"

tar=$CACHE/$VERSION/$asset.tar.xz
if [ ! -f "$tar" ]; then
	echo "llvm-release.sh: downloading $asset.tar.xz (about 2 GB) into $CACHE/$VERSION (MARMOT_LLVM_CACHE)" >&2
	curl -fL --retry 3 -o "$tar.part" \
		"https://github.com/llvm/llvm-project/releases/download/llvmorg-$VERSION/$asset.tar.xz" ||
		{ rm -f "$tar.part"; die "download of $asset.tar.xz failed"; }
	mv "$tar.part" "$tar"
fi
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tar" | cut -d' ' -f1)
else
	got=$(shasum -a 256 "$tar" | cut -d' ' -f1)
fi
if [ "$got" != "$sum" ]; then
	rm -f "$tar"
	die "$asset.tar.xz has sha256 $got, expected $sum; the file is removed"
fi

echo "llvm-release.sh: unpacking $asset.tar.xz" >&2
rm -rf "$dir.part"
mkdir -p "$dir.part"
tar -xJf "$tar" -C "$dir.part" --strip-components=1 || { rm -rf "$dir.part"; die "unpack of $asset.tar.xz failed"; }
why=$(check "$dir.part" 2>&1) || { rm -rf "$dir.part"; die "$asset is not usable: $why"; }
rm -rf "$dir"
mv "$dir.part" "$dir"
rm -f "$tar"
cfg "$dir"
touch "$dir/.checked"
echo "$dir"
