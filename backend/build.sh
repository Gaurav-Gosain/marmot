#!/bin/sh
# Builds build/bin/marmot-backend. Honors LLVM_DIR and MLIR_DIR.
# Extra arguments go to cmake at configure time, for example -DMARMOT_STATIC=ON.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
LLVM_PREFIX=${LLVM_PREFIX:-/opt/homebrew/opt/llvm@22}
LLVM_DIR=${LLVM_DIR:-$LLVM_PREFIX/lib/cmake/llvm}
MLIR_DIR=${MLIR_DIR:-$LLVM_PREFIX/lib/cmake/mlir}
BUILD=$ROOT/build/backend

if [ ! -f "$BUILD/build.ninja" ] || [ $# -gt 0 ]; then
  cmake -G Ninja -S "$ROOT/backend" -B "$BUILD" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_EXPORT_COMPILE_COMMANDS=ON \
    -DCMAKE_RUNTIME_OUTPUT_DIRECTORY="$ROOT/build/bin" \
    -DLLVM_DIR="$LLVM_DIR" -DMLIR_DIR="$MLIR_DIR" "$@"
fi
cmake --build "$BUILD"
