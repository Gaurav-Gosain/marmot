#!/bin/sh
# gcsites.sh: check every allocation site (go.new, go.make_slice, go.box, go.make_closure) of
# each program against its type descriptor (verify/check -gcsites), with the lifetime
# transforms off and on.
#   verify/check/gcsites.sh [dir ...]     default: tests/run/core
set -u
cd "$(dirname "$0")/../.."
W=.work/verify/gcsites
mkdir -p "$W"
go build -o "$W/check" ./verify/check || exit 2
st=0; n=0; total=0
for dir in "${@:-tests/run/core}"; do
	for f in "$dir"/*.go; do
		p=$(basename "$f" .go)
		for life in "" "MARMOT_LIFETIME=all"; do
			m="$W/$p.mlir"
			if ! env $life timeout 120 ./build/bin/marmot build -O 2 -nocache -dialect=go -emit mlir -o "$m" "$f" >/dev/null 2>&1; then
				echo "$p [$life]: skipped (does not build)"; continue
			fi
			out=$(timeout 120 "$W/check" -gcsites "$m" 2>&1)
			case "$out" in *" 0 mismatches"*) ;; *) echo "$p [$life]: $out"; st=1 ;; esac
			k=$(echo "$out" | sed -n 's/^check: \([0-9]*\) allocation sites checked.*/\1/p')
			total=$((total + ${k:-0}))
			rm -f "$m"
		done
		n=$((n + 1))
	done
done
echo "gcsites: $n programs, $total sites checked, status $st"
exit $st
