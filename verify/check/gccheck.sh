#!/bin/sh
# gccheck.sh: check the pointer bitmaps of every type descriptor in the MLIR of each program
# (verify/check -gcdata) against gc's layout computed independently from go/types.
#   verify/check/gccheck.sh [dir ...]     default: tests/run/core
set -u
cd "$(dirname "$0")/../.."
W=.work/verify/gccheck
mkdir -p "$W"
go build -o "$W/check" ./verify/check || exit 2
st=0; n=0; total=0
for dir in "${@:-tests/run/core}"; do
	for f in "$dir"/*.go; do
		p=$(basename "$f" .go)
		m="$W/$p.mlir"
		if ! timeout 120 ./build/bin/marmot build -O 2 -nocache -emit mlir -o "$m" "$f" >/dev/null 2>&1; then
			echo "$p: skipped (does not build)"; continue
		fi
		out=$(cd "$(dirname "$f")" && timeout 300 "$OLDPWD/$W/check" -gcdata "$OLDPWD/$m" "$(basename "$f")" 2>&1)
		case "$out" in *" 0 mismatches"*) ;; *) echo "$p: $out"; st=1 ;; esac
		k=$(echo "$out" | sed -n 's/^check: \([0-9]*\) type descriptors checked.*/\1/p')
		total=$((total + ${k:-0})); n=$((n + 1))
		rm -f "$m"
	done
done
echo "gccheck: $n programs, $total descriptors checked, status $st"
exit $st
