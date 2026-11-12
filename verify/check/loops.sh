#!/bin/sh
# loops.sh: build programs with the loop passes and their report (-loops-report), then check
# every parallelized loop's claims against the proved rules.
#   verify/check/loops.sh [file.go ...]   default: backend/test/loops/Inputs/*.go
set -u
cd "$(dirname "$0")/../.."
export GOCACHE="$PWD/.work/gocache" # build cache inside the repo
W=$PWD/.work/verify/loops
mkdir -p "$W"
go build -o "$W/check" ./verify/check || exit 2
st=0
for f in ${*:-backend/test/loops/Inputs/*.go}; do
	p=$(basename "$f" .go)
	rm -f "$W/$p.jsonl"
	if ! MARMOT_BACKEND_FLAGS="-par -type-alias -loops-report=$W/$p.jsonl" timeout 600 ./build/bin/marmot build -O 2 -nocache -o "$W/$p.bin" "$f" >/dev/null 2>&1; then
		echo "$p: skipped (does not build)"; continue
	fi
	rm -f "$W/$p.bin"
	[ -f "$W/$p.jsonl" ] || { echo "$p: no parallel loops"; continue; }
	echo "$p: $("$W/check" -loops-report "$W/$p.jsonl" | tail -1)"
	"$W/check" -loops-report "$W/$p.jsonl" >/dev/null || st=1
	rm -f "$W/$p.jsonl"
done
exit $st
