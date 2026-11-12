#!/bin/sh
# phases.sh: per-phase build times of cmd/marmot with marmot dev and go
# build, using every core (no build slots, no job caps). Steps: cold (empty
# caches), warm (no change), edit (one line added to internal/codegen), leaf
# (one line added to internal/mlir). Prints wall, user and sys per step and,
# for marmot, the -v phase timeline with the backend's totals.
# Runs in a private marmot root under OUT (its own build cache) that links
# the tools and sources of this tree. Env: OUT (default build/phases),
# MARMOT (default build/bin/marmot).
R=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${OUT:-$R/build/phases}
MARMOT=${MARMOT:-$R/build/bin/marmot}
mkdir -p "$OUT" && OUT=$(cd "$OUT" && pwd)
S=$OUT/src
rm -rf "$S" && mkdir -p "$S" && cp -R "$R/cmd" "$R/internal" "$R/go.mod" "$R/go.sum" "$S/"
rm -rf "$S/internal/specialize/cmd"
MR=$OUT/root
rm -rf "$MR" && mkdir -p "$MR/build/bin"
for x in lib internal go.mod; do ln -s "$R/$x" "$MR/$x"; done
ln -s "$R/build/lib" "$MR/build/lib"
ln -s "$MARMOT" "$MR/build/bin/marmot"
for t in marmot-backend marmot-pcln marmot-opt; do ln -s "$R/build/bin/$t" "$MR/build/bin/$t"; done
MARMOT=$MR/build/bin/marmot
export MARMOT_ROOT=$MR MARMOT_SLOTS=0 MARMOT_MAX_JOBS=$(getconf _NPROCESSORS_ONLN)
export MARMOT_CACHE_MAX_MB=${MARMOT_CACHE_MAX_MB:-4096}
cd "$S" || exit 1
probe() { echo "func phaseProbe$2() int { return $2 }" >> "$1"; }
run() { # tool step command
  log=$OUT/$1-$2.log
  /usr/bin/time -p sh -c "$3" > "$log" 2>&1
  printf '%s %s wall=%s user=%s sys=%s\n' "$1" "$2" \
    "$(awk '/^real/{print $2}' "$log")" "$(awk '/^user/{print $2}' "$log")" "$(awk '/^sys/{print $2}' "$log")"
  grep 'marmot:' "$log" | sed 's/^/    /'
}
export GOCACHE=$OUT/gocache
rm -rf "$GOCACHE" "$MR/build/cache"
run go cold "go build -o $OUT/go.bin ./cmd/marmot"
run go warm "go build -o $OUT/go.bin ./cmd/marmot"
probe internal/codegen/func.go 1; run go edit "go build -o $OUT/go.bin ./cmd/marmot"
probe internal/mlir/mlir.go 2; run go leaf "go build -o $OUT/go.bin ./cmd/marmot"
cp "$R/internal/codegen/func.go" internal/codegen/func.go; cp "$R/internal/mlir/mlir.go" internal/mlir/mlir.go
# go has the standard library export data now; marmot starts empty.
M="$MARMOT build -v -o $OUT/m.bin ./cmd/marmot"
run marmot cold "$M"
run marmot warm "$M"
probe internal/codegen/func.go 3; run marmot edit "$M"
probe internal/mlir/mlir.go 4; run marmot leaf "$M"
rm -rf "$S" "$MR"
