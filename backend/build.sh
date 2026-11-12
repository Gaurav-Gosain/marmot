#!/bin/sh
# Builds marmot-backend, marmot-opt and marmot-pcln into BACKEND_BIN_DIR
# (default build/bin, as make backend and scripts/linux/native.sh expect).
# SPEC rule 12: on the shared laptop tree, development builds go to a private
# directory (backend/test.sh uses .work/backend/bin); build/bin is installed
# only from a committed, tested state with backend/install.sh. Honors LLVM_DIR
# and MLIR_DIR. Extra arguments go to cmake at configure time.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
LLVM_PREFIX=${LLVM_PREFIX:-/opt/homebrew/opt/llvm@22}
LLVM_DIR=${LLVM_DIR:-$LLVM_PREFIX/lib/cmake/llvm}
MLIR_DIR=${MLIR_DIR:-$LLVM_PREFIX/lib/cmake/mlir}
BUILD=${BACKEND_BUILD_DIR:-$ROOT/build/backend}
OUT=${BACKEND_BIN_DIR:-$ROOT/build/bin}

if [ ! -f "$BUILD/build.ninja" ] || [ $# -gt 0 ]; then
  cmake -G Ninja -S "$ROOT/backend" -B "$BUILD" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_EXPORT_COMPILE_COMMANDS=ON \
    -DCMAKE_RUNTIME_OUTPUT_DIRECTORY="$OUT" \
    -DLLVM_DIR="$LLVM_DIR" -DMLIR_DIR="$MLIR_DIR" "$@"
fi
cmake --build "$BUILD"
