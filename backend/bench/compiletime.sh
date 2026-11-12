#!/bin/sh
# compiletime.sh MODE...: build time of cmd/marmot itself .
# MODE: go (go build), dev (marmot build), rel (marmot build -release).
# Steps per mode: cold (go: empty GOCACHE; marmot: -nocache), prime (marmot: fill the caches),
# warm (no change), edit-codegen and edit-codegen2 (one line added to internal/codegen),
# edit-leaf (one line added to internal/mlir, imported by codegen and driver).
# Builds a copy of cmd/ and internal/ in $OUT/src, never the tree itself.
# Prints "MODE STEP exit wall user sys" per build; logs in $OUT.
# Env: OUT (default build/compiletime), MARMOT (default build/bin/marmot).
R=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${OUT:-$R/build/compiletime}
MARMOT=${MARMOT:-$R/build/bin/marmot}
S=$OUT/src
mkdir -p "$OUT" && OUT=$(cd "$OUT" && pwd)
rm -rf "$S" && mkdir -p "$S" && cp -R "$R/cmd" "$R/internal" "$R/go.mod" "$R/go.sum" "$S/"
rm -rf "$S/internal/specialize/cmd"
export MARMOT_ROOT=${MROOT:-$R}
cd "$S" || exit 1
leaf=internal/mlir/mlir.go
restore() { cp "$R/internal/codegen/func.go" internal/codegen/func.go; cp "$R/$leaf" $leaf; }
run() { # mode step command
  log=$OUT/$1-$2.log
  /usr/bin/time -p sh -c "$3" > "$log" 2>&1
  st=$?
  # Time spent waiting for a marmot build slot (other builds on the host) is not build time.
  wait=$(sed -n 's/.*got build slot [0-9]* after \([0-9.]*\)s.*/\1/p' "$log" | head -1)
  printf '%s %s exit=%s wall=%s user=%s sys=%s slotwait=%s\n' "$1" "$2" "$st" \
    "$(awk '/^real/{print $2}' "$log")" "$(awk '/^user/{print $2}' "$log")" "$(awk '/^sys/{print $2}' "$log")" "${wait:-0}"
}
probe() { echo "func editProbe$2() int { return $2 }" >> "$1"; }
for mode in "$@"; do
  restore
  case $mode in
  go)  export GOCACHE=$OUT/gocache; rm -rf "$GOCACHE"
       B="go build -o $OUT/bin-go" ;;
  dev) B="timeout -s KILL 1800 $MARMOT build -v $MFLAGS -o $OUT/bin-dev" ;;
  rel) B="timeout -s KILL 3600 $MARMOT build -release -v $MFLAGS -o $OUT/bin-rel" ;;
  *) echo "unknown mode $mode"; exit 2 ;;
  esac
  P=./cmd/marmot
  if [ $mode = go ]; then run $mode cold "$B $P"; else run $mode cold "$B -nocache $P"; run $mode prime "$B $P"; fi
  B="$B $P"
  run $mode warm "$B"
  probe internal/codegen/func.go 1; run $mode edit-codegen "$B"
  probe internal/codegen/func.go 2; run $mode edit-codegen2 "$B"
  restore
  probe $leaf 3; run $mode edit-leaf "$B"
  restore
  [ $mode = go ] && rm -rf "$GOCACHE"
done
rm -rf "$S"
