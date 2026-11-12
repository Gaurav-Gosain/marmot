#!/bin/sh
# gccheck.sh: check the pointer bitmaps of every type descriptor in the MLIR of each program
# (verify/check -gcdata) against gc's layout computed independently from go/types.
#   verify/check/gccheck.sh [dir ...]     default: tests/run/core
# A dir holding go.mod is built as one module (tests/world/NAME); any other dir is walked for
# single-file programs (*.go at any depth, as in tests/run/std/PKG).
set -u
export MARMOT_OUTPUT=plain # plain compiler output whoever runs this
cd "$(dirname "$0")/../.."
export GOCACHE="$PWD/.work/gocache" # build cache inside the repo
R=$(pwd)
W=.work/verify/gccheck
mkdir -p "$W"
go build -o "$W/check" ./verify/check || exit 2
st=0; n=0; total=0

one() { # one NAME DIR TARGET
	m="$R/$W/$1.mlir"
	if ! (cd "$2" && timeout 300 "$R/build/bin/marmot" build -O 2 -nocache -emit mlir -o "$m" "$3" >/dev/null 2>&1); then
		echo "$1: skipped (does not build)"; return
	fi
	out=$(cd "$2" && timeout 300 "$R/$W/check" -gcdata "$m" "$3" 2>&1)
	case "$out" in *" 0 mismatches"*) ;; *) echo "$1: $out"; st=1 ;; esac
	k=$(echo "$out" | sed -n 's/^check: \([0-9]*\) type descriptors checked.*/\1/p')
	s=$(echo "$out" | sed -n 's/.* \([0-9]*\) skipped$/\1/p')
	[ "${s:-0}" != 0 ] && echo "$1: ${s} descriptors skipped"
	total=$((total + ${k:-0})); n=$((n + 1))
	rm -f "$m"
}

for dir in "${@:-tests/run/core}"; do
	if [ -f "$dir/go.mod" ]; then
		one "$(basename "$dir")" "$dir" .
		continue
	fi
	for f in $(find "$dir" -name vendor -prune -o -name '*.go' -print | sort); do
		p=$(echo "${f#"$dir"/}" | sed 's/\.go$//; s|/|_|g')
		one "$p" "$(dirname "$f")" "$(basename "$f")"
	done
done
echo "gccheck: $n programs, $total descriptors checked, status $st"
exit $st
