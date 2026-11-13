#!/bin/sh
# Build and run runtime core tests.
#
# Environment: CC, CFLAGS, LIB (archive or source files), OUT (build dir),
# LDLIBS (default -lpthread), POSTLINK (a command run as POSTLINK BIN -o BIN
# after linking), RUN (a command the test runs under, as RUN BIN), WASM (set
# for wasm: tests marked "// wasm: skip" are left out).
# Tests run one at a time under nice (shared machine). On the laptop run a
# subset: make test TESTS="test/sort.c test/simd.c"; full runs go to the Linux test host
# (linux-build.sh).
# Each test is a C file defining marmot_main. Header comments set expectations:
#   // expect-exit: N        exit status (default 0)
#   // expect-stdout: TEXT   stdout must equal TEXT (one line, \n appended)
#   // expect-stderr: TEXT   stderr must contain TEXT
#   // env: K=V ...          environment for the run
#   // timeout: SECONDS      default 60
set -u
mkdir -p "$OUT"
pass=0
fail=0
failed=""
for src in "$@"; do
  name=$(basename "$src" .c)
  if [ -n "${TSAN:-}" ] && grep -q '^// tsan: skip' "$src"; then
    echo "skip $name (tsan)"; continue
  fi
  if [ -n "${WASM:-}" ] && grep -q '^// wasm: skip' "$src"; then
    echo "skip $name (wasm)"; continue
  fi
  bin="$OUT/$name"
  if ! { $CC $CFLAGS "$src" $LIB ${LDLIBS--lpthread} -o "$bin" &&
         { [ -z "${POSTLINK:-}" ] || $POSTLINK "$bin" -o "$bin"; }; } 2> "$OUT/$name.build"; then
    echo "FAIL $name (build)"; cat "$OUT/$name.build"
    fail=$((fail+1)); failed="$failed $name"; continue
  fi
  want_exit=$(sed -n 's|^// expect-exit: *||p' "$src" | head -1); want_exit=${want_exit:-0}
  want_out=$(sed -n 's|^// expect-stdout: *||p' "$src" | head -1)
  want_err=$(sed -n 's|^// expect-stderr: *||p' "$src" | head -1)
  env_vars=$(sed -n 's|^// env: *||p' "$src" | head -1)
  tmo=$(sed -n 's|^// timeout: *||p' "$src" | head -1); tmo=${tmo:-60}
  start=$(date +%s)
  env $env_vars nice -n 10 perl -e 'alarm shift; exec @ARGV' "$tmo" ${RUN:-} "$bin" > "$OUT/$name.out" 2> "$OUT/$name.err"
  code=$?
  dur=$(( $(date +%s) - start ))
  ok=1
  [ "$code" = "$want_exit" ] || ok=0
  if [ -n "$want_out" ] && [ "$(cat "$OUT/$name.out")" != "$want_out" ]; then ok=0; fi
  if [ -n "$want_err" ] && ! grep -qF -- "$want_err" "$OUT/$name.err"; then ok=0; fi
  if [ $ok = 1 ]; then
    echo "ok   $name (${dur}s)"; pass=$((pass+1))
  else
    echo "FAIL $name: exit $code (want $want_exit)"
    echo "--- stdout"; head -20 "$OUT/$name.out"
    echo "--- stderr"; head -20 "$OUT/$name.err"
    fail=$((fail+1)); failed="$failed $name"
  fi
done
echo "$pass passed, $fail failed"
[ $fail = 0 ] || { echo "failed:$failed"; exit 1; }
