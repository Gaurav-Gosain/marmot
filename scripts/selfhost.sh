#!/bin/sh
# M7 stage test (SPEC 3, M7).
#
#   marmot-1 = go build ./cmd/marmot
#   marmot-2 = marmot-1 build ./cmd/marmot
#   marmot-3 = marmot-2 build ./cmd/marmot
#
# Then marmot-2 and marmot-3 must emit identical MLIR for every tests/run/core
# program and for cmd/marmot itself, and the core tier must pass with
# marmot-2 as the compiler. Usage: scripts/selfhost.sh [core-test-regexp]
# Runs sequentially under nice (SPEC rule 10). Output: .work/selfhost/.
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
export MARMOT_ROOT="$ROOT" MARMOT_MAX_JOBS=2
OUT="$ROOT/.work/selfhost"
FILTER="${1:-.}"
rm -rf "$OUT"
mkdir -p "$OUT/mlir2" "$OUT/mlir3" "$OUT/run"
T="timeout 900"
fail=0

step() { echo "== $*"; }

step "stage 1: go build"
$T nice go build -o "$OUT/marmot-1" ./cmd/marmot || { echo "stage 1 failed"; exit 1; }
step "stage 2: marmot-1 builds marmot"
$T nice "$OUT/marmot-1" build -o "$OUT/marmot-2" ./cmd/marmot || { echo "stage 2 failed"; exit 1; }
step "stage 3: marmot-2 builds marmot"
$T nice "$OUT/marmot-2" build -o "$OUT/marmot-3" ./cmd/marmot || { echo "stage 3 failed"; exit 1; }
ls -l "$OUT"/marmot-[123] | awk '{print $5, $NF}'

step "MLIR of cmd/marmot: marmot-2 vs marmot-3"
$T nice "$OUT/marmot-2" build -emit mlir -o "$OUT/mlir2/cmd-marmot.mlir" ./cmd/marmot
$T nice "$OUT/marmot-3" build -emit mlir -o "$OUT/mlir3/cmd-marmot.mlir" ./cmd/marmot
if cmp -s "$OUT/mlir2/cmd-marmot.mlir" "$OUT/mlir3/cmd-marmot.mlir"; then
	echo "identical"
else
	echo "DIFFERENT"
	fail=1
fi

step "MLIR of tests/run/core: marmot-2 vs marmot-3"
ndiff=0
nmlir=0
for f in tests/run/core/*.go; do
	echo "$f" | grep -Eq "$FILTER" || continue
	b=$(basename "$f" .go)
	$T nice "$OUT/marmot-2" build -emit mlir -o "$OUT/mlir2/$b.mlir" "$f" 2>/dev/null
	$T nice "$OUT/marmot-3" build -emit mlir -o "$OUT/mlir3/$b.mlir" "$f" 2>/dev/null
	nmlir=$((nmlir + 1))
	if ! cmp -s "$OUT/mlir2/$b.mlir" "$OUT/mlir3/$b.mlir"; then
		echo "DIFFERENT: $b"
		ndiff=$((ndiff + 1))
	fi
done
echo "$nmlir programs, $ndiff differ"
[ "$ndiff" -eq 0 ] || fail=1

step "core tier with marmot-2 as the compiler"
pass=0
nfail=0
for f in tests/run/core/*.go; do
	echo "$f" | grep -Eq "$FILTER" || continue
	b=$(basename "$f" .go)
	d=tests/run/core
	exe="$OUT/run/$b"
	if ! $T nice "$OUT/marmot-2" build -o "$exe" "$f" > "$OUT/run/$b.build" 2>&1; then
		echo "FAIL $b: compile error"
		nfail=$((nfail + 1))
		continue
	fi
	(cd "$d" && timeout 30 "$exe" > "$OUT/run/$b.out" 2> "$OUT/run/$b.err")
	code=$?
	want=0
	[ -f "$d/$b.exit" ] && want=$(cat "$d/$b.exit")
	ok=1
	[ "$code" -eq "$want" ] || ok=0
	cmp -s "$OUT/run/$b.out" "$d/$b.out" || ok=0
	if [ -f "$d/$b.exit" ]; then
		[ "$(head -1 "$OUT/run/$b.err")" = "$(head -1 "$d/$b.err")" ] || ok=0
	elif [ -f "$d/$b.err" ]; then
		cmp -s "$OUT/run/$b.err" "$d/$b.err" || ok=0
	fi
	if [ $ok -eq 1 ]; then
		pass=$((pass + 1))
		rm -f "$exe"
	else
		echo "FAIL $b: exit $code (want $want) or output differs"
		nfail=$((nfail + 1))
	fi
done
echo "core with marmot-2: $pass pass, $nfail fail"
[ "$nfail" -eq 0 ] || fail=1

[ $fail -eq 0 ] && echo "SELFHOST OK" || echo "SELFHOST FAILED"
exit $fail
