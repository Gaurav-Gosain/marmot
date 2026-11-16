#!/bin/sh
# Builds marmot-backend, marmot-opt and marmot-pcln into BACKEND_BIN_DIR
# (default build/bin, as make backend expects). backend/test.sh builds into a
# private directory and tests that; backend/install.sh copies a tested build of
# a committed tree into build/bin. Honors LLVM_DIR
# and MLIR_DIR. Extra arguments go to cmake at configure time.
#
# LLVM_RELEASE=1 (when LLVM_PREFIX, LLVM_DIR and MLIR_DIR are not set): build
# against the official LLVM release that scripts/llvm-release.sh downloads,
# linked statically. A static LLVM (llvm-config --shared-mode) always builds
# with MARMOT_STATIC=ON.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
if [ "${LLVM_RELEASE:-}" = 1 ] && [ -z "${LLVM_PREFIX:-}${LLVM_DIR:-}${MLIR_DIR:-}" ]; then
  LLVM_PREFIX=$("$ROOT/scripts/llvm-release.sh")
fi
LLVM_PREFIX=${LLVM_PREFIX:-/opt/homebrew/opt/llvm@22}
LLVM_DIR=${LLVM_DIR:-$LLVM_PREFIX/lib/cmake/llvm}
MLIR_DIR=${MLIR_DIR:-$LLVM_PREFIX/lib/cmake/mlir}
BUILD=${BACKEND_BUILD_DIR:-$ROOT/build/backend}
OUT=${BACKEND_BIN_DIR:-$ROOT/build/bin}

# MARMOT_STATIC follows llvm-config; -DMARMOT_STATIC in the arguments comes
# later and wins.
static=
case $("$LLVM_PREFIX/bin/llvm-config" --shared-mode 2>/dev/null || true) in
static) static=-DMARMOT_STATIC=ON ;;
shared) static=-DMARMOT_STATIC=OFF ;;
esac
# A build directory configured against another LLVM starts over: entries
# cached from the old one (MLIR_DIR, library paths) would stay.
configured=$(sed -n 's/^LLVM_DIR:[A-Z]*=//p' "$BUILD/CMakeCache.txt" 2>/dev/null || true)
if [ -n "$configured" ] && [ "$configured" != "$LLVM_DIR" ]; then
  rm -rf "$BUILD/CMakeCache.txt" "$BUILD/CMakeFiles"
fi
if [ ! -f "$BUILD/build.ninja" ] || [ ! -f "$BUILD/CMakeCache.txt" ] || [ $# -gt 0 ]; then
  cmake -G Ninja -S "$ROOT/backend" -B "$BUILD" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_EXPORT_COMPILE_COMMANDS=ON \
    -DCMAKE_RUNTIME_OUTPUT_DIRECTORY="$OUT" \
    -DLLVM_DIR="$LLVM_DIR" -DMLIR_DIR="$MLIR_DIR" $static "$@"
fi
cmake --build "$BUILD"
