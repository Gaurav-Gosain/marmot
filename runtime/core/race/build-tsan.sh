#!/bin/sh
# build-tsan.sh COMPILER_RT OUTDIR: build the Go flavor of the TSan runtime
# (compiler-rt/lib/tsan/go/buildgo.sh, as gc's race_GOOS_GOARCH.syso) and write
# OUTDIR/race_GOOS_GOARCH.o. Four names exist in both the Go interface and the C
# interface that LLVM's ThreadSanitizer pass calls (runtime/core/src/race.c
# implements the C one): they are renamed here to __gotsan_*.
set -eu
CRT=$(cd "$1" && pwd)
OUT=$(mkdir -p "$2" && cd "$2" && pwd)
OBJCOPY=${OBJCOPY:-llvm-objcopy}
W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT
mkdir -p "$W/lib"
cp -r "$CRT/lib/tsan" "$CRT/lib/sanitizer_common" "$CRT/lib/ubsan" "$CRT/lib/interception" "$W/lib/"
cp -r "$CRT/include" "$W/"
cd "$W/lib/tsan/go"
CC=${CC:-clang} SILENT=1 sh buildgo.sh > "$W/build.log" 2>&1 || { tail -30 "$W/build.log"; exit 1; }
./test > /dev/null 2>&1 || true  # compiler-rt's own check reports one race on purpose
for f in race_*_*.syso; do
  "$OBJCOPY" --redefine-sym __tsan_init=__gotsan_init --redefine-sym __tsan_func_exit=__gotsan_func_exit \
    --redefine-sym __tsan_read_range=__gotsan_read_range --redefine-sym __tsan_write_range=__gotsan_write_range \
    "$f" "$OUT/${f%.syso}.o"
  echo "$OUT/${f%.syso}.o"
done
